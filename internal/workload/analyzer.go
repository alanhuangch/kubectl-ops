package workload

import (
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resourcehelper "k8s.io/component-helpers/resource"
)

func AnalyzeResources(snapshot Snapshot, options Options) Report {
	report := Report{
		CapturedAt:    snapshot.CapturedAt,
		Namespace:     snapshot.Namespace,
		ResourceClass: options.ResourceClass,
		Completeness:  CompletenessComplete,
		Warnings:      append([]string(nil), snapshot.Warnings...),
	}
	if !snapshot.InventoryComplete || !snapshot.PodsKnown {
		report.Completeness = CompletenessPartial
	}

	workloadIndexes := make(map[string]int, len(snapshot.Workloads))
	plannedGPUs := make([]map[corev1.ResourceName]resource.Quantity, 0, len(snapshot.Workloads))
	actualGPUs := make([]map[corev1.ResourceName]resource.Quantity, 0, len(snapshot.Workloads))
	for _, item := range snapshot.Workloads {
		actualKnown := snapshot.PodsKnown && item.ActualKnown
		if !actualKnown {
			report.Completeness = CompletenessPartial
		}
		requests := resourcehelper.PodRequests(&corev1.Pod{Spec: *item.Template.Spec.DeepCopy()}, resourcehelper.PodResourcesOptions{})
		usage := ResourceUsage{
			Namespace: item.Namespace,
			Kind:      item.Kind,
			Workload:  item.Name,
			UID:       item.UID,
			Pods: PodCounts{
				Planned:     item.PlannedPods,
				ActualKnown: actualKnown,
			},
			CPU: ResourcePair{
				Planned:     multipliedQuantity(requests[corev1.ResourceCPU], item.PlannedPods),
				ActualKnown: actualKnown,
			},
			Memory: ResourcePair{
				Planned:     multipliedQuantity(requests[corev1.ResourceMemory], item.PlannedPods),
				ActualKnown: actualKnown,
			},
			Placement: normalizePlacement(item.Template.Spec),
		}
		report.Items = append(report.Items, usage)
		workloadIndexes[item.UID] = len(report.Items) - 1
		plannedGPUs = append(plannedGPUs, scaledGPURequests(requests, item.PlannedPods))
		actualGPUs = append(actualGPUs, map[corev1.ResourceName]resource.Quantity{})
	}

	ownerLinks := make(map[string]string, len(snapshot.OwnerLinks))
	for _, item := range snapshot.OwnerLinks {
		ownerLinks[item.UID] = item.OwnerUID
	}
	if snapshot.PodsKnown {
		for index := range snapshot.Pods {
			pod := &snapshot.Pods[index]
			if pod.Spec.NodeName == "" || isTerminalPod(pod) {
				continue
			}
			workloadIndex, found := workloadForPod(pod, workloadIndexes, ownerLinks)
			if !found || report.Items[workloadIndex].Namespace != pod.Namespace || !report.Items[workloadIndex].Pods.ActualKnown {
				continue
			}
			requests := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
			usage := &report.Items[workloadIndex]
			usage.Pods.Actual++
			usage.CPU.Actual.Add(requests[corev1.ResourceCPU])
			usage.Memory.Actual.Add(requests[corev1.ResourceMemory])
			for name, quantity := range requests {
				if !isGPUResource(name) || quantity.IsZero() {
					continue
				}
				current := actualGPUs[workloadIndex][name]
				current.Add(quantity)
				actualGPUs[workloadIndex][name] = current
			}
		}
	}

	filtered := make([]ResourceUsage, 0, len(report.Items))
	for index := range report.Items {
		report.Items[index].GPUs = mergeGPURequests(plannedGPUs[index], actualGPUs[index], report.Items[index].Pods.ActualKnown)
		if matchesResourceClass(report.Items[index], options.ResourceClass) {
			filtered = append(filtered, report.Items[index])
		}
	}
	report.Items = filtered
	sort.Slice(report.Items, func(i, j int) bool {
		left, right := report.Items[i], report.Items[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Workload != right.Workload {
			return left.Workload < right.Workload
		}
		return left.UID < right.UID
	})
	if report.Warnings == nil {
		report.Warnings = []string{}
	}
	return report
}

func workloadForPod(pod *corev1.Pod, workloads map[string]int, ownerLinks map[string]string) (int, bool) {
	if index, found := workloads[string(pod.UID)]; found {
		return index, true
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return 0, false
	}
	uid := string(owner.UID)
	for depth := 0; depth < 8 && uid != ""; depth++ {
		if index, found := workloads[uid]; found {
			return index, true
		}
		next, found := ownerLinks[uid]
		if !found || next == uid {
			return 0, false
		}
		uid = next
	}
	return 0, false
}

func matchesResourceClass(item ResourceUsage, class ResourceClass) bool {
	switch class {
	case "", ResourceClassAll:
		return true
	case ResourceClassCPU:
		return !item.CPU.Planned.IsZero() || (item.CPU.ActualKnown && !item.CPU.Actual.IsZero())
	case ResourceClassMemory:
		return !item.Memory.Planned.IsZero() || (item.Memory.ActualKnown && !item.Memory.Actual.IsZero())
	case ResourceClassGPU:
		for _, gpu := range item.GPUs {
			if !gpu.Planned.IsZero() || (gpu.ActualKnown && !gpu.Actual.IsZero()) {
				return true
			}
		}
	}
	return false
}

func multipliedQuantity(value resource.Quantity, multiplier int32) resource.Quantity {
	result := value.DeepCopy()
	result.Mul(int64(multiplier))
	return result
}

func scaledGPURequests(requests corev1.ResourceList, replicas int32) map[corev1.ResourceName]resource.Quantity {
	result := map[corev1.ResourceName]resource.Quantity{}
	for name, quantity := range requests {
		if isGPUResource(name) && !quantity.IsZero() {
			result[name] = multipliedQuantity(quantity, replicas)
		}
	}
	return result
}

func mergeGPURequests(planned, actual map[corev1.ResourceName]resource.Quantity, actualKnown bool) []GPUResourcePair {
	names := make(map[corev1.ResourceName]struct{}, len(planned)+len(actual))
	for name := range planned {
		names[name] = struct{}{}
	}
	for name := range actual {
		names[name] = struct{}{}
	}
	ordered := make([]corev1.ResourceName, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	result := make([]GPUResourcePair, 0, len(ordered))
	for _, name := range ordered {
		result = append(result, GPUResourcePair{
			Resource:    name,
			Planned:     planned[name].DeepCopy(),
			Actual:      actual[name].DeepCopy(),
			ActualKnown: actualKnown,
		})
	}
	return result
}

func isGPUResource(name corev1.ResourceName) bool {
	value := strings.ToLower(string(name))
	return strings.Contains(value, "gpu") || strings.HasPrefix(value, "nvidia.com/mig-")
}

func isTerminalPod(item *corev1.Pod) bool {
	return item.Status.Phase == corev1.PodSucceeded || item.Status.Phase == corev1.PodFailed
}

func normalizePlacement(spec corev1.PodSpec) NodePlacement {
	result := NodePlacement{}
	for key, value := range spec.NodeSelector {
		result.NodeSelector = append(result.NodeSelector, KeyValue{Key: key, Value: value})
	}
	sort.Slice(result.NodeSelector, func(i, j int) bool { return result.NodeSelector[i].Key < result.NodeSelector[j].Key })
	if spec.Affinity == nil || spec.Affinity.NodeAffinity == nil {
		return result
	}
	affinity := spec.Affinity.NodeAffinity
	if affinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		for _, term := range affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			result.Required = append(result.Required, normalizeTerm(term))
		}
		sort.Slice(result.Required, func(i, j int) bool { return termKey(result.Required[i]) < termKey(result.Required[j]) })
	}
	for _, term := range affinity.PreferredDuringSchedulingIgnoredDuringExecution {
		result.Preferred = append(result.Preferred, PreferredNodeSelectorTerm{Weight: term.Weight, Preference: normalizeTerm(term.Preference)})
	}
	sort.Slice(result.Preferred, func(i, j int) bool {
		if result.Preferred[i].Weight != result.Preferred[j].Weight {
			return result.Preferred[i].Weight > result.Preferred[j].Weight
		}
		return termKey(result.Preferred[i].Preference) < termKey(result.Preferred[j].Preference)
	})
	return result
}

func normalizeTerm(term corev1.NodeSelectorTerm) NodeSelectorTerm {
	return NodeSelectorTerm{
		MatchExpressions: normalizeRequirements(term.MatchExpressions),
		MatchFields:      normalizeRequirements(term.MatchFields),
	}
}

func normalizeRequirements(items []corev1.NodeSelectorRequirement) []NodeSelectorRequirement {
	result := make([]NodeSelectorRequirement, 0, len(items))
	for _, item := range items {
		values := append([]string(nil), item.Values...)
		sort.Strings(values)
		result = append(result, NodeSelectorRequirement{Key: item.Key, Operator: item.Operator, Values: values})
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.Operator != right.Operator {
			return left.Operator < right.Operator
		}
		return strings.Join(left.Values, "\x00") < strings.Join(right.Values, "\x00")
	})
	return result
}

func termKey(term NodeSelectorTerm) string {
	var parts []string
	for _, item := range term.MatchExpressions {
		parts = append(parts, "e:"+requirementKey(item))
	}
	for _, item := range term.MatchFields {
		parts = append(parts, "f:"+requirementKey(item))
	}
	return strings.Join(parts, "|")
}

func requirementKey(item NodeSelectorRequirement) string {
	return item.Key + ":" + string(item.Operator) + ":" + strings.Join(item.Values, ",")
}
