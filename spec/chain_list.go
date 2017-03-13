package spec

import (
	"encoding/json"

	"k8s.io/client-go/pkg/api/unversioned"
)

// ChainList is a list of chains.
type ChainList struct {
	unversioned.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: http://releases.k8s.io/HEAD/docs/devel/api-conventions.md#metadata
	Metadata unversioned.ListMeta `json:"metadata,omitempty"`
	// Items is a list of third party objects
	Items []Chain `json:"items"`
}

// There is known issue with TPR in client-go:
//   https://github.com/kubernetes/client-go/issues/8
// Workarounds:
// - We include `Metadata` field in object explicitly.
// - we have the code below to work around a known problem with third-party resources and ugorji.

type ChainListCopy ChainList
type ChainCopy Chain

func (c *Chain) UnmarshalJSON(data []byte) error {
	tmp := ChainCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := Chain(tmp)
	*c = tmp2
	return nil
}

func (cl *ChainList) UnmarshalJSON(data []byte) error {
	tmp := ChainListCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := ChainList(tmp)
	*cl = tmp2
	return nil
}
