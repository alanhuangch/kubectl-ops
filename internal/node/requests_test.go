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
		ShowPods:    true,
	})
	if report.Completeness != CompletenessPartial || len(report.Warnings) != 1 {
		t.Fatalf("unexpected partial report: %#v", report)
	}
	if len(report.Consumers) != 0 {
		t.Fatalf("unexpected consumers: %#v", report.Consumers)
	}
	if !report.ShowPods || report.PodResourcesKnown || len(report.PodResources) != 0 {
		t.Fatalf("unexpected Pod resource availability: %#v", report.PodResources)
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

func TestAnalyzeRequestsNamespaceScopeDoesNotClaimNodeAvailable(t *testing.T) {
	pod := requestTestPod("production", "api", "worker-07", "1")
	other := requestTestPod("kube-system", "agent", "worker-07", "2")
	report := AnalyzeRequests(requestTestNode("worker-07"), []corev1.Pod{other, pod}, RequestsOptions{
		Now:         time.Now(),
		Namespace:   "production",
		Top:         0,
		TopResource: corev1.ResourceCPU,
		PodsKnown:   true,
	})
	if report.Namespace != "production" {
		t.Fatalf("Namespace = %q", report.Namespace)
	}
	for _, usage := range report.Resources {
		if usage.Resource != corev1.ResourceCPU {
			continue
		}
		if !usage.RequestsKnown || usage.Requested.String() != "1" || usage.AvailableKnown {
			t.Fatalf("unexpected namespace-scoped CPU usage: %#v", usage)
		}
		if !usage.RatioKnown || usage.Ratio != 25 {
			t.Fatalf("unexpected namespace-scoped CPU ratio: %#v", usage)
		}
		return
	}
	t.Fatal("CPU resource not found")
}

func TestAnalyzeRequestsFiltersSingleResourceAcrossNamespaces(t *testing.T) {
	gpu := corev1.ResourceName("nvidia.com/gpu")
	production := requestTestPod("production", "inference", "worker-07", "500m")
	production.Spec.Containers[0].Resources.Requests[gpu] = resource.MustParse("1")
	production.Spec.Containers[0].Resources.Limits = corev1.ResourceList{gpu: resource.MustParse("1")}
	training := requestTestPod("training", "model", "worker-07", "1")
	training.Spec.Containers[0].Resources.Requests[gpu] = resource.MustParse("1")
	training.Spec.Containers[0].Resources.Limits = corev1.ResourceList{gpu: resource.MustParse("1")}
	system := requestTestPod("kube-system", "cpu-only", "worker-07", "250m")

	report := AnalyzeRequests(requestTestNode("worker-07"), []corev1.Pod{training, system, production}, RequestsOptions{
		Now:            time.Now(),
		Top:            10,
		TopResource:    gpu,
		PodsKnown:      true,
		ShowPods:       true,
		ResourceFilter: gpu,
	})

	if len(report.Resources) != 1 || report.Resources[0].Resource != gpu {
		t.Fatalf("unexpected filtered resources: %#v", report.Resources)
	}
	assertResourceUsage(t, report, gpu, "2", "2", "0", 100)
	if len(report.Consumers) != 2 || report.Consumers[0].Namespace != "production" || report.Consumers[1].Namespace != "training" {
		t.Fatalf("unexpected filtered consumers: %#v", report.Consumers)
	}
	if len(report.PodResources) != 2 {
		t.Fatalf("unexpected filtered Pod resources: %#v", report.PodResources)
	}
	for _, pod := range report.PodResources {
		if len(pod.Resources) != 1 || pod.Resources[0].Resource != gpu {
			t.Fatalf("unexpected Pod resource breakdown: %#v", pod)
		}
	}
}

func TestAnalyzeRequestsShowsExtendedResourceConsumers(t *testing.T) {
	controller := true
	gpuLarge := requestTestPod("training", "model-large", "worker-07", "100m")
	gpuLarge.OwnerReferences = []metav1.OwnerReference{{Kind: "Job", Name: "model-large", Controller: &controller}}
	gpuLarge.Spec.Containers[0].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("1")
	gpuLarge.Spec.InitContainers = []corev1.Container{{
		Name: "prepare-model",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
		}},
	}}

	gpuSmall := requestTestPod("inference", "model-small", "worker-07", "100m")
	gpuSmall.Spec.Containers[0].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("1")
	gpuSmall.Spec.Containers[0].Resources.Requests[corev1.ResourceName("example.com/fpga")] = resource.MustParse("1")
	gpuSmall.Spec.Containers[0].Resources.Requests[corev1.ResourceName("kubernetes.io/native-device")] = resource.MustParse("1")

	terminal := requestTestPod("jobs", "completed", "worker-07", "100m")
	terminal.Spec.Containers[0].Resources.Requests[corev1.ResourceName("example.com/fpga")] = resource.MustParse("4")
	terminal.Status.Phase = corev1.PodSucceeded

	report := AnalyzeRequests(requestTestNode("worker-07"), []corev1.Pod{gpuSmall, terminal, gpuLarge}, RequestsOptions{
		Now:          time.Now(),
		Top:          0,
		TopResource:  corev1.ResourceCPU,
		PodsKnown:    true,
		ShowExtended: true,
	})

	if !report.ShowExtended || !report.ExtendedKnown || len(report.ExtendedConsumers) != 3 {
		t.Fatalf("unexpected extended consumers: %#v", report.ExtendedConsumers)
	}
	want := []struct {
		resource corev1.ResourceName
		pod      string
		request  string
	}{
		{resource: "example.com/fpga", pod: "model-small", request: "1"},
		{resource: "nvidia.com/gpu", pod: "model-large", request: "2"},
		{resource: "nvidia.com/gpu", pod: "model-small", request: "1"},
	}
	for i, expected := range want {
		actual := report.ExtendedConsumers[i]
		if actual.Resource != expected.resource || actual.Pod != expected.pod || actual.Request.String() != expected.request {
			t.Fatalf("ExtendedConsumers[%d] = %#v, want %#v", i, actual, expected)
		}
	}
	if report.ExtendedConsumers[1].Owner != "Job/model-large" {
		t.Fatalf("unexpected owner: %#v", report.ExtendedConsumers[1])
	}
	assertResourceUsage(t, report, corev1.ResourceName("example.com/fpga"), "0", "1", "-1", 0)
}

func TestIsExtendedResourceName(t *testing.T) {
	tests := []struct {
		name corev1.ResourceName
		want bool
	}{
		{name: "nvidia.com/gpu", want: true},
		{name: "example.com/fpga", want: true},
		{name: corev1.ResourceCPU, want: false},
		{name: "hugepages-2Mi", want: false},
		{name: "kubernetes.io/native-device", want: false},
		{name: "example.kubernetes.io/native-device", want: false},
		{name: "requests.example.com/device", want: false},
	}
	for _, test := range tests {
		t.Run(string(test.name), func(t *testing.T) {
			if got := isExtendedResourceName(test.name); got != test.want {
				t.Fatalf("isExtendedResourceName(%q) = %t, want %t", test.name, got, test.want)
			}
		})
	}
}

func TestAnalyzeRequestsShowsPodResourceBreakdown(t *testing.T) {
	controller := true
	application := requestTestPod("production", "api", "worker-07", "500m")
	application.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-7d8f", Controller: &controller}}
	application.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("1Gi")
	application.Spec.Containers[0].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("1")
	application.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
		corev1.ResourceCPU:                    resource.MustParse("1"),
		corev1.ResourceMemory:                 resource.MustParse("2Gi"),
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
	}
	application.Spec.InitContainers = []corev1.Container{{
		Name: "prepare",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
	}}
	empty := requestTestPod("system", "best-effort", "worker-07", "0")
	terminal := requestTestPod("jobs", "completed", "worker-07", "1")
	terminal.Status.Phase = corev1.PodSucceeded

	report := AnalyzeRequests(requestTestNode("worker-07"), []corev1.Pod{empty, terminal, application}, RequestsOptions{
		Now:         time.Now(),
		Top:         0,
		TopResource: corev1.ResourceCPU,
		PodsKnown:   true,
		ShowPods:    true,
	})

	if !report.ShowPods || !report.PodResourcesKnown || len(report.PodResources) != 2 {
		t.Fatalf("unexpected Pod resources: %#v", report.PodResources)
	}
	applicationResources := report.PodResources[0]
	if applicationResources.Namespace != "production" || applicationResources.Pod != "api" || applicationResources.Owner != "ReplicaSet/api-7d8f" {
		t.Fatalf("unexpected application breakdown: %#v", applicationResources)
	}
	if len(applicationResources.Resources) != 3 {
		t.Fatalf("unexpected application resources: %#v", applicationResources.Resources)
	}
	assertPodResource(t, applicationResources, corev1.ResourceCPU, "2", "3", 50)
	assertPodResource(t, applicationResources, corev1.ResourceMemory, "1Gi", "4Gi", 12.5)
	assertPodResource(t, applicationResources, corev1.ResourceName("nvidia.com/gpu"), "1", "1", 50)
	if report.PodResources[1].Pod != "best-effort" || len(report.PodResources[1].Resources) != 0 {
		t.Fatalf("unexpected best-effort breakdown: %#v", report.PodResources[1])
	}
}

func assertPodResource(t *testing.T, pod PodResourceBreakdown, name corev1.ResourceName, request, limit string, ratio float64) {
	t.Helper()
	for _, usage := range pod.Resources {
		if usage.Resource != name {
			continue
		}
		if !usage.RequestSet || usage.Request.String() != request || !usage.LimitSet || usage.Limit.String() != limit {
			t.Fatalf("Pod resource %s = %#v", name, usage)
		}
		if !usage.RatioKnown || usage.RequestRatio != ratio {
			t.Fatalf("Pod resource %s ratio = %v, want %v", name, usage.RequestRatio, ratio)
		}
		return
	}
	t.Fatalf("Pod resource %s not found", name)
}

func assertResourceUsage(t *testing.T, report RequestsReport, name corev1.ResourceName, allocatable, requested, available string, ratio float64) {
	t.Helper()
	for _, usage := range report.Resources {
		if usage.Resource != name {
			continue
		}
		if !usage.RequestsKnown || !usage.AvailableKnown || usage.Allocatable.String() != allocatable || usage.Requested.String() != requested || usage.Available.String() != available {
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
