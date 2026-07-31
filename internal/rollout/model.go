package rollout

import (
	"time"

	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

type Completeness string

const (
	CompletenessComplete Completeness = "Complete"
	CompletenessPartial  Completeness = "Partial"
)

type Status string

const (
	StatusHealthy     Status = "Healthy"
	StatusProgressing Status = "Progressing"
	StatusDegraded    Status = "Degraded"
	StatusUnknown     Status = "Unknown"
)

type Severity string

const (
	SeverityError   Severity = "Error"
	SeverityWarning Severity = "Warning"
)

type Snapshot struct {
	CapturedAt       time.Time
	Deployment       *appsv1.Deployment
	ReplicaSets      []appsv1.ReplicaSet
	Pods             []corev1.Pod
	Events           []timeline.TimelineEvent
	ReplicaSetsKnown bool
	PodsKnown        bool
	EventsKnown      bool
	Warnings         []string
}

type Options struct {
	EventLimit int
}

type DeploymentInfo struct {
	Namespace          string
	Name               string
	UID                string
	Generation         int64
	ObservedGeneration int64
	Desired            int32
	Current            int32
	Updated            int32
	Ready              int32
	Available          int32
	Unavailable        int32
}

type Condition struct {
	Type               string
	Status             string
	Reason             string
	Message            string
	LastTransitionTime time.Time
}

type ReplicaSetInfo struct {
	Namespace string
	Name      string
	UID       string
	Revision  int64
	Current   bool
	Desired   int32
	Replicas  int32
	Ready     int32
	Available int32
	CreatedAt time.Time
}

type PodInfo struct {
	Namespace  string
	Name       string
	UID        string
	ReplicaSet string
	Node       string
	Phase      string
	Ready      bool
	Restarts   int32
	Reasons    []string
	CreatedAt  time.Time
}

type EventInfo struct {
	Type                string
	Reason              string
	RegardingKind       string
	RegardingName       string
	Note                string
	Count               int32
	LastObservedAt      time.Time
	ReportingController string
}

type Finding struct {
	Severity Severity
	Code     string
	Object   string
	Message  string
}

type Report struct {
	CapturedAt   time.Time
	Status       Status
	Completeness Completeness
	Deployment   DeploymentInfo
	Conditions   []Condition
	ReplicaSets  []ReplicaSetInfo
	Pods         []PodInfo
	Events       []EventInfo
	Findings     []Finding
	Warnings     []string
}
