package workload

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type Completeness string

const (
	CompletenessComplete Completeness = "Complete"
	CompletenessPartial  Completeness = "Partial"
)

type Kind string

const (
	KindDeployment  Kind = "Deployment"
	KindStatefulSet Kind = "StatefulSet"
	KindDaemonSet   Kind = "DaemonSet"
	KindJob         Kind = "Job"
	KindCronJob     Kind = "CronJob"
	KindReplicaSet  Kind = "ReplicaSet"
	KindPod         Kind = "Pod"
)

type ResourceClass string

const (
	ResourceClassAll    ResourceClass = "all"
	ResourceClassCPU    ResourceClass = "cpu"
	ResourceClassMemory ResourceClass = "memory"
	ResourceClassGPU    ResourceClass = "gpu"
)

type Snapshot struct {
	CapturedAt        time.Time
	Namespace         string
	Workloads         []Workload
	OwnerLinks        []OwnerLink
	Pods              []corev1.Pod
	PodsKnown         bool
	InventoryComplete bool
	Warnings          []string
}

type Workload struct {
	Namespace   string
	Kind        Kind
	Name        string
	UID         string
	PlannedPods int32
	Template    corev1.PodTemplateSpec
	ActualKnown bool
}

// OwnerLink represents an intermediate controller in an ownership chain, such
// as ReplicaSet -> Deployment or Job -> CronJob.
type OwnerLink struct {
	UID      string
	OwnerUID string
}

type Options struct {
	ResourceClass ResourceClass
}

type Report struct {
	CapturedAt    time.Time
	Namespace     string
	ResourceClass ResourceClass
	Completeness  Completeness
	Items         []ResourceUsage
	Warnings      []string
}

type ResourceUsage struct {
	Namespace string
	Kind      Kind
	Workload  string
	UID       string
	Pods      PodCounts
	CPU       ResourcePair
	Memory    ResourcePair
	GPUs      []GPUResourcePair
	Placement NodePlacement
}

type PodCounts struct {
	Planned     int32
	Actual      int
	ActualKnown bool
}

type ResourcePair struct {
	Planned     resource.Quantity
	Actual      resource.Quantity
	ActualKnown bool
}

type GPUResourcePair struct {
	Resource    corev1.ResourceName
	Planned     resource.Quantity
	Actual      resource.Quantity
	ActualKnown bool
}

type NodePlacement struct {
	NodeSelector []KeyValue
	Required     []NodeSelectorTerm
	Preferred    []PreferredNodeSelectorTerm
}

type KeyValue struct {
	Key   string
	Value string
}

type NodeSelectorTerm struct {
	MatchExpressions []NodeSelectorRequirement
	MatchFields      []NodeSelectorRequirement
}

type PreferredNodeSelectorTerm struct {
	Weight     int32
	Preference NodeSelectorTerm
}

type NodeSelectorRequirement struct {
	Key      string
	Operator corev1.NodeSelectorOperator
	Values   []string
}
