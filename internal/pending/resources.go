package pending

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	resourcehelper "k8s.io/component-helpers/resource"
)

func analyzeResources(
	target *corev1.Pod,
	node *corev1.Node,
	assigned []corev1.Pod,
	top int,
) ([]Failure, float64, string, []Consumer) {
	targetRequests := resourcehelper.PodRequests(target, resourcehelper.PodResourcesOptions{})
	currentRequests := corev1.ResourceList{}
	activePods := int64(0)
	for i := range assigned {
		item := &assigned[i]
		if item.UID == target.UID || isTerminal(item) {
			continue
		}
		activePods++
		addResources(currentRequests, resourcehelper.PodRequests(item, resourcehelper.PodResourcesOptions{}))
	}
	currentRequests[corev1.ResourcePods] = *resource.NewQuantity(activePods, resource.DecimalSI)
	targetRequests[corev1.ResourcePods] = resource.MustParse("1")

	resourceNames := make([]corev1.ResourceName, 0, len(targetRequests))
	for name, quantity := range targetRequests {
		if !quantity.IsZero() {
			resourceNames = append(resourceNames, name)
		}
	}
	sort.Slice(resourceNames, func(i, j int) bool { return resourceNames[i] < resourceNames[j] })

	var failures []Failure
	totalGap := 0.0
	limitingResource := ""
	limitingGap := 0.0
	for _, name := range resourceNames {
		requested := targetRequests[name].DeepCopy()
		allocatable := node.Status.Allocatable[name].DeepCopy()
		used := currentRequests[name].DeepCopy()
		available := allocatable.DeepCopy()
		available.Sub(used)
		if requested.Cmp(available) <= 0 {
			continue
		}
		shortfall := requested.DeepCopy()
		shortfall.Sub(available)
		gap := normalizedGap(shortfall, requested)
		totalGap += gap
		if gap > limitingGap || (gap == limitingGap && string(name) < limitingResource) {
			limitingGap = gap
			limitingResource = string(name)
		}
		failures = append(failures, Failure{
			Code:     insufficientResourceCode(name),
			Category: "Resources",
			Source:   SourceCurrentState,
			Message:  fmt.Sprintf("insufficient %s", name),
			Details: map[string]string{
				"resource":    string(name),
				"requested":   requested.String(),
				"allocatable": allocatable.String(),
				"used":        used.String(),
				"available":   available.String(),
				"shortfall":   shortfall.String(),
			},
		})
	}
	return failures, totalGap, limitingResource, topResourceConsumers(assigned, target.UID, corev1.ResourceName(limitingResource), top)
}

func addResources(target, addition corev1.ResourceList) {
	for name, quantity := range addition {
		current := target[name]
		current.Add(quantity)
		target[name] = current
	}
}

func normalizedGap(shortfall, requested resource.Quantity) float64 {
	denominator := requested.AsApproximateFloat64()
	if denominator <= 0 {
		return 0
	}
	return shortfall.AsApproximateFloat64() / denominator
}

func insufficientResourceCode(name corev1.ResourceName) string {
	switch name {
	case corev1.ResourceCPU:
		return "InsufficientCPU"
	case corev1.ResourceMemory:
		return "InsufficientMemory"
	case corev1.ResourceEphemeralStorage:
		return "InsufficientEphemeralStorage"
	case corev1.ResourcePods:
		return "TooManyPods"
	default:
		return "InsufficientExtendedResource"
	}
}

func topResourceConsumers(pods []corev1.Pod, targetUID types.UID, name corev1.ResourceName, limit int) []Consumer {
	if name == "" || limit <= 0 {
		return nil
	}
	type item struct {
		consumer Consumer
		quantity resource.Quantity
		uid      string
	}
	items := make([]item, 0, len(pods))
	for i := range pods {
		if pods[i].UID == targetUID || isTerminal(&pods[i]) {
			continue
		}
		quantity := resourcehelper.PodRequests(&pods[i], resourcehelper.PodResourcesOptions{})[name]
		if name == corev1.ResourcePods {
			quantity = resource.MustParse("1")
		}
		if quantity.IsZero() {
			continue
		}
		items = append(items, item{
			consumer: Consumer{Namespace: pods[i].Namespace, Pod: pods[i].Name, Request: quantity.String()},
			quantity: quantity,
			uid:      string(pods[i].UID),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if comparison := items[i].quantity.Cmp(items[j].quantity); comparison != 0 {
			return comparison > 0
		}
		if items[i].consumer.Namespace != items[j].consumer.Namespace {
			return items[i].consumer.Namespace < items[j].consumer.Namespace
		}
		if items[i].consumer.Pod != items[j].consumer.Pod {
			return items[i].consumer.Pod < items[j].consumer.Pod
		}
		return items[i].uid < items[j].uid
	})
	if len(items) > limit {
		items = items[:limit]
	}
	result := make([]Consumer, 0, len(items))
	for _, item := range items {
		result = append(result, item.consumer)
	}
	return result
}

func isTerminal(item *corev1.Pod) bool {
	return item.Status.Phase == corev1.PodSucceeded || item.Status.Phase == corev1.PodFailed
}
