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
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	TPROrdererServiceKind = "OrdererService"
	TPROrdererServiceName = "ordererservices"
)

type OrdererServicesGetter interface {
	OrdererServices(namespace string) *dynamic.ResourceClient
}

type OrdererServiceInterface interface {
	Create(*OrdererService) (*OrdererService, error)
	Get(name string) (*OrdererService, error)
	Update(*OrdererService) (*OrdererService, error)
	Delete(name string, options *v1.DeleteOptions) error
	List(opts v1.ListOptions) (runtime.Object, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
}

type ordererservices struct {
	restClient rest.Interface
	client     *dynamic.ResourceClient
	ns         string
}

func newOrdererServices(r rest.Interface, c *dynamic.Client, namespace string) *ordererservices {
	return &ordererservices{
		r,
		c.Resource(
			&v1.APIResource{
				Kind:       TPROrdererServiceKind,
				Name:       TPROrdererServiceName,
				Namespaced: true,
			},
			namespace,
		),
		namespace,
	}
}

func (s *ordererservices) Create(o *OrdererService) (*OrdererService, error) {
	us, err := UnstructuredFromOrdererService(o)
	if err != nil {
		return nil, err
	}

	us, err = s.client.Create(us)
	if err != nil {
		return nil, err
	}

	return OrdererServiceFromUnstructured(us)
}

func (s *ordererservices) Get(name string) (*OrdererService, error) {
	obj, err := s.client.Get(name)
	if err != nil {
		return nil, err
	}
	return OrdererServiceFromUnstructured(obj)
}

func (s *ordererservices) Update(o *OrdererService) (*OrdererService, error) {
	us, err := UnstructuredFromOrdererService(o)
	if err != nil {
		return nil, err
	}

	us, err = s.client.Update(us)
	if err != nil {
		return nil, err
	}

	return OrdererServiceFromUnstructured(us)
}

func (s *ordererservices) Delete(name string, options *v1.DeleteOptions) error {
	return s.client.Delete(name, options)
}

func (s *ordererservices) List(opts v1.ListOptions) (runtime.Object, error) {
	req := s.restClient.Get().
		Namespace(s.ns).
		Resource(TPRPeerClusterName).
	// VersionedParams(&options, v1.ParameterCodec)
		FieldsSelectorParam(nil)

	b, err := req.DoRaw()
	if err != nil {
		return nil, err
	}
	var sm PeerClusterList
	return sm, json.Unmarshal(b, &sm)
}

func (s *ordererservices) Watch(opts v1.ListOptions) (watch.Interface, error) {
	r, err := s.restClient.Get().
		Prefix("watch").
		Namespace(s.ns).
		Resource(TPRPeerClusterName).
	// VersionedParams(&options, v1.ParameterCodec).
		FieldsSelectorParam(nil).
		Stream()
	if err != nil {
		return nil, err
	}

	return watch.NewStreamWatcher(&ordererServiceDecoder{
		dec:   json.NewDecoder(r),
		close: r.Close,
	}), nil
}

// OrdererServiceFromUnstructured unmarshals a OrdererService object from dynamic client's unstructured
func OrdererServiceFromUnstructured(r *unstructured.Unstructured) (*OrdererService, error) {
	b, err := json.Marshal(r.Object)
	if err != nil {
		return nil, err
	}
	var s OrdererService
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	s.TypeMeta.Kind = TPROrdererServiceKind
	s.TypeMeta.APIVersion = TPRGroup + "/" + TPRVersion
	return &s, nil
}

// UnstructuredFromOrdererService marshals a OrdererService object into dynamic client's unstructured
func UnstructuredFromOrdererService(s *OrdererService) (*unstructured.Unstructured, error) {
	s.TypeMeta.Kind = TPROrdererServiceKind
	s.TypeMeta.APIVersion = TPRGroup + "/" + TPRVersion
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var r unstructured.Unstructured
	if err := json.Unmarshal(b, &r.Object); err != nil {
		return nil, err
	}
	return &r, nil
}

type ordererServiceDecoder struct {
	dec   *json.Decoder
	close func() error
}

func (d *ordererServiceDecoder) Close() {
	d.close()
}

func (d *ordererServiceDecoder) Decode() (action watch.EventType, Obj runtime.Object, err error) {
	var e struct {
		Type   watch.EventType
		Object OrdererService
	}
	if err := d.dec.Decode(&e); err != nil {
		return watch.Error, nil, err
	}
	return e.Type, &e.Object, nil
}
