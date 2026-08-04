package node

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

type ResourceUsage struct {
	Resource       corev1.ResourceName
	Allocatable    resource.Quantity
	Requested      resource.Quantity
	Available      resource.Quantity
	RequestsKnown  bool
	AvailableKnown bool
	Ratio          float64
	RatioKnown     bool
}

type Consumer struct {
	Namespace   string
	Pod         string
	UID         string
	CreatedAt   time.Time
	ScheduledAt time.Time
	Request     resource.Quantity
	Owner       string
	DaemonSet   bool
}

type ExtendedResourceConsumer struct {
	Resource    corev1.ResourceName
	Namespace   string
	Pod         string
	UID         string
	CreatedAt   time.Time
	ScheduledAt time.Time
	Request     resource.Quantity
	Owner       string
	DaemonSet   bool
}

type PodResource struct {
	Resource     corev1.ResourceName
	Request      resource.Quantity
	Limit        resource.Quantity
	RequestSet   bool
	LimitSet     bool
	RequestRatio float64
	RatioKnown   bool
}

type PodResourceBreakdown struct {
	Namespace   string
	Pod         string
	UID         string
	CreatedAt   time.Time
	ScheduledAt time.Time
	Owner       string
	DaemonSet   bool
	Resources   []PodResource
}

type RequestsReport struct {
	CapturedAt        time.Time
	Node              string
	Namespace         string
	Completeness      Completeness
	Resources         []ResourceUsage
	TopResource       corev1.ResourceName
	Consumers         []Consumer
	ShowExtended      bool
	ExtendedKnown     bool
	ExtendedConsumers []ExtendedResourceConsumer
	ShowPods          bool
	PodResourcesKnown bool
	PodResources      []PodResourceBreakdown
	Warnings          []string
}
