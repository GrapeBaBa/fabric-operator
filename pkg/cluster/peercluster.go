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

package cluster

import (
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/grapebaba/fabric-operator/pkg/spec"
	"github.com/grapebaba/fabric-operator/pkg/util/fabricutil"
	"github.com/grapebaba/fabric-operator/pkg/util/k8sutil"
	"github.com/grapebaba/fabric-operator/pkg/util/retryutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
)

var (
	pcReconcileInterval         = 8 * time.Second
	pcPodTerminationGracePeriod = int64(5)
)

type peerClusterEventType string

const (
	eventDeleteCluster peerClusterEventType = "Delete"
	eventModifyCluster peerClusterEventType = "Modify"
)

type peerClusterEvent struct {
	typ     peerClusterEventType
	cluster *spec.PeerCluster
}

type PeerClusterConfig struct {
	ServiceAccount string

	KubeCli kubernetes.Interface
}

type PeerCluster struct {
	logger *logrus.Entry

	config PeerClusterConfig

	cluster *spec.PeerCluster

	// in memory state of the cluster
	// status is the source of truth after OrdererService struct is materialized.
	status        spec.ClusterStatus
	memberCounter int

	eventCh chan *peerClusterEvent
	stopCh  chan struct{}

	// members represents the members in the peer cluster.
	// the name of the member is the the name of the pod the member
	// process runs in.
	members fabricutil.MemberSet
}

func NewPeerCluster(config PeerClusterConfig, cl *spec.PeerCluster, stopC <-chan struct{}, wg *sync.WaitGroup) *PeerCluster {
	lg := logrus.WithField("pkg", "peer-cluster").WithField("peer-cluster-name", cl.Metadata.Name)
	c := &PeerCluster{
		logger:  lg,
		config:  config,
		cluster: cl,
		eventCh: make(chan *peerClusterEvent, 100),
		stopCh:  make(chan struct{}),
		status:  cl.Status.Copy(),
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := c.setup(); err != nil {
			c.logger.Errorf("peer cluster failed to setup: %v", err)
			if c.status.Phase != spec.ClusterPhaseFailed {
				c.status.SetReason(err.Error())
				c.status.SetPhase(spec.ClusterPhaseFailed)
				if err := c.updateTPRStatus(); err != nil {
					c.logger.Errorf("failed to update cluster phase (%v): %v", spec.ClusterPhaseFailed, err)
				}
			}
			return
		}
		c.run(stopC)
	}()

	return c
}

func (c *PeerCluster) setup() error {
	err := c.cluster.Spec.Validate()
	if err != nil {
		return fmt.Errorf("invalid cluster spec: %v", err)
	}

	var shouldCreateCluster bool
	switch c.status.Phase {
	case spec.ClusterPhaseNone:
		shouldCreateCluster = true
	case spec.ClusterPhaseCreating:
		return errCreatedCluster
	case spec.ClusterPhaseRunning:
		shouldCreateCluster = false

	default:
		return fmt.Errorf("unexpected cluster phase: %s", c.status.Phase)
	}

	if shouldCreateCluster {
		return c.create()
	}
	return nil
}

func (c *PeerCluster) create() error {
	c.status.SetPhase(spec.ClusterPhaseCreating)

	if err := c.updateTPRStatus(); err != nil {
		return fmt.Errorf("peer cluster create: failed to update cluster phase (%v): %v", spec.ClusterPhaseCreating, err)
	}
	c.logger.Infof("creating peer cluster with Spec (%#v), Status (%#v)", c.cluster.Spec, c.cluster.Status)

	// Note: For restore case, we don't need to create seed member,
	// and will go through reconcile loop and disaster recovery.
	if err := c.setupMembers(); err != nil {
		return err
	}

	if err := c.setupService(); err != nil {
		return fmt.Errorf("cluster create: fail to create peer cluster service: %v", err)
	}
	return nil
}

func (c *PeerCluster) setupMembers() error {
	peerSize := len(c.cluster.Spec.Peers)
	c.status.AppendScalingUpCondition(0, peerSize)

	err := c.bootstrap()

	if err != nil {
		return err
	}

	c.status.Size = peerSize
	return nil
}

//func (c *OrdererService) Delete() {
//	c.send(&ordererServiceEvent{typ: eventDeleteCluster})
//}
//
//func (c *OrdererService) send(ev *ordererServiceEvent) {
//	select {
//	case c.eventCh <- ev:
//		l, ecap := len(c.eventCh), cap(c.eventCh)
//		if l > int(float64(ecap)*0.8) {
//			c.logger.Warningf("eventCh buffer is almost full [%d/%d]", l, ecap)
//		}
//	case <-c.stopCh:
//	}
//}
//
func (c *PeerCluster) run(stopC <-chan struct{}) {
	clusterFailed := false

	defer func() {
		if clusterFailed {
			c.reportFailedStatus()

			c.logger.Infof("deleting the failed cluster")
			c.delete()
		}

		close(c.stopCh)
	}()

	c.status.SetPhase(spec.ClusterPhaseRunning)
	if err := c.updateTPRStatus(); err != nil {
		c.logger.Warningf("failed to update TPR status: %v", err)
	}
	c.logger.Infof("start running...")

	var rerr error
	for {
		select {
		case <-stopC:
			return
		case event := <-c.eventCh:
			switch event.typ {
			case eventModifyCluster:
				if isSpecEqual(event.cluster.Spec, c.cluster.Spec) {
					break
				}
				// TODO: we can't handle another upgrade while an upgrade is in progress
				c.logger.Infof("spec update: from: %v to: %v", c.cluster.Spec, event.cluster.Spec)
				c.cluster = event.cluster

			case eventDeleteCluster:
				c.logger.Infof("cluster is deleted by the user")
				clusterFailed = true
				return
			}

			//case <-time.After(reconcileInterval):
			//	if c.cluster.Spec.Paused {
			//		c.status.PauseControl()
			//		c.logger.Infof("control is paused, skipping reconcilation")
			//		continue
			//	} else {
			//		c.status.Control()
			//	}
			//
			//	running, pending, err := c.pollPods()
			//	if err != nil {
			//		c.logger.Errorf("fail to poll pods: %v", err)
			//		continue
			//	}
			//
			//	if len(pending) > 0 {
			//		// Pod startup might take long, e.g. pulling image. It would deterministically become running or succeeded/failed later.
			//		c.logger.Infof("skip reconciliation: running (%v), pending (%v)", k8sutil.GetPodNames(running), k8sutil.GetPodNames(pending))
			//		continue
			//	}
			//	if len(running) == 0 {
			//		c.logger.Warningf("all etcd pods are dead. Trying to recover from a previous backup")
			//		rerr = c.disasterRecovery(nil)
			//		if rerr != nil {
			//			c.logger.Errorf("fail to do disaster recovery: %v", rerr)
			//		}
			//		// On normal recovery case, we need backoff. On error case, this could be either backoff or leading to cluster delete.
			//		break
			//	}
			//
			//	// On controller restore, we could have "members == nil"
			//	if rerr != nil || c.members == nil {
			//		rerr = c.updateMembers(podsToMemberSet(running, c.cluster.Spec.SelfHosted))
			//		if rerr != nil {
			//			c.logger.Errorf("failed to update members: %v", rerr)
			//			break
			//		}
			//	}
			//	rerr = c.reconcile(running)
			//	if rerr != nil {
			//		c.logger.Errorf("failed to reconcile: %v", rerr)
			//		break
			//	}
			//
			//	if err := c.updateLocalBackupStatus(); err != nil {
			//		c.logger.Warningf("failed to update local backup service status: %v", err)
			//	}
			//	c.updateMemberStatus(running)
			//	if err := c.updateTPRStatus(); err != nil {
			//		c.logger.Warningf("failed to update TPR status: %v", err)
			//	}
		}

		if isFatalError(rerr) {
			clusterFailed = true
			c.status.SetReason(rerr.Error())

			c.logger.Errorf("cluster failed: %v", rerr)
			return
		}
	}
}

func isSpecEqual(s1, s2 spec.PeerClusterSpec) bool {
	if len(s1.Peers) != len(s2.Peers) || s1.Paused != s2.Paused || s1.Version != s2.Version {
		return false
	}
	return true
}

// bootstrap creates the peer members of peer cluster.
func (c *PeerCluster) bootstrap() error {
	ms := fabricutil.NewMemberSet()
	for i, peer := range c.cluster.Spec.Peers {
		m := &fabricutil.Member{
			Name:       fabricutil.CreateMemberName(c.cluster.Metadata.Name, i),
			SecretName: fabricutil.CreateMemberSecretName(c.cluster.Metadata.Name, i),
			ConfigName: fabricutil.CreateMemberConfigName(c.cluster.Metadata.Name, i),
			Namespace:  c.cluster.Metadata.Namespace,
			OrgMSPId:   peer.Identity.OrgMSPId,
		}
		secretData := make(map[string][]byte)
		keyToPaths := []v1.KeyToPath{}
		for k, v := range peer.Identity.MSP.AdminCerts {
			secretData["admincerts-"+k] = v
			keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "admincerts-" + k, Path: "msp/admincerts/" + k})
		}
		for k, v := range peer.Identity.MSP.CACerts {
			secretData["cacerts-"+k] = v
			keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "cacerts-" + k, Path: "msp/cacerts/" + k})
		}
		for k, v := range peer.Identity.MSP.KeyStore {
			secretData["keystore-"+k] = v
			keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "keystore-" + k, Path: "msp/keystore/" + k})
		}
		for k, v := range peer.Identity.MSP.SignCerts {
			secretData["signcerts-"+k] = v
			keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "signcerts-" + k, Path: "msp/signcerts/" + k})
		}
		if peer.Identity.MSP.IntermediateCerts != nil {
			for k, v := range peer.Identity.MSP.IntermediateCerts {
				secretData["intermediatecerts-"+k] = v
				keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "intermediatecerts-" + k, Path: "msp/intermediatecerts/" + k})
			}
		}

		if peer.TLS != nil {
			if peer.TLS.PeerCert != nil {
				secretData["peercert.pem"] = peer.TLS.PeerCert
				keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "peercert.pem", Path: "tls/peercert.pem"})
			}
			if peer.TLS.PeerKey != nil {
				secretData["peerkey.pem"] = peer.TLS.PeerKey
				keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "peerkey.pem", Path: "tls/peerkey.pem"})
			}
			if peer.TLS.PeerRootCert != nil {
				secretData["peerrootcert.pem"] = peer.TLS.PeerRootCert
				keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "peerrootcert.pem", Path: "tls/peerrootcert.pem"})
			}
			if peer.TLS.VMCert != nil {
				secretData["vmcert.pem"] = peer.TLS.VMCert
				keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "vmcert.pem", Path: "tls/vmcert.pem"})
			}
			if peer.TLS.VMKey != nil {
				secretData["vmkey.pem"] = peer.TLS.VMKey
				keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "vmkey.pem", Path: "tls/vmkey.pem"})
			}
			if peer.TLS.VMRootCert != nil {
				secretData["vmrootcert.pem"] = peer.TLS.VMRootCert
				keyToPaths = append(keyToPaths, v1.KeyToPath{Key: "vmrootcert.pem", Path: "tls/vmrootcert.pem"})

			}
		}

		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: m.SecretName,
			},
			Data: secretData,
		}

		_, err := k8sutil.CreateMemberSecret(c.config.KubeCli, c.cluster.Metadata.Namespace, secret)
		if err != nil {
			return nil
		}

		err = c.createPod(m, keyToPaths)
		if err != nil {
			return nil
		}
		ms[m.Name] = m
		c.memberCounter++
	}
	c.members = ms
	return nil
}

//
//// recover recovers the cluster by creating a seed etcd member from a backup.
//func (c *OrdererService) recover() error {
//	return c.startSeedMember(true)
//}
//
//func (c *OrdererService) Update(cl *spec.Cluster) {
//	c.send(&ordererServiceEvent{
//		typ:     eventModifyCluster,
//		cluster: cl,
//	})
//}
//
func (c *PeerCluster) delete() {
	c.logger.Info("cluster deletion")
}

func (c *PeerCluster) setupService() error {
	return k8sutil.CreatePeerService(c.config.KubeCli, c.cluster.Metadata.Name, c.cluster.Metadata.Namespace, c.cluster.AsOwner())
}

//
//func (c *OrdererService) deleteClientServiceLB() error {
//	err := c.config.KubeCli.Core().Services(c.cluster.Metadata.Namespace).Delete(k8sutil.ClientServiceName(c.cluster.Metadata.Name), nil)
//	if err != nil {
//		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
//			return err
//		}
//	}
//	err = c.config.KubeCli.Core().Services(c.cluster.Metadata.Namespace).Delete(c.cluster.Metadata.Name, nil)
//	if err != nil {
//		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
//			return err
//		}
//	}
//	return nil
//}
//
func (c *PeerCluster) createPod(m *fabricutil.Member, k2p []v1.KeyToPath) error {
	pod := k8sutil.NewPeerPod(m, c.cluster.Metadata.Name, c.cluster.Spec, k2p, c.cluster.AsOwner())

	_, err := c.config.KubeCli.CoreV1().Pods(c.cluster.Metadata.Namespace).Create(pod)
	if err != nil {
		return err
	}
	return nil
}

//func (c *OrdererService) removePodAndService(name string) error {
//	ns := c.cluster.Metadata.Namespace
//	err := c.config.KubeCli.Core().Services(ns).Delete(name, nil)
//	if err != nil {
//		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
//			return err
//		}
//	}
//
//	opts := v1.NewDeleteOptions(podTerminationGracePeriod)
//	err = c.config.KubeCli.Core().Pods(ns).Delete(name, opts)
//	if err != nil {
//		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
//			return err
//		}
//	}
//	return nil
//}
//
//func (c *OrdererService) pollPods() (running, pending []*v1.Pod, err error) {
//	podList, err := c.config.KubeCli.Core().Pods(c.cluster.Metadata.Namespace).List(k8sutil.ClusterListOpt(c.cluster.Metadata.Name))
//	if err != nil {
//		return nil, nil, fmt.Errorf("failed to list running pods: %v", err)
//	}
//
//	for i := range podList.Items {
//		pod := &podList.Items[i]
//		if len(pod.OwnerReferences) < 1 {
//			c.logger.Warningf("pollPods: ignore pod %v: no owner", pod.Name)
//			continue
//		}
//		if pod.OwnerReferences[0].UID != c.cluster.Metadata.UID {
//			c.logger.Warningf("pollPods: ignore pod %v: owner (%v) is not %v",
//				pod.Name, pod.OwnerReferences[0].UID, c.cluster.Metadata.UID)
//			continue
//		}
//		switch pod.Status.Phase {
//		case v1.PodRunning:
//			running = append(running, pod)
//		case v1.PodPending:
//			pending = append(pending, pod)
//		}
//	}
//
//	return running, pending, nil
//}
//
//func (c *OrdererService) updateMemberStatus(pods []*v1.Pod) {
//	var ready, unready []*v1.Pod
//	for _, pod := range pods {
//		// TODO: Change to URL struct for TLS integration
//		url := fmt.Sprintf("http://%s:2379", pod.Status.PodIP)
//		healthy, err := etcdutil.CheckHealth(url)
//		if err != nil {
//			c.logger.Warningf("health check of etcd member (%s) failed: %v", url, err)
//		}
//		if healthy {
//			ready = append(ready, pod)
//		} else {
//			unready = append(unready, pod)
//		}
//	}
//	c.status.Members.Ready = k8sutil.GetPodNames(ready)
//	c.status.Members.Unready = k8sutil.GetPodNames(unready)
//}
//
func (c *PeerCluster) updateTPRStatus() error {
	if reflect.DeepEqual(c.cluster.Status, c.status) {
		return nil
	}

	newCluster := c.cluster
	newCluster.Status = c.status
	newClusterRes, err := k8sutil.UpdateClusterTPRObject(c.config.KubeCli.CoreV1().RESTClient(), c.cluster.Metadata.Namespace, c.cluster.Metadata.Name,
		c.cluster.Metadata.ResourceVersion, spec.TPRPeerClusterURI, newCluster, &spec.PeerCluster{})
	if err != nil {
		return err
	}

	c.cluster = newClusterRes.(*spec.PeerCluster)

	return nil
}

//func (c *OrdererService) updateLocalBackupStatus() error {
//	if c.bm == nil {
//		return nil
//	}
//
//	bs, err := c.bm.getStatus()
//	if err != nil {
//		return err
//	}
//	c.status.BackupServiceStatus = backupServiceStatusToTPRBackupServiceStatu(bs)
//
//	return nil
//}
//
func (c *PeerCluster) reportFailedStatus() {
	retryInterval := 5 * time.Second

	f := func() (bool, error) {
		c.status.SetPhase(spec.ClusterPhaseFailed)
		err := c.updateTPRStatus()
		if err == nil || k8sutil.IsKubernetesResourceNotFoundError(err) {
			return true, nil
		}

		if !apierrors.IsConflict(err) {
			c.logger.Warningf("retry report status in %v: fail to update: %v", retryInterval, err)
			return false, nil
		}

		cl, err := k8sutil.GetTPRObject(c.config.KubeCli.CoreV1().RESTClient(), c.cluster.Metadata.Namespace, c.cluster.Metadata.Name, spec.TPRPeerClusterURI, &spec.PeerCluster{})
		if err != nil {
			// Update (PUT) will return conflict even if object is deleted since we have UID set in object.
			// Because it will check UID first and return something like:
			// "Precondition failed: UID in precondition: 0xc42712c0f0, UID in object meta: ".
			if k8sutil.IsKubernetesResourceNotFoundError(err) {
				return true, nil
			}
			c.logger.Warningf("retry report status in %v: fail to get latest version: %v", retryInterval, err)
			return false, nil
		}
		c.cluster = cl.(*spec.PeerCluster)
		return false, nil

	}

	retryutil.Retry(retryInterval, math.MaxInt64, f)
}
