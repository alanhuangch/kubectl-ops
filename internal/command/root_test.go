package command

import (
	"bytes"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestVersionCommandDoesNotRequireACluster(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) {
			t.Fatal("version command must not create a Kubernetes client")
			return nil, nil
		},
	})
	cmd.SetArgs([]string{"version", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"version": "dev"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestHelpUsesKubectlPluginDisplayName(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{})
	cmd.SetArgs([]string{"pod", "recent", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Usage:\n  kubectl ops pod recent") {
		t.Fatalf("help does not use kubectl plugin invocation:\n%s", out.String())
	}
}

func TestPodRecentUsesSelectorsAndStableJSON(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	client := fake.NewClientset(
		commandTestPod("production", "api", "worker-07", map[string]string{"app": "api"}, now),
		commandTestPod("production", "other-node", "worker-08", map[string]string{"app": "api"}, now),
		commandTestPod("production", "other-label", "worker-07", map[string]string{"app": "worker"}, now),
	)
	var namespace, fieldSelector, labelSelector string
	client.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		listAction := action.(clienttesting.ListAction)
		namespace = action.GetNamespace()
		fieldSelector = listAction.GetListRestrictions().Fields.String()
		labelSelector = listAction.GetListRestrictions().Labels.String()
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"pod", "recent", "-A", "--node", "worker-07", "-l", "app=api", "--since", "1h", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if namespace != metav1.NamespaceAll {
		t.Fatalf("namespace = %q, want all namespaces", namespace)
	}
	if fieldSelector != "spec.nodeName=worker-07" {
		t.Fatalf("field selector = %q", fieldSelector)
	}
	if labelSelector != "app=api" {
		t.Fatalf("label selector = %q", labelSelector)
	}
	if strings.Count(out.String(), `"pod":`) != 1 || !strings.Contains(out.String(), `"pod": "api"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestPodRecentValidatesArgumentsBeforeCreatingClient(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) {
			t.Fatal("invalid command must not create a Kubernetes client")
			return nil, nil
		},
	})
	cmd.SetArgs([]string{"pod", "recent", "-A", "--since", "0s"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--since must be greater than zero") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPodRestartsUsesSelectorsAndStableJSON(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	matching := commandTestPod("production", "payment", "worker-07", map[string]string{"app": "payment"}, now)
	matching.Status.ContainerStatuses = []corev1.ContainerStatus{commandRestartStatus("payment", now.Add(-time.Minute))}
	otherNode := commandTestPod("production", "other-node", "worker-08", map[string]string{"app": "payment"}, now)
	otherNode.Status.ContainerStatuses = []corev1.ContainerStatus{commandRestartStatus("payment", now.Add(-time.Minute))}
	otherLabel := commandTestPod("production", "other-label", "worker-07", map[string]string{"app": "worker"}, now)
	otherLabel.Status.ContainerStatuses = []corev1.ContainerStatus{commandRestartStatus("worker", now.Add(-time.Minute))}
	client := fake.NewClientset(matching, otherNode, otherLabel)

	var fieldSelector, labelSelector string
	client.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		listAction := action.(clienttesting.ListAction)
		fieldSelector = listAction.GetListRestrictions().Fields.String()
		labelSelector = listAction.GetListRestrictions().Labels.String()
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"pod", "restarts", "-A", "--node", "worker-07", "-l", "app=payment", "--since", "1h", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if fieldSelector != "spec.nodeName=worker-07" || labelSelector != "app=payment" {
		t.Fatalf("selectors = %q, %q", fieldSelector, labelSelector)
	}
	if strings.Count(out.String(), `"container":`) != 1 || !strings.Contains(out.String(), `"classification": "OOMKilled"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestNodeRequestsUsesNodeFieldSelector(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	node := commandTestNode("worker-07")
	pod := commandTestPod("production", "api", "worker-07", nil, now)
	pod.Spec.Containers = []corev1.Container{{
		Name: "api",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
		}},
	}}
	client := fake.NewClientset(node, pod)
	var namespace, fieldSelector string
	client.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		namespace = action.GetNamespace()
		fieldSelector = action.(clienttesting.ListAction).GetListRestrictions().Fields.String()
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "--top", "1", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if fieldSelector != "spec.nodeName=worker-07" {
		t.Fatalf("field selector = %q", fieldSelector)
	}
	if namespace != metav1.NamespaceAll {
		t.Fatalf("namespace = %q, want all namespaces", namespace)
	}
	if !strings.Contains(out.String(), `"node": "worker-07"`) || !strings.Contains(out.String(), `"request": "500m"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if strings.Contains(out.String(), `"extendedResourceConsumers"`) {
		t.Fatalf("extended resource consumers must be omitted unless --extended is set: %s", out.String())
	}
	if strings.Contains(out.String(), `"podResources"`) {
		t.Fatalf("Pod resources must be omitted unless --pods is set: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestNodeRequestsFiltersNamespace(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	production := commandTestPod("production", "api", "worker-07", nil, now)
	production.Spec.Containers = []corev1.Container{{
		Name: "api",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
		}},
	}}
	system := commandTestPod("kube-system", "agent", "worker-07", nil, now)
	system.Spec.Containers = []corev1.Container{{
		Name: "agent",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("250m"),
		}},
	}}
	client := fake.NewClientset(commandTestNode("worker-07"), production, system)
	var namespace, fieldSelector string
	client.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		namespace = action.GetNamespace()
		fieldSelector = action.(clienttesting.ListAction).GetListRestrictions().Fields.String()
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "--pods", "--top", "0", "-n", "production", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if namespace != "production" || fieldSelector != "spec.nodeName=worker-07" {
		t.Fatalf("scope = namespace %q, field selector %q", namespace, fieldSelector)
	}
	for _, expected := range []string{`"namespace": "production"`, `"pod": "api"`, `"requested": "500m"`, `"available": null`} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output does not contain %q: %s", expected, out.String())
		}
	}
	if strings.Contains(out.String(), `"pod": "agent"`) || strings.Contains(out.String(), `"requested": "750m"`) {
		t.Fatalf("output includes Pods outside the selected namespace: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestNodeRequestsRejectsNamespaceWithAllNamespaces(t *testing.T) {
	cmd := newRootCommand(genericclioptions.IOStreams{}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) {
			t.Fatal("invalid namespace flags must not create a Kubernetes client")
			return nil, nil
		},
	})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "-n", "production", "-A"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--namespace and --all-namespaces cannot be used together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNodeRequestsOnlyResourceAcrossNamespaces(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	gpu := corev1.ResourceName("nvidia.com/gpu")
	node := commandTestNode("worker-07")
	node.Status.Allocatable[gpu] = resource.MustParse("4")
	production := commandTestPod("production", "inference", "worker-07", nil, now)
	production.Spec.Containers = []corev1.Container{{
		Name: "inference",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{gpu: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")},
			Limits:   corev1.ResourceList{gpu: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
		},
	}}
	training := commandTestPod("training", "model", "worker-07", nil, now)
	training.Spec.Containers = []corev1.Container{{
		Name: "model",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{gpu: resource.MustParse("2")},
			Limits:   corev1.ResourceList{gpu: resource.MustParse("2")},
		},
	}}
	cpuOnly := commandTestPod("kube-system", "cpu-only", "worker-07", nil, now)
	cpuOnly.Spec.Containers = []corev1.Container{{
		Name: "agent",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("250m"),
		}},
	}}
	client := fake.NewClientset(node, production, training, cpuOnly)

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "-A", "--resource", string(gpu), "--only-resource", "--pods", "--top", "0", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), `"resource": "nvidia.com/gpu"`) != 3 {
		t.Fatalf("unexpected filtered resource count: %s", out.String())
	}
	for _, expected := range []string{`"pod": "inference"`, `"pod": "model"`, `"requested": "3"`} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output does not contain %q: %s", expected, out.String())
		}
	}
	for _, unexpected := range []string{`"resource": "memory"`, `"resource": "cpu"`, `"pod": "cpu-only"`} {
		if strings.Contains(out.String(), unexpected) {
			t.Fatalf("output contains filtered value %q: %s", unexpected, out.String())
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestNodeRequestsOnlyResourceRequiresResourceFlag(t *testing.T) {
	cmd := newRootCommand(genericclioptions.IOStreams{}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) {
			t.Fatal("invalid resource filter must not create a Kubernetes client")
			return nil, nil
		},
	})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "--only-resource"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--only-resource requires an explicit --resource") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNodeRequestsShowsExtendedResourceConsumers(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	node := commandTestNode("worker-07")
	node.Status.Allocatable[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("4")
	pod := commandTestPod("training", "model", "worker-07", nil, now)
	pod.Spec.Containers = []corev1.Container{{
		Name: "model",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
		}},
	}}
	client := fake.NewClientset(node, pod)

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "--extended", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"extendedResourceConsumers": [`,
		`"resource": "nvidia.com/gpu"`,
		`"pod": "model"`,
		`"request": "2"`,
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output does not contain %q: %s", expected, out.String())
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestNodeRequestsShowsPodResources(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	node := commandTestNode("worker-07")
	node.Status.Allocatable[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("4")
	pod := commandTestPod("training", "model", "worker-07", nil, now)
	pod.Spec.Containers = []corev1.Container{{
		Name: "model",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory:                 resource.MustParse("1Gi"),
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory:                 resource.MustParse("2Gi"),
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
			},
		},
	}}
	client := fake.NewClientset(node, pod)

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "--pods", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"podResources": [`,
		`"resource": "memory"`,
		`"limit": "2Gi"`,
		`"resource": "nvidia.com/gpu"`,
		`"request": "2"`,
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("output does not contain %q: %s", expected, out.String())
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestNodeRequestsRejectsExtendedWithPods(t *testing.T) {
	cmd := newRootCommand(genericclioptions.IOStreams{}, dependencies{})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "--extended", "--pods"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--pods already includes extended resources") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNodeRequestsDegradesWhenPodListIsForbidden(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	client := fake.NewClientset(commandTestNode("worker-07"))
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", nil)
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"node", "requests", "worker-07", "--extended", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"completeness": "Partial"`) ||
		!strings.Contains(out.String(), `"requested": null`) ||
		!strings.Contains(out.String(), `"extendedResourceConsumers": null`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("JSON warnings must stay in the JSON document: %s", errOut.String())
	}
}

func TestNodeDrainCheckUsesNodeSelectorAndPDBs(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	controller := true
	pod := commandTestPod("production", "api", "worker-07", map[string]string{"app": "api"}, now)
	pod.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", Controller: &controller}}
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "api"},
		Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}},
		Status:     policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
	}
	client := fake.NewClientset(commandTestNode("worker-07"), pod, pdb)
	var podSelector, pdbNamespace string
	client.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		podSelector = action.(clienttesting.ListAction).GetListRestrictions().Fields.String()
		return false, nil, nil
	})
	client.PrependReactor("list", "poddisruptionbudgets", func(action clienttesting.Action) (bool, runtime.Object, error) {
		pdbNamespace = action.GetNamespace()
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"node", "drain-check", "worker-07", "--ignore-daemonsets", "--force", "--delete-emptydir-data", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if podSelector != "spec.nodeName=worker-07" {
		t.Fatalf("Pod selector = %q", podSelector)
	}
	if pdbNamespace != metav1.NamespaceAll {
		t.Fatalf("PDB namespace = %q", pdbNamespace)
	}
	if !strings.Contains(out.String(), `"drainability": "Blocked"`) || !strings.Contains(out.String(), `"PDBViolation"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestNodeDrainCheckDegradesWhenPDBListIsForbidden(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	controller := true
	pod := commandTestPod("production", "api", "worker-07", map[string]string{"app": "api"}, now)
	pod.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", Controller: &controller}}
	client := fake.NewClientset(commandTestNode("worker-07"), pod)
	client.PrependReactor("list", "poddisruptionbudgets", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: policyv1.GroupName, Resource: "poddisruptionbudgets"}, "", nil)
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"node", "drain-check", "worker-07", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"drainability": "Unknown"`) || !strings.Contains(out.String(), `"completeness": "Partial"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("JSON warnings must stay in the JSON document: %s", errOut.String())
	}
}

func TestPodPendingCollectsObservedAndCurrentStateData(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	target := commandTestPod("production", "target", "", nil, now)
	target.Status.Phase = corev1.PodPending
	target.Status.Conditions = []corev1.PodCondition{{
		Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", LastTransitionTime: metav1.NewTime(now.Add(-time.Minute)),
	}}
	target.Spec.Containers = []corev1.Container{{
		Name: "target",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("2"),
		}},
	}}
	node := commandTestNode("worker-07")
	busy := commandTestPod("production", "busy", "worker-07", nil, now)
	busy.Spec.Containers = []corev1.Container{{
		Name: "busy",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("3"),
		}},
	}}
	event := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "production", Name: "failed", UID: types.UID("event")},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "production", Name: "target", UID: target.UID},
		Reason:         "FailedScheduling", Type: corev1.EventTypeWarning, Message: "insufficient cpu", Count: 2, LastTimestamp: metav1.NewTime(now.Add(-30 * time.Second)),
	}
	client := fake.NewClientset(target, node, busy, event)
	var eventSelector string
	client.PrependReactor("list", "events", func(action clienttesting.Action) (bool, runtime.Object, error) {
		eventSelector = action.(clienttesting.ListAction).GetListRestrictions().Fields.String()
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"pod", "pending", "target", "-n", "production", "--all-nodes", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if eventSelector != "involvedObject.uid=target" {
		t.Fatalf("event selector = %q", eventSelector)
	}
	if !strings.Contains(out.String(), `"reason": "FailedScheduling"`) || !strings.Contains(out.String(), `"code": "InsufficientCPU"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestEventsTimelineUsesEventsV1UIDSelector(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	event := &eventsv1.Event{
		ObjectMeta:          metav1.ObjectMeta{Namespace: "production", Name: "event", UID: types.UID("event")},
		EventTime:           metav1.NewMicroTime(now.Add(-time.Minute)),
		ReportingController: "kubelet",
		Action:              "BackOff",
		Reason:              "BackOff",
		Regarding:           corev1.ObjectReference{Kind: "Pod", Namespace: "production", Name: "api", UID: types.UID("pod-uid")},
		Note:                "back-off restarting failed container",
		Type:                corev1.EventTypeWarning,
	}
	client := fake.NewClientset(event)
	var selector string
	client.PrependReactor("list", "events", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetResource().Group == eventsv1.GroupName {
			selector = action.(clienttesting.ListAction).GetListRestrictions().Fields.String()
		}
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"events", "timeline", "-A", "--uid", "pod-uid", "--since", "1h", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if selector != "regarding.uid=pod-uid" {
		t.Fatalf("selector = %q", selector)
	}
	if !strings.Contains(out.String(), `"apiVersion": "events.k8s.io/v1"`) || !strings.Contains(out.String(), `"reason": "BackOff"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestEventsTimelineFallsBackToCoreV1(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	event := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default", Name: "event", UID: types.UID("event")},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "api", UID: types.UID("pod-uid")},
		Reason:         "FailedScheduling",
		Message:        "insufficient cpu",
		Type:           corev1.EventTypeWarning,
		LastTimestamp:  metav1.NewTime(now.Add(-time.Minute)),
	}
	client := fake.NewClientset(event)
	client.PrependReactor("list", "events", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetResource().Group == eventsv1.GroupName {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: eventsv1.GroupName, Resource: "events"}, "", nil)
		}
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"events", "timeline", "--since", "1h", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"apiVersion": "v1"`) || !strings.Contains(out.String(), `"reason": "FailedScheduling"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestRolloutExplainCorrelatesDeploymentResources(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	replicas := int32(1)
	controller := true
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "api", UID: types.UID("deployment-uid"), Generation: 2},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
		},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, UnavailableReplicas: 1},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "production", Name: "api-new", UID: types.UID("rs-uid"), Labels: map[string]string{"app": "api"},
			Annotations:     map[string]string{"deployment.kubernetes.io/revision": "2"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: deployment.UID, Controller: &controller}},
		},
		Spec:   appsv1.ReplicaSetSpec{Replicas: &replicas},
		Status: appsv1.ReplicaSetStatus{Replicas: 1},
	}
	pod := commandTestPod("production", "api-new-a", "worker-07", map[string]string{"app": "api"}, now)
	pod.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-new", UID: replicaSet.UID, Controller: &controller}}
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "api", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
	}}
	event := &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "pull-failed", UID: types.UID("event-uid")},
		EventTime:  metav1.NewMicroTime(now.Add(-time.Minute)), Type: corev1.EventTypeWarning, Reason: "Failed",
		Regarding: corev1.ObjectReference{Kind: "Pod", Namespace: "production", Name: pod.Name, UID: pod.UID},
		Note:      "Failed to pull image", ReportingController: "kubelet",
	}
	client := fake.NewClientset(deployment, replicaSet, pod, event)
	var replicaSetSelector, podSelector string
	client.PrependReactor("list", "replicasets", func(action clienttesting.Action) (bool, runtime.Object, error) {
		replicaSetSelector = action.(clienttesting.ListAction).GetListRestrictions().Labels.String()
		return false, nil, nil
	})
	client.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		podSelector = action.(clienttesting.ListAction).GetListRestrictions().Labels.String()
		return false, nil, nil
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"rollout", "explain", "deployment/api", "-n", "production", "--since", "1h", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if replicaSetSelector != "app=api" || podSelector != "app=api" {
		t.Fatalf("selectors = %q, %q", replicaSetSelector, podSelector)
	}
	if !strings.Contains(out.String(), `"status": "Degraded"`) ||
		!strings.Contains(out.String(), `"code": "ContainerWaiting"`) ||
		!strings.Contains(out.String(), `"reason": "Failed"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestRolloutExplainDegradesWhenReplicaSetsAreForbidden(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "api", UID: types.UID("deployment-uid"), Generation: 1},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
		},
	}
	client := fake.NewClientset(deployment)
	client.PrependReactor("list", "replicasets", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: appsv1.GroupName, Resource: "replicasets"}, "", nil)
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"rollout", "explain", "api", "-n", "production", "--event-limit", "0", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"status": "Unknown"`) || !strings.Contains(out.String(), `"completeness": "Partial"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("JSON warnings must stay in the JSON document: %s", errOut.String())
	}
}

func TestRolloutExplainDegradesWhenEventsAreForbidden(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	replicas := int32(0)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "production", Name: "api", UID: types.UID("deployment-uid"), Generation: 1},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
		},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 1},
	}
	client := fake.NewClientset(deployment)
	client.PrependReactor("list", "events", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: action.GetResource().Group, Resource: "events"}, "", nil)
	})

	var out, errOut bytes.Buffer
	cmd := newRootCommand(genericclioptions.IOStreams{Out: &out, ErrOut: &errOut}, dependencies{
		clientFactory: func() (kubernetes.Interface, error) { return client, nil },
		now:           func() time.Time { return now },
	})
	cmd.SetArgs([]string{"rollout", "explain", "api", "-n", "production", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"status": "Unknown"`) || !strings.Contains(out.String(), "Related Events are unavailable") {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func commandTestPod(namespace, name, node string, labels map[string]string, now time.Time) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			UID:               types.UID(name),
			Labels:            labels,
			CreationTimestamp: metav1.NewTime(now.Add(-time.Minute)),
		},
		Spec: corev1.PodSpec{NodeName: node, SchedulerName: corev1.DefaultSchedulerName},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(now.Add(-30 * time.Second)),
			}},
		},
	}
}

func commandRestartStatus(name string, finishedAt time.Time) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name:         name,
		RestartCount: 3,
		LastTerminationState: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				Reason:     "OOMKilled",
				ExitCode:   137,
				FinishedAt: metav1.NewTime(finishedAt),
			},
		},
	}
}

func commandTestNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
			corev1.ResourcePods:   resource.MustParse("10"),
		}},
	}
}
