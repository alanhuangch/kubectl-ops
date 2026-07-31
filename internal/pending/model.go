package pending

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

type EvidenceSource string

const (
	SourceObserved     EvidenceSource = "Observed"
	SourceCurrentState EvidenceSource = "CurrentState"
)

type Completeness string

const (
	CompletenessComplete Completeness = "Complete"
	CompletenessPartial  Completeness = "Partial"
)

type Failure struct {
	Code     string
	Category string
	Source   EvidenceSource
	Message  string
	Details  map[string]string
}

type ObservedCondition struct {
	Status             corev1.ConditionStatus
	Reason             string
	Message            string
	LastTransitionTime time.Time
}

type ObservedEvent struct {
	Type       string
	Reason     string
	Message    string
	Count      int32
	ObservedAt time.Time
}

type Unsupported struct {
	Code    string
	Message string
}

type Consumer struct {
	Namespace string
	Pod       string
	Request   string
}

type NodeResult struct {
	Node                string
	Failures            []Failure
	HardFailureCount    int
	NonResourceFailures int
	ResourceGapScore    float64
	LimitingResource    string
	TopConsumers        []Consumer
}

type FailureSummary struct {
	Code     string
	Category string
	Count    int
	Nodes    []string
}

type Snapshot struct {
	CapturedAt  time.Time
	TargetPod   *corev1.Pod
	Nodes       []corev1.Node
	PodsByNode  map[string][]corev1.Pod
	Events      []corev1.Event
	NodesKnown  bool
	PodsKnown   bool
	EventsKnown bool
	Warnings    []string
}

type Options struct {
	Closest      int
	TopConsumers int
	AllNodes     bool
}

type Report struct {
	CapturedAt            time.Time
	Namespace             string
	Pod                   string
	UID                   string
	Phase                 corev1.PodPhase
	Scheduler             string
	Completeness          Completeness
	Condition             *ObservedCondition
	Events                []ObservedEvent
	CurrentStateAvailable bool
	Summary               []FailureSummary
	Nodes                 []NodeResult
	ClosestNodes          []NodeResult
	ShowAllNodes          bool
	Unsupported           []Unsupported
	Warnings              []string
}
