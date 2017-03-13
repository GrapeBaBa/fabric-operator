package spec

import (
	"fmt"
	"time"

	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
	"strings"
)

const (
	defaultVersion = "1.0.0"

	TPRKind        = "chain"
	TPRKindPlural  = "chains"
	TPRGroup       = "chain.grapebaba.me"
	TPRVersion     = "v1beta1"
	TPRDescription = "Managed hyperledger fabric blockchain"
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

type ClusterSpec struct {
	// Size is the expected size of the fabric component cluster.
	// The fabric-operator will eventually make the size of the running
	// orderers equal to the expected size.
	Size int `json:"size"`

	// Pod defines the policy to create pod for the container.
	Pod *PodPolicy `json:"pod,omitempty"`
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
	Pod *PodPolicy `json:"pod,omitempty"`

	// PeerSpec defines the spec to create the fabric peers.
	PeerSpec *ClusterSpec `json:"peer_spec"`

	// OrdererSpec defines the spec to create the fabric orderers.
	OrdererSpec *ClusterSpec `json:"orderer_spec"`

	// CASpec defines the spec to create the fabric ca.
	CASpec *ClusterSpec `json:"ca_spec"`
	// Backup defines the policy to backup data of etcd cluster if not nil.
	// If backup policy is set but restore policy not, and if a previous backup exists,
	// this cluster would face conflict and fail to start.
	//Backup *BackupPolicy `json:"backup,omitempty"`

	// Restore defines the policy to restore cluster form existing backup if not nil.
	// It's not allowed if restore policy is set and backup policy not.
	//Restore *RestorePolicy `json:"restore,omitempty"`

	// SelfHosted determines if the etcd cluster is used for a self-hosted
	// Kubernetes cluster.
	//SelfHosted *SelfHostedPolicy `json:"selfHosted,omitempty"`
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

type ChainStatus struct {
	PeerStatus *ClusterStatus `json:"peer_status"`

	OrdererStatus *ClusterStatus `json:"orderer_status"`

	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion"`
	// TargetVersion is the version the cluster upgrading to.
	// If the cluster is not upgrading, TargetVersion is empty.
	TargetVersion string `json:"targetVersion"`
}

type ClusterStatus struct {
	// Phase is the cluster running phase
	Phase  ClusterPhase `json:"phase"`
	Reason string       `json:"reason"`

	// ControlPuased indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused"`

	// Condition keeps ten most recent cluster conditions
	Conditions []ClusterCondition `json:"conditions"`

	// Size is the current size of the cluster
	Size int `json:"size"`
	// ReadyPods are the etcd pods that are ready to serve requests
	ReadyPods []string `json:"readyPods"`
	// UnreadyPods are the etcd pods not ready to serve requests
	UnreadyPods []string `json:"unreadyPods"`

	// BackupServiceStatus is the status of the backup service.
	// BackupServiceStatus only exists when backup is enabled in the
	// cluster spec.
	//BackupServiceStatus *BackupServiceStatus `json:"backupServiceStatus,omitempty"`
}

func (cs *ChainStatus) IsFailed() bool {
	if cs == nil {
		return false
	}

	if cs.PeerStatus.Phase == ClusterPhaseFailed {
		return true
	}

	if cs.OrdererStatus.Phase == ClusterPhaseFailed {
		return true
	}

	return false
}

// Cleanup cleans up user passed spec, e.g. defaulting, transforming fields.
// TODO: move this to admission controller
func (c *ChainSpec) Cleanup() {
	if len(c.Version) == 0 {
		c.Version = defaultVersion
	}
	c.Version = strings.TrimLeft(c.Version, "v")
}