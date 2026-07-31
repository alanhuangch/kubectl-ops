package pending

import (
	corev1 "k8s.io/api/core/v1"
	schedulingcorev1 "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/klog/v2"
)

func taintFailures(item *corev1.Pod, node *corev1.Node) []Failure {
	taint, untolerated := schedulingcorev1.FindMatchingUntoleratedTaint(
		klog.Background(),
		node.Spec.Taints,
		item.Spec.Tolerations,
		func(taint *corev1.Taint) bool {
			return taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute
		},
		false,
	)
	if !untolerated {
		return nil
	}
	return []Failure{{
		Code:     "UntoleratedTaint",
		Category: "Taints",
		Source:   SourceCurrentState,
		Message:  "pod does not tolerate a node taint",
		Details: map[string]string{
			"key":    taint.Key,
			"value":  taint.Value,
			"effect": string(taint.Effect),
		},
	}}
}
