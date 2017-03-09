package controller

import (
	"errors"
	"time"
	"net/http"
	"k8s.io/client-go/kubernetes"
	"fmt"
	"github.com/Sirupsen/logrus"
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
