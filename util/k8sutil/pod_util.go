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
	"encoding/json"
	"fmt"

	"github.com/grapebaba/fabric-operator/util/fabricutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
)

func peerContainer(commands, version string, m *fabricutil.Member) v1.Container {
	c := v1.Container{
		// TODO: fix "sleep 5".
		// Without waiting some time, there is highly probable flakes in network setup.
		Command:    []string{"/bin/sh", "-c", fmt.Sprintf("sleep 5; %s", commands)},
		Name:       "fabric-peer",
		Image:      FabricPeerImageName(version),
		WorkingDir: "/opt/gopath/src/github.com/hyperledger/fabric",
		Ports: []v1.ContainerPort{
			{
				Name:          "peer",
				ContainerPort: int32(7051),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "event",
				ContainerPort: int32(7053),
				Protocol:      v1.ProtocolTCP,
			},
		},
		VolumeMounts: []v1.VolumeMount{
			{Name: "docker", MountPath: "/host/var/run"},
			{Name: "secret", MountPath: "/etc/hyperledger/fabric/secret"},
			{Name: "data", MountPath: "/var/hyperledger/production"},
		},
		Env: []v1.EnvVar{
			{Name: "CORE_LOGGING_LEVEL", Value: "DEBUG"},
			{Name: "CORE_VM_ENDPOINT", Value: "unix:///host/var/run/docker.sock"},
			{Name: "CORE_PEER_ID", Value: m.Name},
			{Name: "CORE_PEER_ENDORSER_ENABLED", Value: "true"},
			{Name: "CORE_PEER_LOCALMSPID", Value: m.OrgMSPId},
			{Name: "CORE_PEER_MSPCONFIGPATH", Value: "/etc/hyperledger/fabric/secret/msp"},
			{Name: "CORE_PEER_GOSSIP_USELEADERELECTION", Value: "true"},
			{Name: "CORE_PEER_GOSSIP_ORGLEADER", Value: "false"},
			{Name: "CORE_PEER_ADDRESS", Value: m.PeerAddr()},
			{Name: "CORE_PEER_GOSSIP_EXTERNALENDPOINT", Value: m.PeerAddr()},
			{Name: "CORE_PEER_GOSSIP_SKIPHANDSHAKE", Value: "true"},
			{Name: "CORE_VM_DOCKER_HOSTCONFIG_NETWORKMODE", Value: "test"},
			{Name: "CORE_PEER_TLS_ENABLED", Value: "true"},
			{Name: "CORE_PEER_TLS_KEY_FILE", Value: "/etc/hyperledger/fabric/secret/tls/peerkey.pem"},
			{Name: "CORE_PEER_TLS_CERT_FILE", Value: "/etc/hyperledger/fabric/secret/tls/peercert.pem"},
			{Name: "CORE_PEER_TLS_ROOTCERT_FILE", Value: "/etc/hyperledger/fabric/secret/tls/peerrootcert.pem"},
		},
	}

	return c
}

func containerWithRequirements(c v1.Container, r v1.ResourceRequirements) v1.Container {
	c.Resources = r
	return c
}

func PodWithAntiAffinity(pod *v1.Pod, clusterName string) *v1.Pod {
	// set pod anti-affinity with the pods that belongs to the same peer cluster
	affinity := v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &unversioned.LabelSelector{
						MatchLabels: map[string]string{
							"peer_cluster": clusterName,
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}

	affinityb, err := json.Marshal(affinity)
	if err != nil {
		panic("failed to marshal affinty struct")
	}

	pod.Annotations[api.AffinityAnnotationKey] = string(affinityb)
	return pod
}
