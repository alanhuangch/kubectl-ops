package rollout

import (
	"testing"
	"time"

	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAnalyzeExplainsDegradedDeployment(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	replicas := int32(2)
	controller := true
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "api", UID: types.UID("deployment-uid"), Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 1, AvailableReplicas: 1, UnavailableReplicas: 1,
			Conditions: []appsv1.DeploymentCondition{{
				Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded",
				Message: "ReplicaSet api-new has timed out progressing.", LastTransitionTime: metav1.NewTime(now.Add(-time.Minute)),
			}},
		},
	}
	current := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "production", Name: "api-new", UID: types.UID("rs-new"), CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
			Annotations:     map[string]string{deploymentRevisionAnnotation: "2"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: deployment.UID, Controller: &controller}},
		},
		Spec:   appsv1.ReplicaSetSpec{Replicas: &replicas},
		Status: appsv1.ReplicaSetStatus{Replicas: 2, ReadyReplicas: 1, AvailableReplicas: 1},
	}
	zero := int32(0)
	old := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "production", Name: "api-old", UID: types.UID("rs-old"), CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
			Annotations:     map[string]string{deploymentRevisionAnnotation: "1"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: deployment.UID, Controller: &controller}},
		},
		Spec: appsv1.ReplicaSetSpec{Replicas: &zero},
	}
	crashing := rolloutTestPod("api-new-a", current.UID, now.Add(-9*time.Minute))
	crashing.Status.Phase = corev1.PodRunning
	crashing.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
	crashing.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "api", RestartCount: 3, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
	}}
	pending := rolloutTestPod("api-new-b", current.UID, now.Add(-8*time.Minute))
	pending.Status.Phase = corev1.PodPending
	unrelated := rolloutTestPod("other", types.UID("other-rs"), now.Add(-time.Minute))
	events := []timeline.TimelineEvent{
		{UID: "event-1", Type: corev1.EventTypeWarning, Reason: "BackOff", Note: "back-off restarting", Regarding: timeline.ObjectReference{Kind: "Pod", Name: crashing.Name, UID: string(crashing.UID)}, LastObservedAt: now.Add(-time.Minute), Count: 4},
		{UID: "event-2", Type: corev1.EventTypeNormal, Reason: "ScalingReplicaSet", Note: "scaled up", Regarding: timeline.ObjectReference{Kind: "Deployment", Name: deployment.Name, UID: string(deployment.UID)}, LastObservedAt: now.Add(-2 * time.Minute), Count: 1},
		{UID: "event-3", Type: corev1.EventTypeWarning, Reason: "Unrelated", Regarding: timeline.ObjectReference{Kind: "Pod", Name: unrelated.Name, UID: string(unrelated.UID)}, LastObservedAt: now, Count: 1},
	}

	report := Analyze(Snapshot{
		CapturedAt: now, Deployment: deployment, ReplicaSets: []appsv1.ReplicaSet{old, current},
		Pods: []corev1.Pod{unrelated, pending, crashing}, Events: events,
		ReplicaSetsKnown: true, PodsKnown: true, EventsKnown: true,
	}, Options{EventLimit: 2})

	if report.Status != StatusDegraded || report.Completeness != CompletenessComplete {
		t.Fatalf("status = %s/%s", report.Status, report.Completeness)
	}
	if len(report.ReplicaSets) != 2 || report.ReplicaSets[0].Name != "api-new" || !report.ReplicaSets[0].Current {
		t.Fatalf("ReplicaSets = %#v", report.ReplicaSets)
	}
	if len(report.Pods) != 2 || report.Pods[0].Name != "api-new-a" || report.Pods[0].Restarts != 3 {
		t.Fatalf("Pods = %#v", report.Pods)
	}
	if len(report.Events) != 2 || report.Events[0].Reason != "BackOff" {
		t.Fatalf("Events = %#v", report.Events)
	}
	assertFinding(t, report.Findings, SeverityError, "ProgressDeadlineExceeded")
	assertFinding(t, report.Findings, SeverityError, "ContainerWaiting")
	assertFinding(t, report.Findings, SeverityWarning, "PodPending")
}

func TestAnalyzeHealthyAndPartialStates(t *testing.T) {
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "api", UID: types.UID("deployment-uid"), Generation: 3},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 3, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
		},
	}
	healthy := Analyze(Snapshot{Deployment: deployment, ReplicaSetsKnown: true, PodsKnown: true, EventsKnown: true}, Options{})
	if healthy.Status != StatusHealthy || healthy.Completeness != CompletenessComplete {
		t.Fatalf("healthy report = %#v", healthy)
	}

	partial := Analyze(Snapshot{
		Deployment: deployment, ReplicaSetsKnown: false, PodsKnown: true, EventsKnown: false,
		Warnings: []string{"details unavailable"},
	}, Options{})
	if partial.Status != StatusUnknown || partial.Completeness != CompletenessPartial || len(partial.Warnings) != 1 {
		t.Fatalf("partial report = %#v", partial)
	}
}

func rolloutTestPod(name string, ownerUID types.UID, createdAt time.Time) corev1.Pod {
	controller := true
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "production", Name: name, UID: types.UID(name), CreationTimestamp: metav1.NewTime(createdAt),
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-new", UID: ownerUID, Controller: &controller}},
		},
		Spec: corev1.PodSpec{NodeName: "worker-07"},
	}
}

func assertFinding(t *testing.T, findings []Finding, severity Severity, code string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Severity == severity && finding.Code == code {
			return
		}
	}
	t.Fatalf("finding %s/%s not found in %#v", severity, code, findings)
}
