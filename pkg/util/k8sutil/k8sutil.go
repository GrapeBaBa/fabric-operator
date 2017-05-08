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
	"github.com/grapebaba/fabric-operator/pkg/spec"
	"github.com/grapebaba/fabric-operator/pkg/util/fabricutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
)

const (
	// TODO: This is constant for current purpose. We might make it configurable later.
	peerVersionAnnotationKey = "peer.version"
)

func GetPeerVersion(pod *v1.Pod) string {
	return pod.Annotations[peerVersionAnnotationKey]
}

func SetPeerVersion(pod *v1.Pod, version string) {
	pod.Annotations[peerVersionAnnotationKey] = version
}

func GetPodNames(pods []*v1.Pod) []string {
	res := []string{}
	for _, p := range pods {
		res = append(res, p.Name)
	}
	return res
}

func FabricPeerImageName(version string) string {
	return fmt.Sprintf("hyperledger/fabric-peer:x86_64-%v", version)
}

func GetNodePortString(srv *v1.Service) string {
	return fmt.Sprint(srv.Spec.Ports[0].NodePort)
}

func PodWithNodeSelector(p *v1.Pod, ns map[string]string) *v1.Pod {
	p.Spec.NodeSelector = ns
	return p
}

func CreateMemberSecret(kubecli kubernetes.Interface, ns string, secret *v1.Secret) (*v1.Secret, error) {
	retSecret, err := kubecli.CoreV1().Secrets(ns).Create(secret)
	if err != nil {
		return nil, err
	}
	return retSecret, nil
}

func NewPeerPod(m *fabricutil.Member, clusterName string, cs spec.PeerClusterSpec, k2p []v1.KeyToPath, owner metav1.OwnerReference) *v1.Pod {
	commands := "peer node start --peer-defaultchain=false"

	container := peerContainer(commands, cs.Version, m)
	if cs.Pod != nil {
		container = containerWithRequirements(container, cs.Pod.Resources)
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: m.Name,
			Labels: map[string]string{
				"app":          "peer",
				"peer_node":    m.Name,
				"peer_cluster": clusterName,
			},
			Annotations: map[string]string{},
		},
		Spec: v1.PodSpec{
			Containers:    []v1.Container{container},
			RestartPolicy: v1.RestartPolicyNever,
			Volumes: []v1.Volume{
				{Name: "data", VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}}},
				{Name: "docker", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/var/run"}}},
				{Name: "secret", VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{
					SecretName: m.SecretName,
					Items:      k2p}}},
			},
			// DNS A record: [m.Name].[clusterName].Namespace.svc.cluster.local.
			// For example, etcd-0000 in default namesapce will have DNS name
			// `etcd-0000.etcd.default.svc.cluster.local`.
			Hostname:  m.Name,
			Subdomain: clusterName,
		},
	}

	SetPeerVersion(pod, cs.Version)

	if cs.Pod != nil {
		if cs.Pod.AntiAffinity {
			pod = PodWithAntiAffinity(pod, clusterName)
		}

		if len(cs.Pod.NodeSelector) != 0 {
			pod = PodWithNodeSelector(pod, cs.Pod.NodeSelector)
		}
	}
	addOwnerRefToObject(pod.GetObjectMeta(), owner)
	return pod
}

func CreatePeerService(kubecli kubernetes.Interface, clusterName, ns string, owner metav1.OwnerReference) error {
	return createService(kubecli, clusterName, clusterName, ns, v1.ClusterIPNone, owner)
}

func createService(kubecli kubernetes.Interface, svcName, clusterName, ns, clusterIP string, owner metav1.OwnerReference) error {
	svc := newPeerClusterServiceManifest(svcName, clusterName, clusterIP)
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	_, err := kubecli.CoreV1().Services(ns).Create(svc)
	return err
}

func newPeerClusterServiceManifest(svcName, clusterName string, clusterIP string) *v1.Service {
	labels := map[string]string{
		"app":          "peer",
		"peer_cluster": clusterName,
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   svcName,
			Labels: labels,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "peer",
					Port:       7051,
					TargetPort: intstr.FromInt(int(7051)),
					Protocol:   v1.ProtocolTCP,
				},
				{
					Name:       "event",
					Port:       7053,
					TargetPort: intstr.FromInt(int(7053)),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Selector:  labels,
			ClusterIP: clusterIP,
		},
	}
	return svc
}

func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
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

	config.GroupVersion = &schema.GroupVersion{
		Group:   spec.TPRGroup,
		Version: spec.TPRVersion,
	}
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	restCli, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}
	return restCli, nil
}

func IsKubernetesResourceAlreadyExistError(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func IsKubernetesResourceNotFoundError(err error) bool {
	return apierrors.IsNotFound(err)
}

// We are using internal api types for cluster related.
func ClusterListOpt(clusterName string) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(newLablesForCluster(clusterName)).String(),
	}
}

func newLablesForCluster(clusterName string) map[string]string {
	return map[string]string{
		"etcd_cluster": clusterName,
		"app":          "etcd",
	}
}
