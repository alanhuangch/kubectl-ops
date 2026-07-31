package pending

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAnalyzeReportsSupportedNodeFailures(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	target := pendingTestPod("target")
	target.Spec.NodeSelector = map[string]string{"zone": "east"}
	target.Spec.Affinity = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
			MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "disk", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}}},
		}}},
	}}
	target.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")
	target.Spec.Containers[0].Ports = []corev1.ContainerPort{{HostPort: 8080, Protocol: corev1.ProtocolTCP}}

	node := pendingTestNode("worker-b", "1")
	node.Labels = map[string]string{"zone": "west", "disk": "hdd"}
	node.Spec.Unschedulable = true
	node.Spec.Taints = []corev1.Taint{{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule}}
	occupied := pendingTestPod("occupied")
	occupied.Namespace = "production"
	occupied.Spec.NodeName = node.Name
	occupied.Status.Phase = corev1.PodRunning
	occupied.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
	occupied.Spec.Containers[0].Ports = []corev1.ContainerPort{{HostPort: 8080, Protocol: corev1.ProtocolTCP}}

	report := Analyze(Snapshot{
		CapturedAt:  now,
		TargetPod:   target,
		Nodes:       []corev1.Node{node},
		PodsByNode:  map[string][]corev1.Pod{node.Name: {*occupied}},
		NodesKnown:  true,
		PodsKnown:   true,
		EventsKnown: true,
	}, Options{Closest: 5, TopConsumers: 5})

	wantCodes := []string{
		"HostPortConflict",
		"NodeSelectorMismatch",
		"NodeUnschedulable",
		"RequiredNodeAffinityMismatch",
		"InsufficientCPU",
		"UntoleratedTaint",
	}
	for _, code := range wantCodes {
		if !nodeHasFailure(report.Nodes[0], code) {
			t.Fatalf("failure %s not found: %#v", code, report.Nodes[0].Failures)
		}
	}
	if report.Nodes[0].LimitingResource != "cpu" || len(report.Nodes[0].TopConsumers) != 1 {
		t.Fatalf("unexpected resource context: %#v", report.Nodes[0])
	}
}

func TestAnalyzeClosestNodesUsesDocumentedOrdering(t *testing.T) {
	target := pendingTestPod("target")
	target.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")
	good := pendingTestNode("good", "4")
	resourceOnly := pendingTestNode("resource", "1")
	placement := pendingTestNode("placement", "4")
	placement.Spec.Unschedulable = true

	report := Analyze(Snapshot{
		CapturedAt:  time.Now(),
		TargetPod:   target,
		Nodes:       []corev1.Node{placement, resourceOnly, good},
		PodsByNode:  map[string][]corev1.Pod{},
		NodesKnown:  true,
		PodsKnown:   true,
		EventsKnown: true,
	}, Options{Closest: 3})

	if report.ClosestNodes[0].Node != "good" || report.ClosestNodes[1].Node != "resource" || report.ClosestNodes[2].Node != "placement" {
		t.Fatalf("unexpected closest order: %#v", report.ClosestNodes)
	}
}

func TestAnalyzeObservedAndUnsupportedData(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	target := pendingTestPod("target")
	target.Spec.SchedulerName = "custom-scheduler"
	target.Spec.Volumes = []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}
	target.Status.Conditions = []corev1.PodCondition{{
		Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "0/2 nodes are available", LastTransitionTime: metav1.NewTime(now.Add(-time.Minute)),
	}}
	events := []corev1.Event{
		{Reason: "Pulled", LastTimestamp: metav1.NewTime(now)},
		{Reason: "FailedScheduling", Type: corev1.EventTypeWarning, Message: "newer", Count: 2, LastTimestamp: metav1.NewTime(now.Add(-time.Minute))},
		{Reason: "FailedScheduling", Type: corev1.EventTypeWarning, Message: "older", Count: 1, LastTimestamp: metav1.NewTime(now.Add(-2 * time.Minute))},
	}
	report := Analyze(Snapshot{
		CapturedAt: now, TargetPod: target, Events: events, NodesKnown: false, PodsKnown: false, EventsKnown: true,
	}, Options{Closest: 5})

	if report.Completeness != CompletenessPartial || report.Condition == nil || len(report.Events) != 2 {
		t.Fatalf("unexpected observed report: %#v", report)
	}
	if report.Events[0].Message != "newer" {
		t.Fatalf("events not sorted: %#v", report.Events)
	}
	if !hasUnsupported(report, "CustomScheduler") || !hasUnsupported(report, "VolumeBinding") {
		t.Fatalf("unexpected unsupported: %#v", report.Unsupported)
	}
}

func TestHostIPConflictSemantics(t *testing.T) {
	if !hostIPsOverlap("", "127.0.0.1") || !hostIPsOverlap("0.0.0.0", "10.0.0.1") || hostIPsOverlap("127.0.0.1", "127.0.0.2") {
		t.Fatal("unexpected host IP overlap behavior")
	}
}

func nodeHasFailure(node NodeResult, code string) bool {
	for _, failure := range node.Failures {
		if failure.Code == code {
			return true
		}
	}
	return false
}

func hasUnsupported(report Report, code string) bool {
	for _, item := range report.Unsupported {
		if item.Code == code {
			return true
		}
	}
	return false
}

func pendingTestPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name, UID: types.UID(name)},
		Spec: corev1.PodSpec{
			SchedulerName: corev1.DefaultSchedulerName,
			Containers:    []corev1.Container{{Name: "main", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
}

func pendingTestNode(name, cpu string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:  resource.MustParse(cpu),
			corev1.ResourcePods: resource.MustParse("10"),
		}},
	}
}
