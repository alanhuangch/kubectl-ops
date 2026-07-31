package pod

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAnalyzeRecentFiltersSortsAndSummarizes(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	pods := []corev1.Pod{
		recentTestPod("team-b", "later", "uid-2", "worker-07", now.Add(-2*time.Minute), now.Add(-30*time.Second)),
		recentTestPod("team-a", "alpha", "uid-1", "worker-07", now.Add(-3*time.Minute), now.Add(-time.Minute)),
		recentTestPod("team-a", "beta", "uid-3", "worker-07", now.Add(-3*time.Minute), now.Add(-time.Minute)),
		recentTestPod("team-a", "old", "uid-4", "worker-07", now.Add(-3*time.Hour), now.Add(-2*time.Hour)),
		recentTestPod("team-a", "other-node", "uid-5", "worker-08", now.Add(-time.Minute), now.Add(-10*time.Second)),
	}
	staticPod := recentTestPod("kube-system", "static", "uid-6", "worker-07", now.Add(-time.Minute), now.Add(-5*time.Second))
	staticPod.Annotations = map[string]string{mirrorPodAnnotationKey: "hash"}
	pods = append(pods, staticPod)
	missingCondition := recentTestPod("team-a", "missing", "uid-7", "worker-07", now.Add(-time.Minute), now.Add(-5*time.Second))
	missingCondition.Status.Conditions = nil
	pods = append(pods, missingCondition)

	report := AnalyzeRecent(pods, RecentOptions{
		Now:   now,
		Since: time.Hour,
		Limit: 2,
		Node:  "worker-07",
	})

	if report.ExcludedStatic != 1 {
		t.Fatalf("ExcludedStatic = %d, want 1", report.ExcludedStatic)
	}
	if report.IgnoredMissingScheduled != 1 {
		t.Fatalf("IgnoredMissingScheduled = %d, want 1", report.IgnoredMissingScheduled)
	}
	if len(report.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(report.Items))
	}
	if report.Items[0].Name != "later" || report.Items[1].Name != "alpha" {
		t.Fatalf("unexpected order: %s, %s", report.Items[0].Name, report.Items[1].Name)
	}
	if report.Items[0].TimeToScheduled != 90*time.Second {
		t.Fatalf("TimeToScheduled = %s, want 1m30s", report.Items[0].TimeToScheduled)
	}
}

func TestAnalyzeRecentSupportsPhaseSchedulerAndStaticFilters(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	matching := recentTestPod("default", "matching", "uid-1", "worker-07", now.Add(-time.Minute), now.Add(-30*time.Second))
	matching.Spec.SchedulerName = "custom-scheduler"
	matching.Status.Phase = corev1.PodPending
	matching.Annotations = map[string]string{mirrorPodAnnotationKey: "hash"}
	nonMatching := recentTestPod("default", "other", "uid-2", "worker-07", now.Add(-time.Minute), now.Add(-20*time.Second))

	report := AnalyzeRecent([]corev1.Pod{matching, nonMatching}, RecentOptions{
		Now:           now,
		Since:         time.Hour,
		Limit:         50,
		Phase:         corev1.PodPending,
		Scheduler:     "custom-scheduler",
		IncludeStatic: true,
	})

	if len(report.Items) != 1 || report.Items[0].Name != "matching" {
		t.Fatalf("unexpected items: %#v", report.Items)
	}
	if !report.Items[0].Static {
		t.Fatal("expected included Pod to be marked static")
	}
}

func TestAnalyzeRecentIgnoresFutureScheduleTimes(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	item := recentTestPod("default", "future", "uid-1", "worker-07", now, now.Add(time.Minute))
	report := AnalyzeRecent([]corev1.Pod{item}, RecentOptions{Now: now, Since: time.Hour, Limit: 50})
	if len(report.Items) != 0 {
		t.Fatalf("len(Items) = %d, want 0", len(report.Items))
	}
}

func recentTestPod(namespace, name, uid, node string, createdAt, scheduledAt time.Time) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			UID:               types.UID(uid),
			CreationTimestamp: metav1.NewTime(createdAt),
		},
		Spec: corev1.PodSpec{
			NodeName:      node,
			SchedulerName: corev1.DefaultSchedulerName,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(scheduledAt),
			}},
		},
	}
}
