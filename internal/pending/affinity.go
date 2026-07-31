package pending

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
)

func placementFailures(item *corev1.Pod, node *corev1.Node) []Failure {
	var failures []Failure
	if node.Spec.Unschedulable {
		failures = append(failures, Failure{
			Code:     "NodeUnschedulable",
			Category: "Placement",
			Source:   SourceCurrentState,
			Message:  "node is cordoned",
		})
	}
	if item.Spec.NodeName != "" && item.Spec.NodeName != node.Name {
		failures = append(failures, Failure{
			Code:     "NodeNameMismatch",
			Category: "Placement",
			Source:   SourceCurrentState,
			Message:  "pod specifies a different nodeName",
			Details:  map[string]string{"requiredNode": item.Spec.NodeName},
		})
	}
	if len(item.Spec.NodeSelector) > 0 && !labels.SelectorFromSet(item.Spec.NodeSelector).Matches(labels.Set(node.Labels)) {
		failures = append(failures, Failure{
			Code:     "NodeSelectorMismatch",
			Category: "Placement",
			Source:   SourceCurrentState,
			Message:  "node labels do not match pod nodeSelector",
		})
	}

	if hasRequiredNodeAffinity(item) {
		copy := item.DeepCopy()
		copy.Spec.NodeSelector = nil
		matched, err := nodeaffinity.GetRequiredNodeAffinity(copy).Match(node)
		switch {
		case err != nil:
			failures = append(failures, Failure{
				Code:     "InvalidRequiredNodeAffinity",
				Category: "Placement",
				Source:   SourceCurrentState,
				Message:  "required node affinity could not be evaluated",
				Details:  map[string]string{"error": err.Error()},
			})
		case !matched:
			failures = append(failures, Failure{
				Code:     "RequiredNodeAffinityMismatch",
				Category: "Placement",
				Source:   SourceCurrentState,
				Message:  fmt.Sprintf("node %q does not match required node affinity", node.Name),
			})
		}
	}
	return failures
}

func hasRequiredNodeAffinity(item *corev1.Pod) bool {
	return item.Spec.Affinity != nil &&
		item.Spec.Affinity.NodeAffinity != nil &&
		item.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil
}
