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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TPROrdererServiceKind        = "OrdererService"
	TPROrdererServiceURI         = "ordererservices"
	TPROrdererServiceName        = "orderer-service." + TPRGroup
	TPROrdererServiceDescription = "Managed hyperledger fabric order service"
)

type OrdererService struct {
	metav1.TypeMeta `json:",inline"`
	Metadata metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec     OrdererServiceSpec   `json:"spec"`
	Status   ClusterStatus `json:"status"`
}

func (c *OrdererService) AsOwner() metav1.OwnerReference {
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

type GenesisMSPSpec struct {

}

type OrdererSpec struct {
	Identity *IdentitySpec `json:"identity"`

	TLS *OrdererTLSSpec `json:"tls,omitempty"`

	Profile string `json:"profile"`

	Configtx string `json:"configtx"`

	Config map[string]string `json:"config,omitempty"`

	GenesisMSPs map[string]*MSPSpec `json:"genesis_msps"`
}

type OrdererServiceSpec struct {
	ClusterSpec `json:",inline"`
	// Orderers defines the secure info and config for the orderer service
	Orderers []*OrdererSpec `json:"orderers"`
}

// Cleanup cleans up user passed spec, e.g. defaulting, transforming fields.
// TODO: move this to admission controller
func (c *OrdererServiceSpec) Cleanup() {
	if len(c.Version) == 0 {
		c.Version = defaultVersion
	}
	c.Version = strings.TrimLeft(c.Version, "v")
}

func (c *OrdererServiceSpec) Validate() error {
	return nil
}

type OrdererTLSSpec struct {
	OrdererCert []byte `json:"orderer_cert,omitempty"`

	OrdererKey []byte `json:"orderer_key,omitempty"`

	OrdererRootCert []byte `json:"orderer_root_cert,omitempty"`

}