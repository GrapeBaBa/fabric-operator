// Copyright 2016 Kai Chen <281165273@qq.com> (@grapebaba)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	"github.com/grapebaba/fabric-operator/pkg/cluster"
	"github.com/grapebaba/fabric-operator/pkg/spec"
	"github.com/grapebaba/fabric-operator/pkg/util/k8sutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kwatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	v1beta1extensions "k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

var (
	ErrVersionOutdated = errors.New("requested version is outdated in apiserver")

	initRetryWaitTime = 30 * time.Second

	// Workaround for watching TPR resource.
	// client-go has encoding issue and we want something more predictable.
	KubeHttpCli *http.Client
	MasterHost  string
)

type ClusterEvent struct {
	Type   kwatch.EventType
	Object interface{}
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
	peerClusters map[string]*cluster.PeerCluster
	// Kubernetes resource version of the peerClusters
	peerClusterRVs map[string]string
	stopPCChMap    map[string]chan struct{}

	waitPeerCluster sync.WaitGroup

	ordererServices   map[string]*cluster.OrdererService
	ordererServiceRVs map[string]string
	stopOSChMap       map[string]chan struct{}

	waitOrdererService sync.WaitGroup
}

func New(cfg Config) *Controller {
	return &Controller{
		logger: logrus.WithField("pkg", "controller"),

		Config:            cfg,
		peerClusters:      make(map[string]*cluster.PeerCluster),
		peerClusterRVs:    make(map[string]string),
		stopPCChMap:       map[string]chan struct{}{},
		ordererServices:   make(map[string]*cluster.OrdererService),
		ordererServiceRVs: make(map[string]string),
		stopOSChMap:       map[string]chan struct{}{},
	}
}

func (c *Config) Validate() error {
	return nil
}

func (c *Controller) Run() error {
	var (
		peerClusterWatchVersion string
		ordererServiceWatchVersion string
		err                     error
	)

	for {
		peerClusterWatchVersion, err = c.initPeerClusterResource()
		if err == nil {
			break
		}
		c.logger.Errorf("initialization failed: %v", err)
		c.logger.Infof("retry in %v...", initRetryWaitTime)
		time.Sleep(initRetryWaitTime)
		// todo: add max retry?
	}
	c.logger.Infof("starts running from peer cluster watch version: %s", peerClusterWatchVersion)

	for {
		ordererServiceWatchVersion, err = c.initOrdererServiceResource()
		if err == nil {
			break
		}
		c.logger.Errorf("initialization failed: %v", err)
		c.logger.Infof("retry in %v...", initRetryWaitTime)
		time.Sleep(initRetryWaitTime)
		// todo: add max retry?
	}
	c.logger.Infof("starts running from orderer service watch version: %s", ordererServiceWatchVersion)

	defer func() {
		for _, stopC := range c.stopPCChMap {
			close(stopC)
		}
		c.waitPeerCluster.Wait()
	}()

	defer func() {
		for _, stopC := range c.stopOSChMap {
			close(stopC)
		}
		c.waitOrdererService.Wait()
	}()

	eventCh := make(chan *ClusterEvent)
	// On unexpected error case, controller should exit
	errCh := make(chan error, 1)
	c.watchPeerClusterTPR(peerClusterWatchVersion, eventCh, errCh)
	c.watchOrdererServiceTPR(peerClusterWatchVersion, eventCh, errCh)
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

func (c *Controller) initPeerClusterResource() (string, error) {
	watchVersion := "0"
	err := c.createPeerClusterTPR()
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

func (c *Controller) initOrdererServiceResource() (string, error) {
	watchVersion := "0"
	err := c.createOrdererServiceTPR()
	if err != nil {
		if k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			// TPR has been initialized before. We need to recover existing cluster.
			watchVersion, err = c.findAllOrdererServices()
			if err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("fail to create TPR: %v", err)
		}
	}

	return watchVersion, nil
}


func (c *Controller) createPeerClusterTPR() error {
	tpr := &v1beta1extensions.ThirdPartyResource{
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.TPRPeerClusterName,
		},
		Versions: []v1beta1extensions.APIVersion{
			{Name: spec.TPRVersion},
		},
		Description: spec.TPRPeerClusterDescription,
	}
	_, err := c.KubeCli.ExtensionsV1beta1().ThirdPartyResources().Create(tpr)
	if err != nil {
		return err
	}

	return k8sutil.WaitTPRReady(c.KubeCli.CoreV1().RESTClient(), 3*time.Second, 30*time.Second, c.Namespace, spec.TPRPeerClusterURI)
}

func (c *Controller) createOrdererServiceTPR() error {
	tpr := &v1beta1extensions.ThirdPartyResource{
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.TPROrdererServiceName,
		},
		Versions: []v1beta1extensions.APIVersion{
			{Name: spec.TPRVersion},
		},
		Description: spec.TPROrdererServiceDescription,
	}
	_, err := c.KubeCli.ExtensionsV1beta1().ThirdPartyResources().Create(tpr)
	if err != nil {
		return err
	}

	return k8sutil.WaitTPRReady(c.KubeCli.CoreV1().RESTClient(), 3*time.Second, 30*time.Second, c.Namespace, spec.TPROrdererServiceURI)
}

func (c *Controller) findAllPeerClusters() (string, error) {
	c.logger.Info("finding existing peer clusters...")
	clusterList, err := k8sutil.GetTPRList(c.Config.KubeCli.CoreV1().RESTClient(), c.Config.Namespace, spec.TPRPeerClusterURI, &spec.PeerClusterList{})
	if err != nil {
		return "", err
	}

	//TODO: Recover existing peerClusters
	peerClusterList := clusterList.(*spec.PeerClusterList)
	for i := range peerClusterList.Items {
		peerCluster := peerClusterList.Items[i]

		if peerCluster.Status.IsFailed() {
			c.logger.Infof("ignore failed peerCluster (%s). Please delete its TPR", peerCluster.Metadata.Name)
			continue
		}

		peerCluster.Spec.Cleanup()

		stopC := make(chan struct{})
		nc := cluster.NewPeerCluster(c.makePeerClusterConfig(), &peerCluster, stopC, &c.waitPeerCluster)
		c.stopPCChMap[peerCluster.Metadata.Name] = stopC
		c.peerClusters[peerCluster.Metadata.Name] = nc
		c.peerClusterRVs[peerCluster.Metadata.Name] = peerCluster.Metadata.ResourceVersion
	}

	return peerClusterList.Metadata.ResourceVersion, nil
}

func (c *Controller) findAllOrdererServices() (string, error) {
	c.logger.Info("finding existing orderer services...")
	clusterList, err := k8sutil.GetTPRList(c.Config.KubeCli.CoreV1().RESTClient(), c.Config.Namespace, spec.TPROrdererServiceURI, &spec.OrdererServiceList{})
	if err != nil {
		return "", err
	}

	//TODO: Recover existing peerClusters
	ordererServiceList := clusterList.(*spec.OrdererServiceList)
	for i := range ordererServiceList.Items {
		ordererService := ordererServiceList.Items[i]

		if ordererService.Status.IsFailed() {
			c.logger.Infof("ignore failed orderer service (%s). Please delete its TPR", ordererService.Metadata.Name)
			continue
		}

		ordererService.Spec.Cleanup()

		stopC := make(chan struct{})
		nc := cluster.NewOrdererService(c.makeOrdererServiceConfig(), &ordererService, stopC, &c.waitOrdererService)
		c.stopOSChMap[ordererService.Metadata.Name] = stopC
		c.ordererServices[ordererService.Metadata.Name] = nc
		c.ordererServiceRVs[ordererService.Metadata.Name] = ordererService.Metadata.ResourceVersion
	}

	return ordererServiceList.Metadata.ResourceVersion, nil
}


// watchPeerClusterTPR creates a go routine, and watches the peerClusters kind resources from
// the given watch version. It emits events on the resources through the returned
// event chan. Errors will be reported through the returned error chan. The go routine
// exits on any error.
func (c *Controller) watchPeerClusterTPR(watchVersion string, eventCh chan<- *ClusterEvent, errCh chan<- error) {

	go func() {
		defer close(eventCh)

		for {
			resp, err := k8sutil.WatchTPR(MasterHost, c.Config.Namespace, spec.TPRPeerClusterURI, KubeHttpCli, watchVersion)
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
				ev, st, err := pollEvent(decoder, &spec.PeerCluster{})
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
						clusterList, err := k8sutil.GetTPRList(c.Config.KubeCli.CoreV1().RESTClient(), c.Config.Namespace, spec.TPRPeerClusterURI, &spec.PeerClusterList{})
						if err == nil {
							if peerClusterList := clusterList.(*spec.PeerClusterList); !c.isPeerClustersCacheStale(peerClusterList.Items) {
								watchVersion = peerClusterList.Metadata.ResourceVersion
								break
							}
						}

						// if anything has changed (or error on relist), we have to rebuild the state.
						// go to recovery path
						errCh <- ErrVersionOutdated
						return
					}

					c.logger.Fatalf("unexpected status response from API server: %v", st.Message)
				}

				peerCluster := ev.Object.(&spec.PeerCluster)
				c.logger.Debugf("peer cluster event: %v %v", ev.Type, peerCluster.Spec)

				watchVersion = peerCluster.Metadata.ResourceVersion
				eventCh <- ev
			}

			resp.Body.Close()
		}
	}()

}

// watchOrdererServiceTPR creates a go routine, and watches the peerClusters kind resources from
// the given watch version. It emits events on the resources through the returned
// event chan. Errors will be reported through the returned error chan. The go routine
// exits on any error.
func (c *Controller) watchOrdererServiceTPR(watchVersion string, eventCh chan<- *ClusterEvent, errCh chan<- error) {

	go func() {
		defer close(eventCh)

		for {
			resp, err := k8sutil.WatchTPR(MasterHost, c.Config.Namespace, spec.TPROrdererServiceURI, KubeHttpCli, watchVersion)
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
				ev, st, err := pollEvent(decoder, &spec.OrdererService{})
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
						clusterList, err := k8sutil.GetTPRList(c.Config.KubeCli.CoreV1().RESTClient(), c.Config.Namespace, spec.TPROrdererServiceURI, &spec.OrdererServiceList{})
						if err == nil {
							if ordererServiceList := clusterList.(*spec.OrdererServiceList); !c.isOrdererServicesCacheStale(ordererServiceList.Items) {
								watchVersion = ordererServiceList.Metadata.ResourceVersion
								break
							}
						}

						// if anything has changed (or error on relist), we have to rebuild the state.
						// go to recovery path
						errCh <- ErrVersionOutdated
						return
					}

					c.logger.Fatalf("unexpected status response from API server: %v", st.Message)
				}

				ordererService := ev.Object.(&spec.OrdererService{})
				c.logger.Debugf("peer cluster event: %v %v", ev.Type, ordererService.Spec)

				watchVersion = ordererService.Metadata.ResourceVersion
				eventCh <- ev
			}

			resp.Body.Close()
		}
	}()

}

func (c *Controller) isPeerClustersCacheStale(currentClusters []spec.PeerCluster) bool {
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

func (c *Controller) isOrdererServicesCacheStale(currentClusters []spec.OrdererService) bool {
	if len(c.peerClusterRVs) != len(currentClusters) {
		return true
	}

	for _, cc := range currentClusters {
		rv, ok := c.ordererServiceRVs[cc.Metadata.Name]
		if !ok || rv != cc.Metadata.ResourceVersion {
			return true
		}
	}

	return false
}

func (c *Controller) handleClusterEvent(event *ClusterEvent) {
	clus := event.Object

	switch clus.(type) {
	case *spec.PeerCluster:
		peerCluster := clus.(&spec.PeerCluster)
		if peerCluster.Status.IsFailed() {
			c.logger.Infof("ignore failed cluster (%s). Please delete its TPR", peerCluster.Metadata.Name)
			return
		}

		peerCluster.Spec.Cleanup()

		switch event.Type {
		case kwatch.Added:
			stopC := make(chan struct{})
			nc := cluster.NewPeerCluster(c.makePeerClusterConfig(), peerCluster, stopC, &c.waitPeerCluster)

			c.stopPCChMap[peerCluster.Metadata.Name] = stopC
			c.peerClusters[peerCluster.Metadata.Name] = nc
			c.peerClusterRVs[peerCluster.Metadata.Name] = peerCluster.Metadata.ResourceVersion

		case kwatch.Modified:
			if _, ok := c.peerClusters[peerCluster.Metadata.Name]; !ok {
				c.logger.Warningf("unsafe state. cluster was never created but we received event (%s)", event.Type)
				return
			}
			//c.peerClusters[clus.Metadata.Name].Update(clus)
			c.peerClusterRVs[peerCluster.Metadata.Name] = peerCluster.Metadata.ResourceVersion

		case kwatch.Deleted:
			if _, ok := c.peerClusters[peerCluster.Metadata.Name]; !ok {
				c.logger.Warningf("unsafe state. cluster was never created but we received event (%s)", event.Type)
				return
			}
			//c.peerClusters[clus.Metadata.Name].Delete()
			delete(c.peerClusters, peerCluster.Metadata.Name)
			delete(c.peerClusterRVs, peerCluster.Metadata.Name)
		}
	case *spec.OrdererService:
		ordererService := clus.(&spec.OrdererService)
		if ordererService.Status.IsFailed() {
			c.logger.Infof("ignore failed cluster (%s). Please delete its TPR", ordererService.Metadata.Name)
			return
		}

		ordererService.Spec.Cleanup()

		switch event.Type {
		case kwatch.Added:
			stopC := make(chan struct{})
			//nc := cluster.NewPeerCluster(c.makeOrdererServiceConfig(), ordererService, stopC, &c.waitPeerCluster)

			c.stopOSChMap[ordererService.Metadata.Name] = stopC
			c.ordererServices[ordererService.Metadata.Name] = nil
			c.ordererServiceRVs[ordererService.Metadata.Name] = ordererService.Metadata.ResourceVersion

		case kwatch.Modified:
			if _, ok := c.ordererServices[ordererService.Metadata.Name]; !ok {
				c.logger.Warningf("unsafe state. cluster was never created but we received event (%s)", event.Type)
				return
			}
			//c.peerClusters[clus.Metadata.Name].Update(clus)
			c.ordererServiceRVs[ordererService.Metadata.Name] = ordererService.Metadata.ResourceVersion

		case kwatch.Deleted:
			if _, ok := c.ordererServices[ordererService.Metadata.Name]; !ok {
				c.logger.Warningf("unsafe state. cluster was never created but we received event (%s)", event.Type)
				return
			}
			//c.peerClusters[clus.Metadata.Name].Delete()
			delete(c.ordererServices, ordererService.Metadata.Name)
			delete(c.ordererServiceRVs, ordererService.Metadata.Name)
		}
	}

}

func (c *Controller) makePeerClusterConfig() cluster.PeerClusterConfig {
	return cluster.PeerClusterConfig{
		ServiceAccount: c.Config.ServiceAccount,

		KubeCli: c.KubeCli,
	}
}

func (c *Controller) makeOrdererServiceConfig() cluster.OrdererServiceConfig {
	return cluster.OrdererServiceConfig{
		ServiceAccount: c.Config.ServiceAccount,

		KubeCli: c.KubeCli,
	}
}

func pollEvent(decoder *json.Decoder, v interface{}) (*ClusterEvent, *metav1.Status, error) {
	re := &rawEvent{}
	err := decoder.Decode(re)
	if err != nil {
		if err == io.EOF {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("fail to decode raw event from apiserver (%v)", err)
	}

	if re.Type == kwatch.Error {
		status := &metav1.Status{}
		err = json.Unmarshal(re.Object, status)
		if err != nil {
			return nil, nil, fmt.Errorf("fail to decode (%s) into unversioned.Status (%v)", re.Object, err)
		}
		return nil, status, nil
	}

	ev := &ClusterEvent{
		Type:   re.Type,
		Object: v,
	}
	err = json.Unmarshal(re.Object, ev.Object)
	if err != nil {
		return nil, nil, fmt.Errorf("fail to unmarshal Chain object from data (%s): %v", re.Object, err)
	}
	return ev, nil, nil
}
