package drain

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAnalyzeClassifiesPodsAndPDBBlockers(t *testing.T) {
	controller := true
	alwaysAllow := policyv1.AlwaysAllow
	pods := []corev1.Pod{
		testPod("z-system", "mirror", corev1.PodRunning, nil, nil),
		testPod("production", "unmanaged-local", corev1.PodRunning, map[string]string{"app": "other"}, nil),
		testPod("production", "healthy", corev1.PodRunning, map[string]string{"app": "api"}, &metav1.OwnerReference{Kind: "ReplicaSet", Name: "api-rs", Controller: &controller}),
		testPod("production", "unhealthy", corev1.PodRunning, map[string]string{"app": "api"}, &metav1.OwnerReference{Kind: "ReplicaSet", Name: "api-rs", Controller: &controller}),
		testPod("production", "daemon", corev1.PodRunning, nil, &metav1.OwnerReference{Kind: "DaemonSet", Name: "agent", Controller: &controller}),
		testPod("production", "complete", corev1.PodSucceeded, nil, nil),
	}
	pods[0].Annotations = map[string]string{corev1.MirrorPodAnnotationKey: "mirror-hash"}
	pods[1].Spec.Volumes = []corev1.Volume{{
		Name: "cache",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}}
	pods[2].Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	pods[3].Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
	pdbs := []policyv1.PodDisruptionBudget{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "api"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector:                   &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			UnhealthyPodEvictionPolicy: &alwaysAllow,
		},
		Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
	}}

	report := Analyze("worker-07", pods, pdbs, Options{Now: time.Now(), PDBsKnown: true})

	if report.Drainability != DrainabilityBlocked || report.Completeness != CompletenessComplete {
		t.Fatalf("status = %s/%s", report.Drainability, report.Completeness)
	}
	if report.Summary != (Summary{Pods: 6, Evict: 1, Delete: 1, Skip: 1, Blocked: 3}) {
		t.Fatalf("summary = %#v", report.Summary)
	}
	assertPodImpact(t, report.Pods, "complete", PodActionDelete, []ImpactCode{ImpactTerminalPod}, nil)
	assertPodImpact(t, report.Pods, "daemon", PodActionBlocked, []ImpactCode{ImpactDaemonSetManaged}, []ImpactCode{ImpactDaemonSetManaged})
	assertPodImpact(t, report.Pods, "healthy", PodActionBlocked, []ImpactCode{ImpactPDBViolation}, []ImpactCode{ImpactPDBViolation})
	assertPodImpact(t, report.Pods, "unhealthy", PodActionEvict, nil, nil)
	assertPodImpact(t, report.Pods, "unmanaged-local", PodActionBlocked, []ImpactCode{ImpactUnmanagedPod, ImpactLocalStorage}, []ImpactCode{ImpactUnmanagedPod, ImpactLocalStorage})
	assertPodImpact(t, report.Pods, "mirror", PodActionSkip, []ImpactCode{ImpactMirrorPod}, nil)
	if len(report.PDBs) != 1 || report.PDBs[0].MatchedTargets != 2 || report.PDBs[0].UnhealthyExempt != 1 || !report.PDBs[0].Blocked {
		t.Fatalf("PDB impact = %#v", report.PDBs)
	}
}

func TestAnalyzeOptionsMakePodsDrainable(t *testing.T) {
	controller := true
	pods := []corev1.Pod{
		testPod("production", "daemon", corev1.PodRunning, nil, &metav1.OwnerReference{Kind: "DaemonSet", Name: "agent", Controller: &controller}),
		testPod("production", "unmanaged-local", corev1.PodRunning, nil, nil),
	}
	pods[1].Spec.Volumes = []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}

	report := Analyze("worker-07", pods, nil, Options{
		PDBsKnown:          true,
		IgnoreDaemonSets:   true,
		Force:              true,
		DeleteEmptyDirData: true,
	})

	if report.Drainability != DrainabilityReady || report.Summary != (Summary{Pods: 2, Evict: 1, Skip: 1}) {
		t.Fatalf("report = %#v", report)
	}
	assertPodImpact(t, report.Pods, "daemon", PodActionSkip, []ImpactCode{ImpactDaemonSetManaged}, nil)
	assertPodImpact(t, report.Pods, "unmanaged-local", PodActionEvict, []ImpactCode{ImpactUnmanagedPod, ImpactLocalStorage}, nil)
}

func TestAnalyzePDBAvailabilityControlsUnknownState(t *testing.T) {
	controller := true
	pod := testPod("production", "api", corev1.PodRunning, nil, &metav1.OwnerReference{Kind: "ReplicaSet", Name: "api-rs", Controller: &controller})

	unknown := Analyze("worker-07", []corev1.Pod{pod}, nil, Options{PDBsKnown: false})
	if unknown.Drainability != DrainabilityUnknown || unknown.Completeness != CompletenessPartial || len(unknown.Warnings) != 1 {
		t.Fatalf("unknown report = %#v", unknown)
	}

	unmanaged := testPod("production", "manual", corev1.PodRunning, nil, nil)
	blocked := Analyze("worker-07", []corev1.Pod{unmanaged}, nil, Options{PDBsKnown: false})
	if blocked.Drainability != DrainabilityBlocked || blocked.Completeness != CompletenessPartial {
		t.Fatalf("blocked report = %#v", blocked)
	}
}

func TestAnalyzeInvalidPDBSelectorIsPartial(t *testing.T) {
	controller := true
	pod := testPod("production", "api", corev1.PodRunning, nil, &metav1.OwnerReference{Kind: "ReplicaSet", Name: "api-rs", Controller: &controller})
	pdb := policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "invalid"},
		Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key: "app", Operator: metav1.LabelSelectorOpExists, Values: []string{"unexpected"},
		}}}},
	}

	report := Analyze("worker-07", []corev1.Pod{pod}, []policyv1.PodDisruptionBudget{pdb}, Options{PDBsKnown: true})

	if report.Drainability != DrainabilityUnknown || report.Completeness != CompletenessPartial {
		t.Fatalf("status = %s/%s", report.Drainability, report.Completeness)
	}
	if len(report.PDBs) != 1 || report.PDBs[0].SelectorValid || len(report.Warnings) != 1 {
		t.Fatalf("PDB report = %#v, warnings = %#v", report.PDBs, report.Warnings)
	}
}

func testPod(namespace, name string, phase corev1.PodPhase, labels map[string]string, owner *metav1.OwnerReference) corev1.Pod {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID(name), Labels: labels},
		Spec:       corev1.PodSpec{NodeName: "worker-07"},
		Status:     corev1.PodStatus{Phase: phase},
	}
	if owner != nil {
		pod.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return pod
}

func assertPodImpact(t *testing.T, pods []PodImpact, name string, action PodAction, impacts, blockers []ImpactCode) {
	t.Helper()
	for _, pod := range pods {
		if pod.Pod != name {
			continue
		}
		if pod.Action != action || !equalCodes(pod.Impacts, impacts) || !equalCodes(pod.Blockers, blockers) {
			t.Fatalf("%s = %#v", name, pod)
		}
		return
	}
	t.Fatalf("Pod %q not found", name)
}

func equalCodes(actual, expected []ImpactCode) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}
