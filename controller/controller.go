package controller

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/grapebaba/fabric-operator/spec"
	"github.com/grapebaba/fabric-operator/util/k8sutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	v1beta1extensions "k8s.io/client-go/pkg/apis/extensions/v1beta1"
	kwatch "k8s.io/client-go/pkg/watch"
	"encoding/json"
	"io"
	"sync"
	"github.com/grapebaba/fabric-operator/chain"
	"github.com/coreos/etcd-operator/pkg/analytics"
)

var (
	ErrVersionOutdated = errors.New("requested version is outdated in apiserver")

	initRetryWaitTime = 30 * time.Second

	// Workaround for watching TPR resource.
	// client-go has encoding issue and we want something more predictable.
	KubeHttpCli *http.Client
	MasterHost  string
)

type Event struct {
	Type   kwatch.EventType
	Object *spec.Chain
}

type Config struct {
	Namespace      string
	ServiceAccount string
	PVProvisioner  string
	KubeCli        kubernetes.Interface
}

func (c *Config) Validate() error {
	return nil
}

type Controller struct {
	logger *logrus.Entry
	Config

	//// TODO: combine the three cluster map.
	chains map[string]*chain.Chain
	// Kubernetes resource version of the chains
	chainRVs  map[string]string
	stopChMap map[string]chan struct{}

	waitChain sync.WaitGroup
}

func New(cfg Config) *Controller {
	return &Controller{Config: cfg}
	return &Controller{
		logger: logrus.WithField("controller", "controller"),

		Config:    cfg,
		chains:    make(map[string]*chain.Chain),
		chainRVs:  make(map[string]string),
		stopChMap: map[string]chan struct{}{},
	}
}

func (c *Controller) Run() error {
	var (
		watchVersion string
		err          error
	)

	for {
		watchVersion, err = c.initResource()
		if err == nil {
			break
		}
		c.logger.Errorf("initialization failed: %v", err)
		c.logger.Infof("retry in %v...", initRetryWaitTime)
		time.Sleep(initRetryWaitTime)
		// todo: add max retry?
	}

	c.logger.Infof("starts running from watch version: %s", watchVersion)

	//defer func() {
	//	for _, stopC := range c.stopChMap {
	//		close(stopC)
	//	}
	//	c.waitCluster.Wait()
	//}()
	//
	eventCh, errCh := c.watch(watchVersion)
	//
	go func() {
		pt := newPanicTimer(time.Minute, "unexpected long blocking (> 1 Minute) when handling cluster event")

		for ev := range eventCh {
			pt.start()
			c.handleClusterEvent(ev)
			pt.stop()
		}
	}()
	return <-errCh
}

func (c *Controller) initResource() (string, error) {
	watchVersion := "0"
	err := c.createTPR()
	if err != nil {
		if k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			// TPR has been initialized before. We need to recover existing cluster.
			watchVersion, err = c.findAllChains()
			if err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("fail to create TPR: %v", err)
		}
	}

	return watchVersion, nil
}

func (c *Controller) createTPR() error {
	tpr := &v1beta1extensions.ThirdPartyResource{
		ObjectMeta: v1.ObjectMeta{
			Name: spec.TPRName(),
		},
		Versions: []v1beta1extensions.APIVersion{
			{Name: spec.TPRVersion},
		},
		Description: spec.TPRDescription,
	}
	_, err := c.KubeCli.ExtensionsV1beta1().ThirdPartyResources().Create(tpr)
	if err != nil {
		return err
	}

	return k8sutil.WaitChainTPRReady(c.KubeCli.CoreV1().RESTClient(), 3*time.Second, 30*time.Second, c.Namespace)
}

func (c *Controller) findAllChains() (string, error) {
	c.logger.Info("finding existing chains...")
	chainList, err := k8sutil.GetChainList(c.Config.KubeCli.CoreV1().RESTClient(), c.Config.Namespace)
	if err != nil {
		return "", err
	}

	//TODO: Recover existing chains
	//for i := range chainList.Items {
	//	chain := chainList.Items[i]
	//
	//	if chain.Status.IsFailed() {
	//		c.logger.Infof("ignore failed chain (%s). Please delete its TPR", chain.Metadata.Name)
	//		continue
	//	}
	//
	//	chain.Spec.Cleanup()
	//
	//	stopC := make(chan struct{})
	//	nc := cluster.New(c.makeClusterConfig(), &chain, stopC, &c.waitCluster)
	//	c.stopChMap[chain.Metadata.Name] = stopC
	//	c.clusters[chain.Metadata.Name] = nc
	//	c.clusterRVs[chain.Metadata.Name] = chain.Metadata.ResourceVersion
	//}

	return chainList.Metadata.ResourceVersion, nil
}

// watch creates a go routine, and watches the chain kind resources from
// the given watch version. It emits events on the resources through the returned
// event chan. Errors will be reported through the returned error chan. The go routine
// exits on any error.
func (c *Controller) watch(watchVersion string) (<-chan *Event, <-chan error) {
	eventCh := make(chan *Event)
	// On unexpected error case, controller should exit
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)

		for {
			resp, err := k8sutil.WatchChains(MasterHost, c.Config.Namespace, KubeHttpCli, watchVersion)
			if err != nil {
				errCh <- err
				return
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				errCh <- errors.New("invalid status code: " + resp.Status)
				return
			}

			c.logger.Infof("start watching at %v", watchVersion)

			decoder := json.NewDecoder(resp.Body)
			for {
				ev, st, err := pollEvent(decoder)
				if err != nil {
					if err == io.EOF { // apiserver will close stream periodically
						c.logger.Debug("apiserver closed stream")
						break
					}

					c.logger.Errorf("received invalid event from API server: %v", err)
					errCh <- err
					return
				}

				if st != nil {
					resp.Body.Close()

					if st.Code == http.StatusGone {
						// event history is outdated.
						// if nothing has changed, we can go back to watch again.
						clusterList, err := k8sutil.GetChainList(c.Config.KubeCli.CoreV1().RESTClient(), c.Config.Namespace)
						if err == nil && !c.isClustersCacheStale(clusterList.Items) {
							watchVersion = clusterList.Metadata.ResourceVersion
							break
						}

						// if anything has changed (or error on relist), we have to rebuild the state.
						// go to recovery path
						errCh <- ErrVersionOutdated
						return
					}

					c.logger.Fatalf("unexpected status response from API server: %v", st.Message)
				}

				c.logger.Debugf("chain event: %v %v", ev.Type, ev.Object.Spec)

				watchVersion = ev.Object.Metadata.ResourceVersion
				eventCh <- ev
			}

			resp.Body.Close()
		}
	}()

	return eventCh, errCh
}

func (c *Controller) isClustersCacheStale(currentClusters []spec.Chain) bool {
	if len(c.chainRVs) != len(currentClusters) {
		return true
	}

	for _, cc := range currentClusters {
		rv, ok := c.chainRVs[cc.Metadata.Name]
		if !ok || rv != cc.Metadata.ResourceVersion {
			return true
		}
	}

	return false
}

func (c *Controller) handleClusterEvent(event *Event) {
	clus := event.Object

	if clus.Status.IsFailed() {
		c.logger.Infof("ignore failed cluster (%s). Please delete its TPR", clus.Metadata.Name)
		return
	}

	clus.Spec.Cleanup()

	switch event.Type {
	case kwatch.Added:
		stopC := make(chan struct{})
		nc := cluster.New(c.makeClusterConfig(), clus, stopC, &c.waitCluster)

		c.stopChMap[clus.Metadata.Name] = stopC
		c.clusters[clus.Metadata.Name] = nc
		c.clusterRVs[clus.Metadata.Name] = clus.Metadata.ResourceVersion

		clustersCreated.Inc()
		clustersTotal.Inc()

	//case kwatch.Modified:
	//	if _, ok := c.clusters[clus.Metadata.Name]; !ok {
	//		c.logger.Warningf("unsafe state. cluster was never created but we received event (%s)", event.Type)
	//		return
	//	}
	//	c.clusters[clus.Metadata.Name].Update(clus)
	//	c.clusterRVs[clus.Metadata.Name] = clus.Metadata.ResourceVersion
	//	clustersModified.Inc()
	//
	//case kwatch.Deleted:
	//	if _, ok := c.clusters[clus.Metadata.Name]; !ok {
	//		c.logger.Warningf("unsafe state. cluster was never created but we received event (%s)", event.Type)
	//		return
	//	}
	//	c.clusters[clus.Metadata.Name].Delete()
	//	delete(c.clusters, clus.Metadata.Name)
	//	delete(c.clusterRVs, clus.Metadata.Name)
	//	analytics.ClusterDeleted()
	//	clustersDeleted.Inc()
	//	clustersTotal.Dec()
	}
}
