package v1alpha1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PeerCluster struct {
	v1.TypeMeta `json:",inline"`
	Metadata v1.ObjectMeta `json:"metadata,omitempty"`
	Spec     PeerClusterSpec   `json:"spec"`
	Status   PeerClusterStatus `json:"status"`
}

type PeerClusterSpec struct {

}

type PeerClusterStatus struct {

}

// A list of PeerClusters.
type PeerClusterList struct {
	v1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: http://releases.k8s.io/HEAD/docs/devel/api-conventions.md#metadata
	v1.ListMeta `json:"metadata,omitempty"`
	// List of PeerClusters
	Items []*PeerCluster `json:"items"`
}

type OrdererService struct {
	v1.TypeMeta `json:",inline"`
	Metadata v1.ObjectMeta `json:"metadata,omitempty"`
	Spec     PeerClusterSpec   `json:"spec"`
	Status   PeerClusterStatus `json:"status"`
}

type OrdererServiceSpec struct {

}

type OrdererServiceStatus struct {

}

// A list of OrdererServices.
type OrdererServiceList struct {
	v1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: http://releases.k8s.io/HEAD/docs/devel/api-conventions.md#metadata
	v1.ListMeta `json:"metadata,omitempty"`
	// List of OrdererServices
	Items []*OrdererService `json:"items"`
}