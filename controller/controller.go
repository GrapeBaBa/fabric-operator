package controller

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/grapebaba/fabric-operator/spec"
	"github.com/grapebaba/fabric-operator/util/k8sutil"
	v1beta1extensions "k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
)

var (
	supportedPVProvisioners = map[string]struct{}{
		"kubernetes.io/gce-pd":  {},
		"kubernetes.io/aws-ebs": {},
	}

	ErrVersionOutdated = errors.New("requested version is outdated in apiserver")

	initRetryWaitTime = 30 * time.Second

	// Workaround for watching TPR resource.
	// client-go has encoding issue and we want something more predictable.
	KubeHttpCli *http.Client
	MasterHost  string
)

type Config struct {
	Namespace      string
	ServiceAccount string
	PVProvisioner  string
	KubeCli        kubernetes.Interface
}

func (c *Config) Validate() error {
	if _, ok := supportedPVProvisioners[c.PVProvisioner]; !ok {
		return fmt.Errorf(
			"persistent volume provisioner %s is not supported: options = %v",
			c.PVProvisioner, supportedPVProvisioners,
		)
	}

	return nil
}

type Controller struct {
	logger *logrus.Entry
	Config

	//// TODO: combine the three cluster map.
	//clusters map[string]*cluster.Cluster
	//// Kubernetes resource version of the clusters
	//clusterRVs map[string]string
	//stopChMap  map[string]chan struct{}

	//waitCluster sync.WaitGroup
}

func New(cfg Config) *Controller {
	return &Controller{Config: cfg}
	//return &Controller{
	//	logger: logrus.WithField("pkg", "controller"),
	//
	//	Config:     cfg,
	//	clusters:   make(map[string]*cluster.Cluster),
	//	clusterRVs: make(map[string]string),
	//	stopChMap:  map[string]chan struct{}{},
	//}
}

func (c *Controller) Run() error {
	return nil
	//var (
	//	watchVersion string
	//	err          error
	//)

	//for {
	//	watchVersion, err = c.initResource()
	//	if err == nil {
	//		break
	//	}
	//	c.logger.Errorf("initialization failed: %v", err)
	//	c.logger.Infof("retry in %v...", initRetryWaitTime)
	//	time.Sleep(initRetryWaitTime)
	//	// todo: add max retry?
	//}
	//
	//c.logger.Infof("starts running from watch version: %s", watchVersion)
	//
	//defer func() {
	//	for _, stopC := range c.stopChMap {
	//		close(stopC)
	//	}
	//	c.waitCluster.Wait()
	//}()
	//
	//eventCh, errCh := c.watch(watchVersion)
	//
	//go func() {
	//	pt := newPanicTimer(time.Minute, "unexpected long blocking (> 1 Minute) when handling cluster event")
	//
	//	for ev := range eventCh {
	//		pt.start()
	//		c.handleClusterEvent(ev)
	//		pt.stop()
	//	}
	//}()
	//return <-errCh
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
	err = k8sutil.CreateStorageClass(c.KubeCli, c.PVProvisioner)
	if err != nil {
		if !k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			return "", fmt.Errorf("fail to create storage class: %v", err)
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

	for i := range chainList.Items {
		chain := chainList.Items[i]

		if chain.Status.IsFailed() {
			c.logger.Infof("ignore failed chain (%s). Please delete its TPR", chain.Metadata.Name)
			continue
		}

		chain.Spec.Cleanup()

		stopC := make(chan struct{})
		nc := cluster.New(c.makeClusterConfig(), &chain, stopC, &c.waitCluster)
		c.stopChMap[chain.Metadata.Name] = stopC
		c.clusters[chain.Metadata.Name] = nc
		c.clusterRVs[chain.Metadata.Name] = chain.Metadata.ResourceVersion
	}

	return chainList.Metadata.ResourceVersion, nil
}
