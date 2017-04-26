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

package spec

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
)

const (
	PeersTPRKind              = "peerCluster"
	PeerClusterTPRDescription = "Managed hyperledger fabric peer cluster"
)

func PeersTPRName() string {
	return fmt.Sprintf("%s.%s", PeersTPRKind, TPRGroup)
}

type PeerCluster struct {
	metav1.TypeMeta `json:",inline"`
	Metadata v1.ObjectMeta `json:"metadata,omitempty"`
	Spec     PeerClusterSpec   `json:"spec"`
	Status   ClusterStatus `json:"status"`
}

func (c *PeerCluster) AsOwner() metav1.OwnerReference {
	trueVar := true
	// TODO: In 1.6 this is gonna be "k8s.io/kubernetes/pkg/apis/meta/v1"
	// Both api.OwnerReference and metatypes.OwnerReference are combined into that.
	return metav1.OwnerReference{
		APIVersion: c.APIVersion,
		Kind:       c.Kind,
		Name:       c.Metadata.Name,
		UID:        c.Metadata.UID,
		Controller: &trueVar,
	}
}

type MSPSpec struct {
	AdminCerts map[string][]byte `json:"admin_certs"`

	CACerts map[string][]byte `json:"ca_certs"`

	KeyStore map[string][]byte `json:"key_store"`

	SignCerts map[string][]byte `json:"sign_certs"`

	IntermediateCerts map[string][]byte `json:"intermediate_certs,omitempty"`
}

type IdentitySpec struct {
	OrgMSPId string `json:"org_msp_id"`

	MSP *MSPSpec `json:"msp"`
}

type TLSSpec struct {
	PeerCert []byte `json:"peer_cert,omitempty"`

	PeerKey []byte `json:"peer_key,omitempty"`

	PeerRootCert []byte `json:"peer_root_cert,omitempty"`

	VMCert []byte `json:"vm_cert,omitempty"`

	VMKey []byte `json:"vm_key,omitempty"`

	VMRootCert []byte `json:"vm_root_cert,omitempty"`
}

type PeerSpec struct {
	Identity *IdentitySpec `json:"identity"`

	TLS *TLSSpec `json:"tls,omitempty"`

	// Channels defines the channels of this peer own to.
	Channels []string `json:"channels,omitempty"`

	Chain string `json:"chain"`

	Config map[string]string `json:"config,omitempty"`
}

type PeerClusterSpec struct {
	ClusterSpec `json:",inline"`
	// Peers defines the secure info and config for the peer cluster
	Peers []*PeerSpec `json:"peers"`
}

// Cleanup cleans up user passed spec, e.g. defaulting, transforming fields.
// TODO: move this to admission controller
func (c *PeerClusterSpec) Cleanup() {
	if len(c.Version) == 0 {
		c.Version = defaultVersion
	}
	c.Version = strings.TrimLeft(c.Version, "v")
}

func (c *PeerClusterSpec) Validate() error {
	return nil
}
