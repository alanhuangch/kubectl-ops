package node

import (
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	resourcehelper "k8s.io/component-helpers/resource"
)

type RequestsOptions struct {
	Now            time.Time
	Namespace      string
	Top            int
	TopResource    corev1.ResourceName
	PodsKnown      bool
	ShowExtended   bool
	ShowPods       bool
	ResourceFilter corev1.ResourceName
}

func AnalyzeRequests(item *corev1.Node, pods []corev1.Pod, options RequestsOptions) RequestsReport {
	report := RequestsReport{
		CapturedAt:        options.Now,
		Node:              item.Name,
		Namespace:         options.Namespace,
		Completeness:      CompletenessComplete,
		TopResource:       options.TopResource,
		ShowExtended:      options.ShowExtended,
		ExtendedKnown:     options.ShowExtended && options.PodsKnown,
		ShowPods:          options.ShowPods,
		PodResourcesKnown: options.ShowPods && options.PodsKnown,
	}
	if !options.PodsKnown {
		report.Completeness = CompletenessPartial
		report.Warnings = append(report.Warnings, "Pod requests and consumers are unavailable because Pods could not be listed.")
	}

	requested := corev1.ResourceList{}
	type podRequest struct {
		pod      *corev1.Pod
		requests corev1.ResourceList
		limits   corev1.ResourceList
	}
	activeRequests := make([]podRequest, 0, len(pods))
	if options.PodsKnown {
		activeCount := int64(0)
		for i := range pods {
			pod := &pods[i]
			if pod.Spec.NodeName != item.Name ||
				(options.Namespace != "" && pod.Namespace != options.Namespace) ||
				isTerminalPod(pod) {
				continue
			}
			activeCount++
			podRequests := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
			var podLimits corev1.ResourceList
			if options.ShowPods {
				podLimits = resourcehelper.PodLimits(pod, resourcehelper.PodResourcesOptions{})
			}
			activeRequests = append(activeRequests, podRequest{pod: pod, requests: podRequests, limits: podLimits})
			addResourceList(requested, podRequests)
		}
		requested[corev1.ResourcePods] = *resource.NewQuantity(activeCount, resource.DecimalSI)
	}

	var resourceNames []corev1.ResourceName
	if options.ResourceFilter != "" {
		resourceNames = []corev1.ResourceName{options.ResourceFilter}
	} else {
		resourceNameSet := make(map[corev1.ResourceName]struct{}, len(item.Status.Allocatable)+len(requested))
		for name := range item.Status.Allocatable {
			resourceNameSet[name] = struct{}{}
		}
		for name := range requested {
			resourceNameSet[name] = struct{}{}
		}
		resourceNames = make([]corev1.ResourceName, 0, len(resourceNameSet))
		for name := range resourceNameSet {
			resourceNames = append(resourceNames, name)
		}
		sort.Slice(resourceNames, func(i, j int) bool {
			leftRank, rightRank := resourceRank(resourceNames[i]), resourceRank(resourceNames[j])
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			return resourceNames[i] < resourceNames[j]
		})
	}

	for _, name := range resourceNames {
		allocatable := item.Status.Allocatable[name].DeepCopy()
		usage := ResourceUsage{Resource: name, Allocatable: allocatable}
		if options.PodsKnown {
			requestedQuantity := requested[name].DeepCopy()
			usage.Requested = requestedQuantity
			usage.RequestsKnown = true
			if options.Namespace == "" {
				available := allocatable.DeepCopy()
				available.Sub(requestedQuantity)
				usage.Available = available
				usage.AvailableKnown = true
			}
			if !allocatable.IsZero() {
				usage.Ratio = requestedQuantity.AsApproximateFloat64() / allocatable.AsApproximateFloat64() * 100
				usage.RatioKnown = true
			}
		}
		report.Resources = append(report.Resources, usage)
	}

	if options.PodsKnown && options.Top > 0 {
		for _, entry := range activeRequests {
			quantity := entry.requests[options.TopResource]
			if quantity.IsZero() {
				continue
			}
			owner, daemonSet := podOwner(entry.pod)
			report.Consumers = append(report.Consumers, Consumer{
				Namespace: entry.pod.Namespace,
				Pod:       entry.pod.Name,
				UID:       string(entry.pod.UID),
				Request:   quantity.DeepCopy(),
				Owner:     owner,
				DaemonSet: daemonSet,
			})
		}
		sort.Slice(report.Consumers, func(i, j int) bool {
			left, right := report.Consumers[i], report.Consumers[j]
			if comparison := left.Request.Cmp(right.Request); comparison != 0 {
				return comparison > 0
			}
			if left.Namespace != right.Namespace {
				return left.Namespace < right.Namespace
			}
			if left.Pod != right.Pod {
				return left.Pod < right.Pod
			}
			return left.UID < right.UID
		})
		if len(report.Consumers) > options.Top {
			report.Consumers = report.Consumers[:options.Top]
		}
	}

	if report.ExtendedKnown {
		for _, entry := range activeRequests {
			owner, daemonSet := podOwner(entry.pod)
			for name, quantity := range entry.requests {
				if !isExtendedResourceName(name) || quantity.IsZero() ||
					(options.ResourceFilter != "" && name != options.ResourceFilter) {
					continue
				}
				report.ExtendedConsumers = append(report.ExtendedConsumers, ExtendedResourceConsumer{
					Resource:  name,
					Namespace: entry.pod.Namespace,
					Pod:       entry.pod.Name,
					UID:       string(entry.pod.UID),
					Request:   quantity.DeepCopy(),
					Owner:     owner,
					DaemonSet: daemonSet,
				})
			}
		}
		sort.Slice(report.ExtendedConsumers, func(i, j int) bool {
			left, right := report.ExtendedConsumers[i], report.ExtendedConsumers[j]
			if left.Resource != right.Resource {
				return left.Resource < right.Resource
			}
			if comparison := left.Request.Cmp(right.Request); comparison != 0 {
				return comparison > 0
			}
			if left.Namespace != right.Namespace {
				return left.Namespace < right.Namespace
			}
			if left.Pod != right.Pod {
				return left.Pod < right.Pod
			}
			return left.UID < right.UID
		})
	}

	if report.PodResourcesKnown {
		for _, entry := range activeRequests {
			owner, daemonSet := podOwner(entry.pod)
			breakdown := PodResourceBreakdown{
				Namespace: entry.pod.Namespace,
				Pod:       entry.pod.Name,
				UID:       string(entry.pod.UID),
				Owner:     owner,
				DaemonSet: daemonSet,
			}
			resourceSet := make(map[corev1.ResourceName]struct{}, len(entry.requests)+len(entry.limits))
			for name, quantity := range entry.requests {
				if !quantity.IsZero() && (options.ResourceFilter == "" || name == options.ResourceFilter) {
					resourceSet[name] = struct{}{}
				}
			}
			for name, quantity := range entry.limits {
				if !quantity.IsZero() && (options.ResourceFilter == "" || name == options.ResourceFilter) {
					resourceSet[name] = struct{}{}
				}
			}
			resourceNames := make([]corev1.ResourceName, 0, len(resourceSet))
			for name := range resourceSet {
				resourceNames = append(resourceNames, name)
			}
			sort.Slice(resourceNames, func(i, j int) bool {
				leftRank, rightRank := resourceRank(resourceNames[i]), resourceRank(resourceNames[j])
				if leftRank != rightRank {
					return leftRank < rightRank
				}
				return resourceNames[i] < resourceNames[j]
			})
			for _, name := range resourceNames {
				request, requestSet := entry.requests[name]
				limit, limitSet := entry.limits[name]
				usage := PodResource{
					Resource:   name,
					Request:    request.DeepCopy(),
					Limit:      limit.DeepCopy(),
					RequestSet: requestSet && !request.IsZero(),
					LimitSet:   limitSet && !limit.IsZero(),
				}
				allocatable := item.Status.Allocatable[name]
				if usage.RequestSet && !allocatable.IsZero() {
					usage.RequestRatio = request.AsApproximateFloat64() / allocatable.AsApproximateFloat64() * 100
					usage.RatioKnown = true
				}
				breakdown.Resources = append(breakdown.Resources, usage)
			}
			if options.ResourceFilter == "" || len(breakdown.Resources) > 0 {
				report.PodResources = append(report.PodResources, breakdown)
			}
		}
		sort.Slice(report.PodResources, func(i, j int) bool {
			left, right := report.PodResources[i], report.PodResources[j]
			if left.Namespace != right.Namespace {
				return left.Namespace < right.Namespace
			}
			if left.Pod != right.Pod {
				return left.Pod < right.Pod
			}
			return left.UID < right.UID
		})
	}
	return report
}

func isExtendedResourceName(name corev1.ResourceName) bool {
	value := string(name)
	return strings.Contains(value, "/") &&
		!strings.Contains(value, corev1.ResourceDefaultNamespacePrefix) &&
		!strings.HasPrefix(value, corev1.DefaultResourceRequestsPrefix)
}

func addResourceList(target, addition corev1.ResourceList) {
	for name, quantity := range addition {
		current := target[name]
		current.Add(quantity)
		target[name] = current
	}
}

func isTerminalPod(item *corev1.Pod) bool {
	return item.Status.Phase == corev1.PodSucceeded || item.Status.Phase == corev1.PodFailed
}

func podOwner(item *corev1.Pod) (string, bool) {
	if len(item.OwnerReferences) == 0 {
		return "<none>", false
	}
	owner := item.OwnerReferences[0]
	for _, candidate := range item.OwnerReferences {
		if candidate.Controller != nil && *candidate.Controller {
			owner = candidate
			break
		}
	}
	return owner.Kind + "/" + owner.Name, owner.Kind == "DaemonSet"
}

func resourceRank(name corev1.ResourceName) int {
	switch name {
	case corev1.ResourceCPU:
		return 0
	case corev1.ResourceMemory:
		return 1
	case corev1.ResourceEphemeralStorage:
		return 2
	case corev1.ResourcePods:
		return 3
	default:
		if strings.HasPrefix(string(name), corev1.ResourceHugePagesPrefix) {
			return 4
		}
		return 5
	}
}
