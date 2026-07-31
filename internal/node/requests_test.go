package node

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAnalyzeRequestsCalculatesKubernetesPodRequests(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	item := requestTestNode("worker-07")
	always := corev1.ContainerRestartPolicyAlways
	controller := true
	application := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "production",
			Name:      "api",
			UID:       types.UID("api"),
			OwnerReferences: []metav1.OwnerReference{{
				Kind:       "ReplicaSet",
				Name:       "api-7d8f",
				Controller: &controller,
			}},
		},
		Spec: corev1.PodSpec{
			NodeName: "worker-07",
			Containers: []corev1.Container{{
				Name: "api",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:                    resource.MustParse("100m"),
					corev1.ResourceMemory:                 resource.MustParse("200Mi"),
					corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
				}},
			}},
			InitContainers: []corev1.Container{
				{
					Name:          "sidecar",
					RestartPolicy: &always,
					Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("100Mi"),
					}},
				},
				{
					Name: "setup",
					Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					}},
				},
			},
			Overhead: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	daemon := requestTestPod("kube-system", "agent", "worker-07", "250m")
	daemon.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent", Controller: &controller}}
	completed := requestTestPod("jobs", "done", "worker-07", "2")
	completed.Status.Phase = corev1.PodSucceeded

	report := AnalyzeRequests(item, []corev1.Pod{application, daemon, completed}, RequestsOptions{
		Now:         now,
		Top:         10,
		TopResource: corev1.ResourceCPU,
		PodsKnown:   true,
	})

	if report.Completeness != CompletenessComplete {
		t.Fatalf("Completeness = %q", report.Completeness)
	}
	assertResourceUsage(t, report, corev1.ResourceCPU, "4", "1", "3", 25)
	assertResourceUsage(t, report, corev1.ResourceMemory, "8Gi", "1174Mi", "7018Mi", 0)
	assertResourceUsage(t, report, corev1.ResourcePods, "10", "2", "8", 20)
	assertResourceUsage(t, report, corev1.ResourceName("nvidia.com/gpu"), "2", "1", "1", 50)
	if len(report.Consumers) != 2 {
		t.Fatalf("len(Consumers) = %d, want 2", len(report.Consumers))
	}
	if report.Consumers[0].Pod != "api" || report.Consumers[0].Request.String() != "750m" {
		t.Fatalf("unexpected first consumer: %#v", report.Consumers[0])
	}
	if report.Consumers[1].Pod != "agent" || !report.Consumers[1].DaemonSet {
		t.Fatalf("unexpected second consumer: %#v", report.Consumers[1])
	}
}

func TestAnalyzeRequestsReturnsPartialWithoutPods(t *testing.T) {
	report := AnalyzeRequests(requestTestNode("worker-07"), nil, RequestsOptions{
		Now:         time.Now(),
		Top:         10,
		TopResource: corev1.ResourceCPU,
		PodsKnown:   false,
	})
	if report.Completeness != CompletenessPartial || len(report.Warnings) != 1 {
		t.Fatalf("unexpected partial report: %#v", report)
	}
	if len(report.Consumers) != 0 {
		t.Fatalf("unexpected consumers: %#v", report.Consumers)
	}
	for _, usage := range report.Resources {
		if usage.RequestsKnown || usage.RatioKnown {
			t.Fatalf("resource should be unknown: %#v", usage)
		}
	}
}

func TestAnalyzeRequestsUsesDeterministicTopLimit(t *testing.T) {
	pods := []corev1.Pod{
		requestTestPod("b", "same", "worker-07", "100m"),
		requestTestPod("a", "z", "worker-07", "100m"),
		requestTestPod("a", "a", "worker-07", "100m"),
	}
	report := AnalyzeRequests(requestTestNode("worker-07"), pods, RequestsOptions{
		Now:         time.Now(),
		Top:         2,
		TopResource: corev1.ResourceCPU,
		PodsKnown:   true,
	})
	if len(report.Consumers) != 2 || report.Consumers[0].Pod != "a" || report.Consumers[1].Pod != "z" {
		t.Fatalf("unexpected consumers: %#v", report.Consumers)
	}
}

func assertResourceUsage(t *testing.T, report RequestsReport, name corev1.ResourceName, allocatable, requested, available string, ratio float64) {
	t.Helper()
	for _, usage := range report.Resources {
		if usage.Resource != name {
			continue
		}
		if usage.Allocatable.String() != allocatable || usage.Requested.String() != requested || usage.Available.String() != available {
			t.Fatalf("resource %s = %s/%s/%s", name, usage.Allocatable.String(), usage.Requested.String(), usage.Available.String())
		}
		if ratio > 0 && usage.Ratio != ratio {
			t.Fatalf("resource %s ratio = %v, want %v", name, usage.Ratio, ratio)
		}
		return
	}
	t.Fatalf("resource %s not found", name)
}

func requestTestNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:                    resource.MustParse("4"),
			corev1.ResourceMemory:                 resource.MustParse("8Gi"),
			corev1.ResourceEphemeralStorage:       resource.MustParse("100Gi"),
			corev1.ResourcePods:                   resource.MustParse("10"),
			corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
		}},
	}
}

func requestTestPod(namespace, name, node, cpu string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID(namespace + "/" + name)},
		Spec: corev1.PodSpec{
			NodeName: node,
			Containers: []corev1.Container{{
				Name: "main",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse(cpu),
				}},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}
