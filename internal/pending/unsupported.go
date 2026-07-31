package pending

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
)

func detectUnsupported(item *corev1.Pod) []Unsupported {
	unsupported := map[string]string{}
	for _, volume := range item.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil || volume.Ephemeral != nil {
			unsupported["VolumeBinding"] = "volume binding and storage topology are not evaluated"
		}
	}
	if len(item.Spec.TopologySpreadConstraints) > 0 {
		unsupported["TopologySpread"] = "topology spread constraints are not evaluated"
	}
	if item.Spec.Affinity != nil {
		if item.Spec.Affinity.PodAffinity != nil {
			unsupported["PodAffinity"] = "pod affinity is not evaluated"
		}
		if item.Spec.Affinity.PodAntiAffinity != nil {
			unsupported["PodAntiAffinity"] = "pod anti-affinity is not evaluated"
		}
	}
	if len(item.Spec.SchedulingGates) > 0 {
		unsupported["SchedulingGates"] = "pod scheduling gates are not evaluated"
	}
	if item.Spec.SchedulerName != "" && item.Spec.SchedulerName != corev1.DefaultSchedulerName {
		unsupported["CustomScheduler"] = "custom scheduler plugins and extenders are not evaluated"
	}
	hasNonDefaultPriority := item.Spec.PriorityClassName != "" || (item.Spec.Priority != nil && *item.Spec.Priority != 0)
	if hasNonDefaultPriority && (item.Spec.PreemptionPolicy == nil || *item.Spec.PreemptionPolicy != corev1.PreemptNever) {
		unsupported["Preemption"] = "preemption is not evaluated"
	}
	if len(item.Spec.ResourceClaims) > 0 {
		unsupported["DynamicResourceAllocation"] = "dynamic resource allocation is not evaluated"
	}

	result := make([]Unsupported, 0, len(unsupported))
	for code, message := range unsupported {
		result = append(result, Unsupported{Code: code, Message: message})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}
