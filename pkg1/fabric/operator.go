// Copyright 2016 The prometheus-operator Authors
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

package fabric

import (
	"fmt"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/grapebaba/fabric-operator/pkg1/client/v1alpha1"
	"github.com/grapebaba/fabric-operator/util/k8sutil"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	tprOrdererService = "orderer-service." + v1alpha1.TPRGroup
	tprPeerCluster     = "peer-cluster." + v1alpha1.TPRGroup

	resyncPeriod = 5 * time.Minute
)

// Operator manages lify cycle of Prometheus deployments and
// monitoring configurations.
type Operator struct {
	kclient *kubernetes.Clientset
	mclient *v1alpha1.FabricV1alpha1Client
	logger  *logrus.Entry

	pcInf cache.SharedIndexInformer
	osInf cache.SharedIndexInformer

	queue workqueue.RateLimitingInterface
	config                 Config
}

// Config defines configuration parameters for the Operator.
type Config struct {
	Namespace string

	Name string
}

// New creates a new controller.
func New(conf Config) (*Operator, error) {
	cfg, err := k8sutil.InClusterConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	mclient, err := v1alpha1.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	c := &Operator{
		kclient:                client,
		mclient:                mclient,
		logger:                 logrus.WithField("fabric", "operator"),
		queue:                  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "fabric"),
		config:                 conf,
	}

	c.pcInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc:  mclient.PeerCluster(api.NamespaceAll).List,
			WatchFunc: mclient.PeerCluster(api.NamespaceAll).Watch,
		},
		&v1alpha1.PeerCluster{}, resyncPeriod, cache.Indexers{},
	)
	c.pcInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handleAddPeerCluster,
		DeleteFunc: c.handleDeletePeerCluster,
		UpdateFunc: c.handleUpdatePeerCluster,
	})

	c.osInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc:  mclient.OrdererServices(api.NamespaceAll).List,
			WatchFunc: mclient.OrdererServices(api.NamespaceAll).Watch,
		},
		&v1alpha1.OrdererService{}, resyncPeriod, cache.Indexers{},
	)
	c.osInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handleAddOrdererService,
		DeleteFunc: c.handleDeleteOrdererService,
		UpdateFunc: c.handleUpdateOrdererService,
	})

	return c, nil
}

// Run the controller.
func (c *Operator) Run(stopc <-chan struct{}) error {
	defer c.queue.ShutDown()

	errChan := make(chan error)
	go func() {
		v, err := c.kclient.Discovery().ServerVersion()
		if err != nil {
			errChan <- errors.Wrap(err, "communicating with server failed")
			return
		}
		c.logger.Infof("connection established with cluster-version: %v",v)

		if err := c.createTPRs(); err != nil {
			errChan <- errors.Wrap(err, "creating TPRs failed")
			return
		}
		errChan <- nil
	}()

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
		c.logger.Info("TPR API endpoints ready")
	case <-stopc:
		return nil
	}

	go c.worker()

	go c.pcInf.Run(stopc)
	go c.osInf.Run(stopc)

	<-stopc
	return nil
}

func (c *Operator) keyFunc(obj interface{}) (string, bool) {
	k, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		c.logger.Errorf("creating key failed err: %v", err)
		return k, false
	}
	return k, true
}

func (c *Operator) handleAddPeerCluster(obj interface{}) {
	key, ok := c.keyFunc(obj)
	if !ok {
		return
	}

	c.logger.Infof("peer cluster added key: %v",key)
	c.enqueue(key)
}

func (c *Operator) handleDeletePeerCluster(obj interface{}) {
	key, ok := c.keyFunc(obj)
	if !ok {
		return
	}

	c.logger.Infof("peer cluster deleted key: %v",key)
	c.enqueue(key)
}

func (c *Operator) handleUpdatePeerCluster(old, cur interface{}) {
	key, ok := c.keyFunc(cur)
	if !ok {
		return
	}

	c.logger.Infof("peer cluster updated key: %v",key)
	c.enqueue(key)
}

func (c *Operator) handleAddOrdererService(obj interface{}) {
	key, ok := c.keyFunc(obj)
	if !ok {
		return
	}

	c.logger.Infof("orderer service added key: %v",key)
	c.enqueue(key)
}

func (c *Operator) handleUpdateOrdererService(old, cur interface{}) {
	key, ok := c.keyFunc(cur)
	if !ok {
		return
	}

	c.logger.Infof("orderer service updated key: %v",key)
	c.enqueue(key)
}

func (c *Operator) handleDeleteOrdererService(obj interface{}) {
	key, ok := c.keyFunc(obj)
	if !ok {
		return
	}

	c.logger.Infof("orderer service deleted key: %v",key)
	c.enqueue(key)
}

// enqueue adds a key to the queue. If obj is a key already it gets added directly.
// Otherwise, the key is extracted via keyFunc.
func (c *Operator) enqueue(obj interface{}) {
	if obj == nil {
		return
	}

	key, ok := obj.(string)
	if !ok {
		key, ok = c.keyFunc(obj)
		if !ok {
			return
		}
	}

	c.queue.Add(key)
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *Operator) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Operator) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.sync(key.(string))
	if err == nil {
		c.queue.Forget(key)
		return true
	}

	runtime.HandleError(errors.Wrap(err, fmt.Sprintf("Sync %q failed", key)))
	c.queue.AddRateLimited(key)

	return true
}

func (c *Operator) sync(key string) error {

	return nil
}


// syncVersion ensures that all running pods for a Prometheus have the required version.
// It kills pods with the wrong version one-after-one and lets the StatefulSet controller
// create new pods.
//
// TODO(fabxc): remove this once the StatefulSet controller learns how to do rolling updates.
func (c *Operator) syncVersion(key string, p *v1alpha1.PeerCluster) error {

	return nil
}


func (c *Operator) destroyPrometheus(key string) error {

	return nil
}

func (c *Operator) createTPRs() error {
	tprs := []*v1beta1.ThirdPartyResource{
		{
			ObjectMeta: v1.ObjectMeta{
				Name: tprPeerCluster,
			},
			Versions: []v1beta1.APIVersion{
				{Name: v1alpha1.TPRVersion},
			},
			Description: "Manage peer cluster",
		},
		{
			ObjectMeta: v1.ObjectMeta{
				Name: tprOrdererService,
			},
			Versions: []v1beta1.APIVersion{
				{Name: v1alpha1.TPRVersion},
			},
			Description: "Managed orderer service",
		},
	}
	tprClient := c.kclient.ExtensionsV1beta1().ThirdPartyResources()

	for _, tpr := range tprs {
		if _, err := tprClient.Create(tpr); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		c.logger.Infof("TPR created: %s", tpr.Name)
	}

	// We have to wait for the TPRs to be ready. Otherwise the initial watch may fail.
	err := k8sutil.WaitForTPRReady(c.kclient.CoreV1().RESTClient(), v1alpha1.TPRGroup, v1alpha1.TPRVersion, v1alpha1.TPRPeerClusterName)
	if err != nil {
		return err
	}
	return k8sutil.WaitForTPRReady(c.kclient.CoreV1().RESTClient(), v1alpha1.TPRGroup, v1alpha1.TPRVersion, v1alpha1.TPROrdererServiceName)
}
