// Copyright 2016 The etcd-operator Authors
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

package k8sutil

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/coreos/etcd-operator/pkg/util/constants"
	"github.com/grapebaba/fabric-operator/spec"
	"github.com/grapebaba/fabric-operator/util/retryutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	apierrors "k8s.io/client-go/pkg/api/errors"
	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/api/meta/metatypes"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/labels"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/runtime/serializer"
	"k8s.io/client-go/pkg/util/intstr"
	"k8s.io/client-go/rest"
)

const (
	// TODO: This is constant for current purpose. We might make it configurable later.
	etcdDir                    = "/var/etcd"
	dataDir                    = etcdDir + "/data"
	backupFile                 = "/var/etcd/latest.backup"
	etcdVersionAnnotationKey   = "etcd.version"
	annotationPrometheusScrape = "prometheus.io/scrape"
	annotationPrometheusPort   = "prometheus.io/port"
)

func GetEtcdVersion(pod *v1.Pod) string {
	return pod.Annotations[etcdVersionAnnotationKey]
}

func SetEtcdVersion(pod *v1.Pod, version string) {
	pod.Annotations[etcdVersionAnnotationKey] = version
}

func GetPodNames(pods []*v1.Pod) []string {
	res := []string{}
	for _, p := range pods {
		res = append(res, p.Name)
	}
	return res
}


func EtcdImageName(version string) string {
	return fmt.Sprintf("quay.io/coreos/etcd:v%v", version)
}

func GetNodePortString(srv *v1.Service) string {
	return fmt.Sprint(srv.Spec.Ports[0].NodePort)
}

func PodWithNodeSelector(p *v1.Pod, ns map[string]string) *v1.Pod {
	p.Spec.NodeSelector = ns
	return p
}

func BackupServiceAddr(clusterName string) string {
	return fmt.Sprintf("%s:%d", BackupServiceName(clusterName), constants.DefaultBackupPodHTTPPort)
}

func BackupServiceName(clusterName string) string {
	return fmt.Sprintf("%s-backup-sidecar", clusterName)
}

func CreateMemberService(kubecli kubernetes.Interface, ns string, svc *v1.Service) (*v1.Service, error) {
	retSvc, err := kubecli.CoreV1().Services(ns).Create(svc)
	if err != nil {
		return nil, err
	}
	return retSvc, nil
}

func CreateEtcdService(kubecli kubernetes.Interface, clusterName, ns string, owner metatypes.OwnerReference) (*v1.Service, error) {
	svc := newEtcdServiceManifest(clusterName)
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	retSvc, err := kubecli.CoreV1().Services(ns).Create(svc)
	if err != nil {
		return nil, err
	}
	return retSvc, nil
}

// CreateAndWaitPod is a workaround for self hosted and util for testing.
// We should eventually get rid of this in critical code path and move it to test util.
func CreateAndWaitPod(kubecli kubernetes.Interface, ns string, pod *v1.Pod, timeout time.Duration) (*v1.Pod, error) {
	_, err := kubecli.CoreV1().Pods(ns).Create(pod)
	if err != nil {
		return nil, err
	}

	interval := 3 * time.Second
	var retPod *v1.Pod
	retryutil.Retry(interval, int(timeout/(interval)), func() (bool, error) {
		retPod, err = kubecli.CoreV1().Pods(ns).Get(pod.Name)
		if err != nil {
			return false, err
		}
		switch retPod.Status.Phase {
		case v1.PodRunning:
			return true, nil
		case v1.PodPending:
			return false, nil
		default:
			return false, fmt.Errorf("unexpected pod status.phase: %v", retPod.Status.Phase)
		}
	})

	return retPod, nil
}

func newEtcdServiceManifest(clusterName string) *v1.Service {
	labels := map[string]string{
		"app":          "etcd",
		"etcd_cluster": clusterName,
	}
	svc := &v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:   clusterName,
			Labels: labels,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "client",
					Port:       2379,
					TargetPort: intstr.FromInt(2379),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Selector: labels,
		},
	}
	return svc
}

// TODO: converge the port logic with member ClientAddr() and PeerAddr()
func NewMemberServiceManifest(etcdName, clusterName string, owner metatypes.OwnerReference) *v1.Service {
	svc := &v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name: etcdName,
			Labels: map[string]string{
				"etcd_cluster": clusterName,
			},
			Annotations: map[string]string{
				annotationPrometheusScrape: "true",
				annotationPrometheusPort:   "2379",
			},
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "server",
					Port:       2380,
					TargetPort: intstr.FromInt(2380),
					Protocol:   v1.ProtocolTCP,
				},
				{
					Name:       "client",
					Port:       2379,
					TargetPort: intstr.FromInt(2379),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app":          "etcd",
				"etcd_node":    etcdName,
				"etcd_cluster": clusterName,
			},
		},
	}
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	return svc
}


func addOwnerRefToObject(o meta.Object, r metatypes.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}

func MustNewKubeClient() kubernetes.Interface {
	cfg, err := InClusterConfig()
	if err != nil {
		panic(err)
	}
	return kubernetes.NewForConfigOrDie(cfg)
}

func InClusterConfig() (*rest.Config, error) {
	// Work around https://github.com/kubernetes/kubernetes/issues/40973
	// See https://github.com/coreos/etcd-operator/issues/731#issuecomment-283804819
	if len(os.Getenv("KUBERNETES_SERVICE_HOST")) == 0 {
		addrs, err := net.LookupHost("kubernetes.default.svc")
		if err != nil {
			panic(err)
		}
		os.Setenv("KUBERNETES_SERVICE_HOST", addrs[0])
	}
	if len(os.Getenv("KUBERNETES_SERVICE_PORT")) == 0 {
		os.Setenv("KUBERNETES_SERVICE_PORT", "443")
	}
	return rest.InClusterConfig()
}

func NewTPRClient() (*rest.RESTClient, error) {
	config, err := InClusterConfig()
	if err != nil {
		return nil, err
	}

	config.GroupVersion = &unversioned.GroupVersion{
		Group:   spec.TPRGroup,
		Version: spec.TPRVersion,
	}
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	restcli, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}
	return restcli, nil
}

func IsKubernetesResourceAlreadyExistError(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func IsKubernetesResourceNotFoundError(err error) bool {
	return apierrors.IsNotFound(err)
}

// We are using internal api types for cluster related.
func ClusterListOpt(clusterName string) v1.ListOptions {
	return v1.ListOptions{
		LabelSelector: labels.SelectorFromSet(newLablesForCluster(clusterName)).String(),
	}
}

func newLablesForCluster(clusterName string) map[string]string {
	return map[string]string{
		"etcd_cluster": clusterName,
		"app":          "etcd",
	}
}
