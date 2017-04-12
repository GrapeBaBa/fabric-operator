package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/grapebaba/fabric-operator/peer_cluster"
	"github.com/grapebaba/fabric-operator/spec"
	"github.com/grapebaba/fabric-operator/util/k8sutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
	v1beta1extensions "k8s.io/client-go/pkg/apis/extensions/v1beta1"
	kwatch "k8s.io/client-go/pkg/watch"
)

var (
	ErrVersionOutdated = errors.New("requested version is outdated in apiserver")

	initRetryWaitTime = 30 * time.Second

	// Workaround for watching TPR resource.
	// client-go has encoding issue and we want something more predictable.
	KubeHttpCli *http.Client
	MasterHost  string
)

type PeersEvent struct {
	Type   kwatch.EventType
	Object *spec.PeerCluster
}

type Config struct {
	Namespace      string
	ServiceAccount string
	KubeCli        kubernetes.Interface
}

type Controller struct {
	logger *logrus.Entry
	Config

	//// TODO: combine the three cluster map.
	peerClusters map[string]*peer_cluster.PeerCluster
	// Kubernetes resource version of the peerClusters
	peerClusterRVs map[string]string
	stopChMap      map[string]chan struct{}

	waitPeerCluster sync.WaitGroup
}

func New(cfg Config) *Controller {
	return &Controller{Config: cfg}
	return &Controller{
		logger: logrus.WithField("controller", "controller"),

		Config:         cfg,
		peerClusters:   make(map[string]*peer_cluster.PeerCluster),
		peerClusterRVs: make(map[string]string),
		stopChMap:      map[string]chan struct{}{},
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
	eventCh, errCh := c.watchPeers(watchVersion)
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
	err := c.createPeersTPR()
	if err != nil {
		if k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			// TPR has been initialized before. We need to recover existing cluster.
			watchVersion, err = c.findAllPeerClusters()
			if err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("fail to create TPR: %v", err)
		}
	}

	return watchVersion, nil
}

func (c *Controller) createPeersTPR() error {
	tpr := &v1beta1extensions.ThirdPartyResource{
		ObjectMeta: v1.ObjectMeta{
			Name: spec.PeersTPRName(),
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

	return k8sutil.WaitPeersTPRReady(c.KubeCli.CoreV1().RESTClient(), 3*time.Second, 30*time.Second, c.Namespace)
}

func (c *Controller) findAllPeerClusters() (string, error) {
	c.logger.Info("finding existing peerClusters...")
	peerClusterList, err := k8sutil.GetPeerClusterList(c.Config.KubeCli.CoreV1().RESTClient(), c.Config.Namespace)
	if err != nil {
		return "", err
	}

	//TODO: Recover existing peerClusters
	for i := range peerClusterList.Items {
		peerCluster := peerClusterList.Items[i]

		if peerCluster.Status.IsFailed() {
			c.logger.Infof("ignore failed peerCluster (%s). Please delete its TPR", peerCluster.Metadata.Name)
			continue
		}

		peerCluster.Spec.Cleanup()

		stopC := make(chan struct{})
		nc := peer_cluster.New(c.makeClusterConfig(), &peerCluster, stopC, &c.waitPeerCluster)
		c.stopChMap[peerCluster.Metadata.Name] = stopC
		c.peerClusters[peerCluster.Metadata.Name] = nc
		c.peerClusterRVs[peerCluster.Metadata.Name] = peerCluster.Metadata.ResourceVersion
	}

	return peerClusterList.Metadata.ResourceVersion, nil
}

// watchPeers creates a go routine, and watches the peerClusters kind resources from
// the given watch version. It emits events on the resources through the returned
// event chan. Errors will be reported through the returned error chan. The go routine
// exits on any error.
func (c *Controller) watchPeers(watchVersion string) (<-chan *PeersEvent, <-chan error) {
	eventCh := make(chan *PeersEvent)
	// On unexpected error case, controller should exit
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)

		for {
			resp, err := k8sutil.WatchPeerCluster(MasterHost, c.Config.Namespace, KubeHttpCli, watchVersion)
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
				ev, st, err := pollPeersEvent(decoder)
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
						clusterList, err := k8sutil.GetPeerClusterList(c.Config.KubeCli.CoreV1().RESTClient(), c.Config.Namespace)
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

				c.logger.Debugf("peer_cluster event: %v %v", ev.Type, ev.Object.Spec)

				watchVersion = ev.Object.Metadata.ResourceVersion
				eventCh <- ev
			}

			resp.Body.Close()
		}
	}()

	return eventCh, errCh
}

func (c *Controller) isClustersCacheStale(currentClusters []spec.PeerCluster) bool {
	if len(c.peerClusterRVs) != len(currentClusters) {
		return true
	}

	for _, cc := range currentClusters {
		rv, ok := c.peerClusterRVs[cc.Metadata.Name]
		if !ok || rv != cc.Metadata.ResourceVersion {
			return true
		}
	}

	return false
}

func (c *Controller) handleClusterEvent(event *PeersEvent) {
	clus := event.Object

	if clus.Status.IsFailed() {
		c.logger.Infof("ignore failed cluster (%s). Please delete its TPR", clus.Metadata.Name)
		return
	}

	clus.Spec.Cleanup()

	switch event.Type {
	case kwatch.Added:
		stopC := make(chan struct{})
		nc := peer_cluster.New(c.makeClusterConfig(), clus, stopC, &c.waitPeerCluster)

		c.stopChMap[clus.Metadata.Name] = stopC
		c.peerClusters[clus.Metadata.Name] = nc
		c.peerClusterRVs[clus.Metadata.Name] = clus.Metadata.ResourceVersion

	case kwatch.Modified:
		if _, ok := c.peerClusters[clus.Metadata.Name]; !ok {
			c.logger.Warningf("unsafe state. cluster was never created but we received event (%s)", event.Type)
			return
		}
		//c.peerClusters[clus.Metadata.Name].Update(clus)
		c.peerClusterRVs[clus.Metadata.Name] = clus.Metadata.ResourceVersion

	case kwatch.Deleted:
		if _, ok := c.peerClusters[clus.Metadata.Name]; !ok {
			c.logger.Warningf("unsafe state. cluster was never created but we received event (%s)", event.Type)
			return
		}
		//c.peerClusters[clus.Metadata.Name].Delete()
		delete(c.peerClusters, clus.Metadata.Name)
		delete(c.peerClusterRVs, clus.Metadata.Name)
	}
}

func (c *Controller) makeClusterConfig() peer_cluster.Config {
	return peer_cluster.Config{
		ServiceAccount: c.Config.ServiceAccount,

		KubeCli: c.KubeCli,
	}
}

func pollPeersEvent(decoder *json.Decoder) (*PeersEvent, *unversioned.Status, error) {
	re := &rawEvent{}
	err := decoder.Decode(re)
	if err != nil {
		if err == io.EOF {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("fail to decode raw event from apiserver (%v)", err)
	}

	if re.Type == kwatch.Error {
		status := &unversioned.Status{}
		err = json.Unmarshal(re.Object, status)
		if err != nil {
			return nil, nil, fmt.Errorf("fail to decode (%s) into unversioned.Status (%v)", re.Object, err)
		}
		return nil, status, nil
	}

	ev := &PeersEvent{
		Type:   re.Type,
		Object: &spec.PeerCluster{},
	}
	err = json.Unmarshal(re.Object, ev.Object)
	if err != nil {
		return nil, nil, fmt.Errorf("fail to unmarshal Chain object from data (%s): %v", re.Object, err)
	}
	return ev, nil, nil
}
