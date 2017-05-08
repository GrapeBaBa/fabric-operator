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
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/grapebaba/fabric-operator/pkg/spec"
	"github.com/grapebaba/fabric-operator/pkg/util/retryutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

// TODO: replace this package with Operator client
func WatchTPR(host, ns, tpr string, httpClient *http.Client, resourceVersion string) (*http.Response, error) {
	return httpClient.Get(fmt.Sprintf("%s/apis/%s/%s/namespaces/%s/%s?watch=true&resourceVersion=%s",
		host, spec.TPRGroup, spec.TPRVersion, ns, tpr, resourceVersion))
}

func GetTPRList(restcli rest.Interface, ns, tpr string, v interface{}) (interface{}, error) {
	b, err := restcli.Get().RequestURI(listTPRURI(ns, tpr)).DoRaw()
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(b, v); err != nil {
		return nil, err
	}
	return v, nil
}

func WaitTPRReady(restCli rest.Interface, interval, timeout time.Duration, ns, tpr string) error {
	return retryutil.Retry(interval, int(timeout/interval), func() (bool, error) {
		_, err := restCli.Get().RequestURI(listTPRURI(ns, tpr)).DoRaw()
		if err != nil {
			if apierrors.IsNotFound(err) { // not set up yet. wait more.
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}

func listTPRURI(ns, tpr string) string {
	return fmt.Sprintf("/apis/%s/%s/namespaces/%s/%s", spec.TPRGroup, spec.TPRVersion, ns, tpr)
}

func GetTPRObject(restcli rest.Interface, ns, name, tpr string, res interface{}) (interface{}, error) {
	uri := fmt.Sprintf("/apis/%s/%s/namespaces/%s/%s/%s", spec.TPRGroup, spec.TPRVersion, ns, tpr, name)
	b, err := restcli.Get().RequestURI(uri).DoRaw()
	if err != nil {
		return nil, err
	}
	return readOutCluster(b, res)
}

// UpdateClusterTPRObject updates the given TPR object.
// ResourceVersion of the object MUST be set or update will fail.
func UpdateClusterTPRObject(restcli rest.Interface, ns, name, version, tpr string, req, res interface{}) (interface{}, error) {
	if len(version) == 0 {
		return nil, errors.New("k8sutil: resource version is not provided")
	}
	return updateClusterTPRObject(restcli, ns, name, tpr, req, res)
}

// UpdateClusterTPRObjectUnconditionally updates the given TPR object.
// This should only be used in tests.
//func UpdateClusterTPRObjectUnconditionally(restcli rest.Interface, ns string, c *spec.PeerCluster) (*spec.PeerCluster, error) {
//	c.Metadata.ResourceVersion = ""
//	return updateClusterTPRObject(restcli, ns, c)
//}

func updateClusterTPRObject(restcli rest.Interface, ns, name, tpr string, req, res interface{}) (interface{}, error) {
	uri := fmt.Sprintf("/apis/%s/%s/namespaces/%s/%s/%s", spec.TPRGroup, spec.TPRVersion, ns, tpr, name)
	b, err := restcli.Put().RequestURI(uri).Body(req).DoRaw()
	if err != nil {
		return nil, err
	}
	return readOutCluster(b, res)
}

func readOutCluster(b []byte, v interface{}) (interface{}, error) {
	cluster := v
	if err := json.Unmarshal(b, cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}
