package spec

import (
	"encoding/json"

	"k8s.io/client-go/pkg/api/unversioned"
)

// PeerClusterList is a list of peer clusters.
type PeerClusterList struct {
	unversioned.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: http://releases.k8s.io/HEAD/docs/devel/api-conventions.md#metadata
	Metadata unversioned.ListMeta `json:"metadata,omitempty"`
	// Items is a list of third party objects
	Items []PeerCluster `json:"items"`
}

// There is known issue with TPR in client-go:
//   https://github.com/kubernetes/client-go/issues/8
// Workarounds:
// - We include `Metadata` field in object explicitly.
// - we have the code below to work around a known problem with third-party resources and ugorji.

type PeerClusterListCopy PeerClusterList
type PeerClusterCopy PeerCluster

func (c *PeerCluster) UnmarshalJSON(data []byte) error {
	tmp := PeerClusterCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := PeerCluster(tmp)
	*c = tmp2
	return nil
}

func (cl *PeerClusterList) UnmarshalJSON(data []byte) error {
	tmp := PeerClusterListCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := PeerClusterList(tmp)
	*cl = tmp2
	return nil
}
