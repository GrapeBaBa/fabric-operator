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

	"github.com/grapebaba/fabric-operator/pkg/util/fabricutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
)

func peerContainer(commands, version string, m *fabricutil.Member) v1.Container {
	c := v1.Container{
		// TODO: fix "sleep 5".
		// Without waiting some time, there is highly probable flakes in network setup.
		Command: []string{"/bin/sh", "-c", fmt.Sprintf("sleep 5; %s", commands)},
		Name:    "fabric-peer",
		Image:   FabricPeerImageName(version),
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
			//{Name: "CORE_PEER_TLS_ENABLED", Value: "true"},
			//{Name: "CORE_PEER_TLS_KEY_FILE", Value: "/etc/hyperledger/fabric/secret/tls/peerkey.pem"},
			//{Name: "CORE_PEER_TLS_CERT_FILE", Value: "/etc/hyperledger/fabric/secret/tls/peercert.pem"},
			//{Name: "CORE_PEER_TLS_ROOTCERT_FILE", Value: "/etc/hyperledger/fabric/secret/tls/peerrootcert.pem"},
		},
		ImagePullPolicy: v1.PullIfNotPresent,
	}

	return c
}

func ordererContainer(commands, version string, m *fabricutil.Member, config *v1.ConfigMap) v1.Container {
	c := v1.Container{
		// TODO: fix "sleep 5".
		// Without waiting some time, there is highly probable flakes in network setup.
		Command: []string{"/bin/sh", "-c", fmt.Sprintf("sleep 5; env; %s", commands)},
		Name:    "fabric-orderer",
		Image:   FabricOrdererImageName(version),
		Ports: []v1.ContainerPort{
			{
				Name:          "orderer",
				ContainerPort: int32(7050),
				Protocol:      v1.ProtocolTCP,
			},

		},
		VolumeMounts: []v1.VolumeMount{
			{Name: "secret", MountPath: "/etc/hyperledger/fabric/secret"},
			{Name: "config", MountPath: "/etc/hyperledger/fabric/configtx.yaml", SubPath: "configtx.yaml"},
			{Name: "data", MountPath: "/var/hyperledger/production"},
		},
		Env: []v1.EnvVar{
			{Name: "ORDERER_GENERAL_LOGLEVEL", Value: "DEBUG"},
			{Name: "ORDERER_GENERAL_LISTENADDRESS", Value: "0.0.0.0"},
			{Name: "ORDERER_GENERAL_LOCALMSPID", Value: m.OrgMSPId},
			{Name: "ORDERER_GENERAL_LOCALMSPDIR", Value: "/etc/hyperledger/fabric/secret/msp"},
			//{Name: "ORDERER_GENERAL_GENESISPROFILE", Value: "TwoOrgs"},
			{Name: "ORDERER_GENERAL_GENESISPROFILE", ValueFrom: &v1.EnvVarSource{
				ConfigMapKeyRef: &v1.ConfigMapKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: config.Name,
					},
					Key: "ORDERER_GENERAL_GENESISPROFILE",
				},
			}},
			//{Name: "ORDERER_GENERAL_TLS_ENABLED", Value: "true"},
			//{Name: "ORDERER_GENERAL_TLS_PRIVATEKEY", Value: "/etc/hyperledger/fabric/secret/tls/ordererkey.pem"},
			//{Name: "ORDERER_GENERAL_TLS_CERTIFICATE", Value: "/etc/hyperledger/fabric/secret/tls/orderercert.pem"},
			//{Name: "ORDERER_GENERAL_TLS_ROOTCAS", Value: "[/etc/hyperledger/fabric/secret/tls/ordererrootcert.pem]"},
		},

		EnvFrom: []v1.EnvFromSource{
			{
				ConfigMapRef: &v1.ConfigMapEnvSource{
					LocalObjectReference: v1.LocalObjectReference{Name: config.Name},
				},
			},
		},
		ImagePullPolicy: v1.PullIfNotPresent,
	}

	return c
}

func containerWithRequirements(c v1.Container, r v1.ResourceRequirements) v1.Container {
	c.Resources = r
	return c
}

func PodWithAntiAffinity(pod *v1.Pod, labels map[string]string) *v1.Pod {
	// set pod anti-affinity with the pods that belongs to the same peer cluster
	affinity := v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: labels,
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
