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
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/grapebaba/fabric-operator/pkg/spec"
	"github.com/grapebaba/fabric-operator/pkg/util/fabricutil"
	"k8s.io/client-go/kubernetes"
)

var (
	osReconcileInterval         = 8 * time.Second
	osPodTerminationGracePeriod = int64(5)
)

type ordererServiceEventType string

const (
	eventDeleteOrdererService ordererServiceEventType = "Delete"
	eventModifyOrdererService ordererServiceEventType = "Modify"
)

type ordererServiceEvent struct {
	typ     ordererServiceEventType
	cluster *spec.OrdererService
}

type OrdererServiceConfig struct {
	ServiceAccount string

	KubeCli kubernetes.Interface
}

type OrdererService struct {
	logger *logrus.Entry

	config OrdererServiceConfig

	cluster *spec.OrdererService

	// in memory state of the cluster
	// status is the source of truth after OrdererService struct is materialized.
	status        spec.ClusterStatus
	memberCounter int

	eventCh chan *ordererServiceEvent
	stopCh  chan struct{}

	// members represents the members in the peer cluster.
	// the name of the member is the the name of the pod the member
	// process runs in.
	members fabricutil.MemberSet
}

func NewOrdererService(config OrdererServiceConfig, cl *spec.OrdererService, stopC <-chan struct{}, wg *sync.WaitGroup) *OrdererService {
	lg := logrus.WithField("pkg", "ordererservice").WithField("ordererservice-name", cl.Metadata.Name)
	c := &OrdererService{
		logger:  lg,
		config:  config,
		cluster: cl,
		eventCh: make(chan *ordererServiceEvent, 100),
		stopCh:  make(chan struct{}),
		status:  cl.Status.Copy(),
	}

	return c
}
