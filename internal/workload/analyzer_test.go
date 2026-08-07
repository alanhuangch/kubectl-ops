package workload

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAnalyzeResourcesSupportsOwnershipChainsAndGPUFiltering(t *testing.T) {
	controller := true
	gpuRequests := corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi"),
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
	}
	cpuRequests := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}
	workloads := []Workload{
		resourceTestWorkload(KindDeployment, "trainer", "deployment-uid", 10, gpuRequests, true),
		resourceTestWorkload(KindStatefulSet, "database", "statefulset-uid", 3, cpuRequests, true),
		resourceTestWorkload(KindCronJob, "nightly", "cronjob-uid", 1, gpuRequests, true),
	}
	var pods []corev1.Pod
	for index := 0; index < 8; index++ {
		pods = append(pods, resourceTestPod(fmt.Sprintf("trainer-%d", index), "worker-gpu", corev1.PodRunning, "rs-uid", controller, gpuRequests))
	}
	pods = append(pods,
		resourceTestPod("trainer-pending", "", corev1.PodPending, "rs-uid", controller, gpuRequests),
		resourceTestPod("database-0", "worker-cpu", corev1.PodRunning, "statefulset-uid", controller, cpuRequests),
		resourceTestPod("nightly-run", "worker-gpu", corev1.PodRunning, "job-uid", controller, gpuRequests),
	)
	snapshot := Snapshot{
		Workloads: workloads,
		OwnerLinks: []OwnerLink{
			{UID: "rs-uid", OwnerUID: "deployment-uid"},
			{UID: "job-uid", OwnerUID: "cronjob-uid"},
		},
		Pods: pods, PodsKnown: true, InventoryComplete: true,
	}

	report := AnalyzeResources(snapshot, Options{ResourceClass: ResourceClassGPU})
	if report.Completeness != CompletenessComplete || len(report.Items) != 2 {
		t.Fatalf("unexpected GPU report: %#v", report)
	}
	if report.Items[0].Kind != KindCronJob || report.Items[1].Kind != KindDeployment {
		t.Fatalf("GPU workload ordering = %s, %s", report.Items[0].Kind, report.Items[1].Kind)
	}
	deployment := report.Items[1]
	if deployment.Pods.Planned != 10 || deployment.Pods.Actual != 8 {
		t.Fatalf("Deployment Pods = %#v", deployment.Pods)
	}
	assertQuantity(t, "planned Deployment GPU", deployment.GPUs[0].Planned, "20")
	assertQuantity(t, "actual Deployment GPU", deployment.GPUs[0].Actual, "16")
	if len(deployment.Placement.Required) != 1 || deployment.Placement.Required[0].MatchExpressions[0].Values[0] != "a" {
		t.Fatalf("node affinity was not retained and normalized: %#v", deployment.Placement)
	}
	cronJob := report.Items[0]
	assertQuantity(t, "planned CronJob GPU", cronJob.GPUs[0].Planned, "2")
	assertQuantity(t, "actual CronJob GPU", cronJob.GPUs[0].Actual, "2")

	cpuReport := AnalyzeResources(snapshot, Options{ResourceClass: ResourceClassCPU})
	if len(cpuReport.Items) != 3 {
		t.Fatalf("CPU filter returned %d workloads, want 3", len(cpuReport.Items))
	}
}

func TestAnalyzeResourcesMarksPerWorkloadActualUnknown(t *testing.T) {
	requests := corev1.ResourceList{corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1")}
	report := AnalyzeResources(Snapshot{
		Workloads: []Workload{resourceTestWorkload(KindDeployment, "trainer", "deployment-uid", 2, requests, false)},
		PodsKnown: true, InventoryComplete: false, Warnings: []string{"ReplicaSets unavailable."},
	}, Options{ResourceClass: ResourceClassGPU})
	if report.Completeness != CompletenessPartial || len(report.Items) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	item := report.Items[0]
	if item.Pods.ActualKnown || item.GPUs[0].ActualKnown {
		t.Fatalf("actual values must be unknown: %#v", item)
	}
	assertQuantity(t, "planned GPU", item.GPUs[0].Planned, "2")
}

func resourceTestWorkload(kind Kind, name, uid string, planned int32, requests corev1.ResourceList, actualKnown bool) Workload {
	return Workload{
		Namespace: "production", Kind: kind, Name: name, UID: uid, PlannedPods: planned, ActualKnown: actualKnown,
		Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			NodeSelector: map[string]string{"pool": "gpu"},
			Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"b", "a"}}},
				}}},
			}},
			Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: requests.DeepCopy()}}},
		}},
	}
}

func resourceTestPod(name, node string, phase corev1.PodPhase, ownerUID string, controller bool, requests corev1.ResourceList) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: name, UID: types.UID(name), OwnerReferences: []metav1.OwnerReference{{Kind: "Controller", UID: types.UID(ownerUID), Controller: &controller}}},
		Spec:       corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: requests.DeepCopy()}}}},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func assertQuantity(t *testing.T, name string, actual resource.Quantity, expected string) {
	t.Helper()
	want := resource.MustParse(expected)
	if actual.Cmp(want) != 0 {
		t.Fatalf("%s = %s, want %s", name, actual.String(), want.String())
	}
}
