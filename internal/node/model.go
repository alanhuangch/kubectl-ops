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
	Resource      corev1.ResourceName
	Allocatable   resource.Quantity
	Requested     resource.Quantity
	Available     resource.Quantity
	RequestsKnown bool
	Ratio         float64
	RatioKnown    bool
}

type Consumer struct {
	Namespace string
	Pod       string
	UID       string
	Request   resource.Quantity
	Owner     string
	DaemonSet bool
}

type RequestsReport struct {
	CapturedAt   time.Time
	Node         string
	Completeness Completeness
	Resources    []ResourceUsage
	TopResource  corev1.ResourceName
	Consumers    []Consumer
	Warnings     []string
}
