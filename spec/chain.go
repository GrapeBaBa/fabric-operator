package spec

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
)

const (
	defaultVersion = "1.0.0"

	TPRKind        = "chain"
	TPRKindPlural  = "chains"
	TPRGroup       = "chain.grapebaba.me"
	TPRVersion     = "v1beta1"
	TPRDescription = "Managed hyperledger fabric"
)

func TPRName() string {
	return fmt.Sprintf("%s.%s", TPRKind, TPRGroup)
}

type Chain struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata v1.ObjectMeta `json:"metadata,omitempty"`
	Spec     ChainSpec   `json:"spec"`
	Status   ChainStatus `json:"status"`
}

type ChainSpec struct {
	// Version is the expected version of the fabric.
	// The fabric-operator will eventually make the fabric version
	// equal to the expected version.
	//
	// The version must follow the [semver]( http://semver.org) format, for example "3.1.2".
	// Only fabric released versions are supported
	//
	// If version is not set, default is "1.0.0".
	Version string `json:"version"`

	// Paused is to pause the control of the operator for the fabric.
	Paused bool `json:"paused,omitempty"`

	// Pod defines the policy to create pod for the container.
	// This will be overridden by sub spec (e.g OrganizationSpec, AuthoritySpec)
	Pod *PodPolicy `json:"pod,omitempty"`

	// OrganizationsSpec defines the spec to create fabric organizations.
	OrganizationsSpec OrganizationsSpec `json:"organizations_spec"`

	// AuthoritiesSpec defines the spec to create the fabric cas.
	AuthoritiesSpec AuthoritiesSpec `json:"authorities_spec"`
}

type ChainStatus struct {
	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion"`
	// TargetVersion is the version the cluster upgrading to.
	// If the cluster is not upgrading, TargetVersion is empty.
	TargetVersion string `json:"targetVersion"`

	OrgnazationsStatus OrgnazationStatus `json:"orgnazation_status"`

	AuthoritiesStatus AuthoritiesStatus `json:"authorities_status"`
}

type OrganizationsSpec struct {
	// OrganizationsSpec defines the spec to create fabric organizations.
	Organizations []OrganizationSpec `json:"organizations"`
}

type OrganizationSpec struct {
	// PeersSpec defines the spec to create fabric peers.
	PeersSpec *ClusterSpec `json:"peers_spec"`

	// OrderersSpec defines the spec to create fabric orderers.
	OrderersSpec *ClusterSpec `json:"orderers_spec"`
}

type AuthoritiesSpec struct {
	// AuthoritiesSpec defines the spec to create the fabric cas.
	Authorities []AuthoritySpec `json:"authorities"`
}

type AuthoritySpec struct {
	ClusterSpec
}

type ClusterSpec struct {
	// Size is the expected size of the fabric component cluster.
	// The fabric-operator will eventually make the size of the running
	// members equal to the expected size.
	Size int `json:"size"`

	// Pod defines the policy to create pod for the container.
	Pod *PodPolicy `json:"pod,omitempty"`
}

// PodPolicy defines the policy to create pod for the fabric container.
type PodPolicy struct {
	// NodeSelector specifies a map of key-value pairs. For the pod to be eligible
	// to run on a node, the node must have each of the indicated key-value pairs as
	// labels.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// AntiAffinity determines if the fabric-operator tries to avoid putting
	// the members in the same cluster onto the same node.
	AntiAffinity bool `json:"antiAffinity"`

	// Resources is the resource requirements for the container.
	// This field cannot be updated once the cluster is created.
	Resources v1.ResourceRequirements `json:"resources"`
}

type ClusterPhase string

const (
	ClusterPhaseNone     ClusterPhase = ""
	ClusterPhaseCreating              = "Creating"
	ClusterPhaseRunning               = "Running"
	ClusterPhaseFailed                = "Failed"
)

type ClusterCondition struct {
	Type ClusterConditionType `json:"type"`

	Reason string `json:"reason"`

	TransitionTime time.Time `json:"transitionTime"`
}

type ClusterConditionType string

const (
	ClusterConditionReady = "Ready"

	ClusterConditionRemovingDeadMember = "RemovingDeadMember"

	ClusterConditionRecovering = "Recovering"

	ClusterConditionScalingUp   = "ScalingUp"
	ClusterConditionScalingDown = "ScalingDown"

	ClusterConditionUpgrading = "Upgrading"
)

type OrgnazationsStatus struct {
	// All organizations status in the chain
	Orgnazations []OrgnazationStatus `json:"orgnazations"`
}

type AuthoritiesStatus struct {
	// All authorities status in the chain
	Authorities []AuthorityStatus `json:"orgnazations"`
}

type OrgnazationStatus struct {
	// Peer cluster status of this organization
	Peers ClusterStatus `json:"peers"`

	// Orderer cluster status of this organization
	Orderers ClusterStatus `json:"orderers"`
}

type AuthorityStatus struct {
	// CA cluster status of this authority
	CAs ClusterStatus `json:"peers"`
}

type ClusterStatus struct {
	// Phase is the cluster running phase
	Phase ClusterPhase `json:"phase"`

	// Reason for status
	Reason string       `json:"reason"`

	// ControlPuased indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused"`

	// Condition keeps ten most recent cluster conditions
	Conditions []ClusterCondition `json:"conditions"`

	// Size is the current size of the cluster
	Size int `json:"size"`

	// Members are the  members in the cluster
	Members MembersStatus `json:"members"`
}

type MembersStatus struct {
	// Ready are the peer/orderer members that are ready to serve requests
	// The member names are the same as the peer/orderer pod names
	Ready []string `json:"ready,omitempty"`

	// Unready are the peer/orderer members not ready to serve requests
	Unready []string `json:"unready,omitempty"`
}

// Cleanup cleans up user passed spec, e.g. defaulting, transforming fields.
// TODO: move this to admission controller
func (c *ChainSpec) Cleanup() {
	if len(c.Version) == 0 {
		c.Version = defaultVersion
	}
	c.Version = strings.TrimLeft(c.Version, "v")
}

func (cs *ChainStatus) IsFailed() bool {
	if cs == nil {
		return false
	}
	return true
}
