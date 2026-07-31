package pod

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAnalyzeRestartsAggregatesContainerKinds(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	item := restartTestPod("production", "payment", "worker-07")
	item.Status.ContainerStatuses = []corev1.ContainerStatus{
		restartTestStatus("payment", 12, "OOMKilled", 137, 0, now.Add(-time.Minute)),
		restartTestStatus("never-restarted", 0, "", 0, 0, time.Time{}),
	}
	item.Status.InitContainerStatuses = []corev1.ContainerStatus{
		restartTestStatus("migrate", 2, "Error", 1, 0, now.Add(-2*time.Minute)),
	}
	item.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{
		restartTestStatus("debugger", 1, "Error", 143, 0, now.Add(-3*time.Minute)),
	}
	missingTime := restartTestStatus("missing-time", 1, "Error", 1, 0, time.Time{})
	missingTime.LastTerminationState.Terminated.FinishedAt = metav1.Time{}
	item.Status.ContainerStatuses = append(item.Status.ContainerStatuses, missingTime)

	old := restartTestPod("production", "old", "worker-07")
	old.Status.ContainerStatuses = []corev1.ContainerStatus{
		restartTestStatus("old", 4, "Error", 1, 0, now.Add(-2*time.Hour)),
	}
	otherNode := restartTestPod("production", "other-node", "worker-08")
	otherNode.Status.ContainerStatuses = []corev1.ContainerStatus{
		restartTestStatus("other", 1, "Error", 1, 0, now.Add(-30*time.Second)),
	}

	report := AnalyzeRestarts([]corev1.Pod{item, old, otherNode}, RestartOptions{
		Now:   now,
		Since: time.Hour,
		Node:  "worker-07",
	})

	if report.IgnoredMissingFinishedAt != 1 {
		t.Fatalf("IgnoredMissingFinishedAt = %d, want 1", report.IgnoredMissingFinishedAt)
	}
	if len(report.Items) != 3 {
		t.Fatalf("len(Items) = %d, want 3", len(report.Items))
	}
	wantNames := []string{"payment", "migrate", "debugger"}
	wantKinds := []ContainerKind{ContainerKindRegular, ContainerKindInit, ContainerKindEphemeral}
	wantClasses := []RestartClassification{RestartOOMKilled, RestartError, RestartSIGTERM}
	for i := range report.Items {
		if report.Items[i].Container != wantNames[i] || report.Items[i].Kind != wantKinds[i] || report.Items[i].Classification != wantClasses[i] {
			t.Fatalf("item %d = %#v", i, report.Items[i])
		}
	}
}

func TestClassifyRestart(t *testing.T) {
	tests := []struct {
		name       string
		terminated corev1.ContainerStateTerminated
		want       RestartClassification
	}{
		{name: "oom", terminated: corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}, want: RestartOOMKilled},
		{name: "exit 137", terminated: corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 137}, want: RestartSIGKILL},
		{name: "signal 9", terminated: corev1.ContainerStateTerminated{Signal: 9, ExitCode: 1}, want: RestartSIGKILL},
		{name: "exit 143", terminated: corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 143}, want: RestartSIGTERM},
		{name: "signal 15", terminated: corev1.ContainerStateTerminated{Signal: 15, ExitCode: 1}, want: RestartSIGTERM},
		{name: "completed", terminated: corev1.ContainerStateTerminated{Reason: "Completed", ExitCode: 0}, want: RestartCompleted},
		{name: "error reason with zero exit", terminated: corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 0}, want: RestartError},
		{name: "non-zero", terminated: corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 2}, want: RestartError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyRestart(&test.terminated); got != test.want {
				t.Fatalf("classifyRestart() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAnalyzeRestartsUsesClassificationAsMissingReason(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	item := restartTestPod("default", "api", "worker-07")
	item.Status.ContainerStatuses = []corev1.ContainerStatus{
		restartTestStatus("api", 1, "", 137, 0, now.Add(-time.Minute)),
	}
	report := AnalyzeRestarts([]corev1.Pod{item}, RestartOptions{Now: now, Since: time.Hour})
	if len(report.Items) != 1 || report.Items[0].LastReason != "SIGKILL" {
		t.Fatalf("unexpected items: %#v", report.Items)
	}
}

func restartTestPod(namespace, name, node string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID(name)},
		Spec:       corev1.PodSpec{NodeName: node},
	}
}

func restartTestStatus(name string, count int32, reason string, exitCode, signal int32, finishedAt time.Time) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name:         name,
		RestartCount: count,
		LastTerminationState: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				Reason:     reason,
				ExitCode:   exitCode,
				Signal:     signal,
				FinishedAt: metav1.NewTime(finishedAt),
			},
		},
	}
}
