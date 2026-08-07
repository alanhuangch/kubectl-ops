package command

import (
	"testing"

	workloadanalysis "github.com/alanhuangch/kubectl-ops/internal/workload"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestDesiredJobPodsUsesRemainingParallelism(t *testing.T) {
	parallelism := int32(4)
	completions := int32(10)
	item := &batchv1.Job{
		Spec:   batchv1.JobSpec{Parallelism: &parallelism, Completions: &completions},
		Status: batchv1.JobStatus{Succeeded: 8},
	}
	if actual := desiredJobPods(item); actual != 2 {
		t.Fatalf("desired Job Pods = %d, want 2", actual)
	}
	suspended := true
	item.Spec.Suspend = &suspended
	if actual := desiredJobPods(item); actual != 0 {
		t.Fatalf("suspended Job desired Pods = %d, want 0", actual)
	}
}

func TestCronJobWorkloadPlanIsPerRunAndHonorsSuspend(t *testing.T) {
	parallelism := int32(3)
	suspended := true
	item := &batchv1.CronJob{Spec: batchv1.CronJobSpec{
		Suspend: &suspended,
		JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{
			Parallelism: &parallelism,
			Template:    corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "job"}}}},
		}},
	}}
	if actual := cronJobWorkload(item, true).PlannedPods; actual != 0 {
		t.Fatalf("suspended CronJob planned Pods = %d, want 0", actual)
	}
	*item.Spec.Suspend = false
	if actual := cronJobWorkload(item, true).PlannedPods; actual != 3 {
		t.Fatalf("CronJob planned Pods per run = %d, want 3", actual)
	}
}

func TestParseWorkloadKindAliases(t *testing.T) {
	tests := map[string]workloadanalysis.Kind{
		"deploy": workloadanalysis.KindDeployment,
		"sts":    workloadanalysis.KindStatefulSet,
		"ds":     workloadanalysis.KindDaemonSet,
		"cj":     workloadanalysis.KindCronJob,
		"rs":     workloadanalysis.KindReplicaSet,
		"po":     workloadanalysis.KindPod,
	}
	for value, expected := range tests {
		actual, err := parseWorkloadKind(value)
		if err != nil || actual != expected {
			t.Fatalf("parseWorkloadKind(%q) = %q, %v; want %q", value, actual, err, expected)
		}
	}
	if _, err := parseWorkloadKind("service"); err == nil {
		t.Fatal("expected unsupported workload kind error")
	}
}
