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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/rest"
)

const (
	TPRGroup   = "fabric.hyperledger.org"
	TPRVersion = "v1alpha1"
)

type FabricV1alpha1Interface interface {
	RESTClient() rest.Interface
	PeerClustersGetter
	OrdererServicesGetter
}

type FabricV1alpha1Client struct {
	restClient    rest.Interface
	dynamicClient *dynamic.Client
}

func (c *FabricV1alpha1Client) PeerCluster(namespace string) PeerClusterInterface {
	return newPeerClusters(c.restClient, c.dynamicClient, namespace)
}

func (c *FabricV1alpha1Client) OrdererServices(namespace string) OrdererServiceInterface {
	return newOrdererServices(c.restClient, c.dynamicClient, namespace)
}

func (c *FabricV1alpha1Client) RESTClient() rest.Interface {
	return c.restClient
}

func NewForConfig(c *rest.Config) (*FabricV1alpha1Client, error) {
	config := *c
	setConfigDefaults(&config)
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewClient(&config)
	if err != nil {
		return nil, err
	}

	return &FabricV1alpha1Client{client, dynamicClient}, nil
}

func setConfigDefaults(config *rest.Config) {
	config.GroupVersion = &schema.GroupVersion{
		Group:   TPRGroup,
		Version: TPRVersion,
	}
	config.APIPath = "/apis"
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}
	return
}
