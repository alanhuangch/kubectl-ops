package command

import (
	"context"
	"fmt"
	"strings"

	workloadanalysis "github.com/alanhuangch/kubectl-ops/internal/workload"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

var allWorkloadKinds = []workloadanalysis.Kind{
	workloadanalysis.KindDeployment,
	workloadanalysis.KindStatefulSet,
	workloadanalysis.KindDaemonSet,
	workloadanalysis.KindJob,
	workloadanalysis.KindCronJob,
	workloadanalysis.KindReplicaSet,
	workloadanalysis.KindPod,
}

type workloadResourcesOptions struct {
	root          *rootOptions
	namespace     string
	selector      string
	kinds         []string
	fixedKinds    []workloadanalysis.Kind
	resolvedKinds map[workloadanalysis.Kind]bool
	resourceClass string
}

func newWorkloadResourcesCommand(root *rootOptions) *cobra.Command {
	return newWorkloadResourcesCommandForKinds(root, nil)
}

func newDeploymentResourcesCommand(root *rootOptions) *cobra.Command {
	return newWorkloadResourcesCommandForKinds(root, []workloadanalysis.Kind{workloadanalysis.KindDeployment})
}

func newWorkloadResourcesCommandForKinds(root *rootOptions, fixedKinds []workloadanalysis.Kind) *cobra.Command {
	options := &workloadResourcesOptions{root: root, fixedKinds: fixedKinds, resourceClass: string(workloadanalysis.ResourceClassAll)}
	long := "Compare planned requests from workload specifications with actual requests from active owned Pods assigned to a Node. " +
		"Deployment, StatefulSet, DaemonSet, active Job, CronJob, standalone ReplicaSet, and standalone Pod resources are supported. " +
		"CPU, memory, GPU resources, nodeSelector, and required and preferred node affinity are included. This reports scheduling requests, not live utilization."
	examples := `  kubectl ops workload resources
  kubectl ops workload resources -A --resource-class gpu
  kubectl ops workload resources -A --kind deployment,statefulset
  kubectl ops workload resources -n production -l team=platform -o wide
  kubectl ops workload resources -A -o json`
	if len(fixedKinds) > 0 {
		long = "Compatibility view that compares planned Deployment requests with actual requests from active Pods assigned to a Node. " +
			"ReplicaSet ownership is followed across rolling updates. CPU, memory, GPU resources, nodeSelector, and required and preferred node affinity are included. " +
			"This reports scheduling requests, not live utilization."
		examples = `  kubectl ops deployment resources
  kubectl ops deployment resources -A --resource-class gpu
  kubectl ops deployment resources -n production -o wide`
	}
	cmd := &cobra.Command{
		Use:     "resources",
		Short:   "Show planned and actual workload resource requests",
		Long:    long,
		Example: examples,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := options.complete(cmd); err != nil {
				return err
			}
			if err := options.validate(); err != nil {
				return err
			}
			return options.run(cmd.Context())
		},
	}
	cmd.Flags().StringVarP(&options.selector, "selector", "l", "", "Selector (label query) to filter top-level workloads")
	cmd.Flags().StringVar(&options.resourceClass, "resource-class", string(workloadanalysis.ResourceClassAll), "Only show workloads requesting this resource class: all, cpu, memory, or gpu")
	if len(fixedKinds) == 0 {
		cmd.Flags().StringSliceVar(&options.kinds, "kind", nil, "Only show these workload kinds (comma-separated)")
	}
	return cmd
}

func (options *workloadResourcesOptions) complete(cmd *cobra.Command) error {
	if options.root.allNamespaces {
		if cmd.Flags().Changed("namespace") {
			return fmt.Errorf("--namespace and --all-namespaces cannot be used together")
		}
		options.namespace = metav1.NamespaceAll
	} else {
		namespace, _, err := options.root.configFlags.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return fmt.Errorf("resolve namespace: %w", err)
		}
		options.namespace = namespace
	}
	options.resolvedKinds = map[workloadanalysis.Kind]bool{}
	if len(options.fixedKinds) > 0 {
		for _, kind := range options.fixedKinds {
			options.resolvedKinds[kind] = true
		}
		return nil
	}
	if len(options.kinds) == 0 {
		for _, kind := range allWorkloadKinds {
			options.resolvedKinds[kind] = true
		}
		return nil
	}
	for _, value := range options.kinds {
		kind, err := parseWorkloadKind(value)
		if err != nil {
			return err
		}
		options.resolvedKinds[kind] = true
	}
	return nil
}

func (options *workloadResourcesOptions) validate() error {
	if _, err := labels.Parse(options.selector); err != nil {
		return fmt.Errorf("invalid label selector: %w", err)
	}
	switch workloadanalysis.ResourceClass(strings.ToLower(options.resourceClass)) {
	case workloadanalysis.ResourceClassAll, workloadanalysis.ResourceClassCPU, workloadanalysis.ResourceClassMemory, workloadanalysis.ResourceClassGPU:
		options.resourceClass = strings.ToLower(options.resourceClass)
	default:
		return fmt.Errorf("invalid --resource-class %q: expected all, cpu, memory, or gpu", options.resourceClass)
	}
	if _, err := options.root.dependencies.writerFactory(options.root.outputFormat); err != nil {
		return err
	}
	return nil
}

func parseWorkloadKind(value string) (workloadanalysis.Kind, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "deployment", "deployments", "deploy":
		return workloadanalysis.KindDeployment, nil
	case "statefulset", "statefulsets", "sts":
		return workloadanalysis.KindStatefulSet, nil
	case "daemonset", "daemonsets", "ds":
		return workloadanalysis.KindDaemonSet, nil
	case "job", "jobs":
		return workloadanalysis.KindJob, nil
	case "cronjob", "cronjobs", "cj":
		return workloadanalysis.KindCronJob, nil
	case "replicaset", "replicasets", "rs":
		return workloadanalysis.KindReplicaSet, nil
	case "pod", "pods", "po":
		return workloadanalysis.KindPod, nil
	default:
		return "", fmt.Errorf("invalid workload kind %q", value)
	}
}

func (options *workloadResourcesOptions) run(ctx context.Context) error {
	client, err := options.root.dependencies.clientFactory()
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	selector, _ := labels.Parse(options.selector)
	snapshot := workloadanalysis.Snapshot{
		CapturedAt:        options.root.dependencies.now(),
		Namespace:         options.namespace,
		PodsKnown:         true,
		InventoryComplete: true,
	}

	var replicaSets []appsv1.ReplicaSet
	replicaSetsKnown := true
	if options.resolvedKinds[workloadanalysis.KindDeployment] || options.resolvedKinds[workloadanalysis.KindReplicaSet] {
		replicaSets, err = listWorkloadReplicaSets(ctx, client, options.namespace)
		if err != nil {
			if !isPermissionDenied(err) {
				return fmt.Errorf("list ReplicaSets: %w", err)
			}
			replicaSetsKnown = false
			markWorkloadInventoryPartial(&snapshot, "ReplicaSet")
		}
	}
	for index := range replicaSets {
		item := &replicaSets[index]
		owner := metav1.GetControllerOf(item)
		if owner != nil && owner.Kind == string(workloadanalysis.KindDeployment) {
			snapshot.OwnerLinks = append(snapshot.OwnerLinks, workloadanalysis.OwnerLink{UID: string(item.UID), OwnerUID: string(owner.UID)})
			continue
		}
		if options.resolvedKinds[workloadanalysis.KindReplicaSet] && owner == nil && selector.Matches(labels.Set(item.Labels)) {
			snapshot.Workloads = append(snapshot.Workloads, replicaSetWorkload(item))
		}
	}

	var jobs []batchv1.Job
	jobsKnown := true
	if options.resolvedKinds[workloadanalysis.KindJob] || options.resolvedKinds[workloadanalysis.KindCronJob] {
		jobs, err = listWorkloadJobs(ctx, client, options.namespace)
		if err != nil {
			if !isPermissionDenied(err) {
				return fmt.Errorf("list Jobs: %w", err)
			}
			jobsKnown = false
			markWorkloadInventoryPartial(&snapshot, "Job")
		}
	}
	for index := range jobs {
		item := &jobs[index]
		owner := metav1.GetControllerOf(item)
		if owner != nil && owner.Kind == string(workloadanalysis.KindCronJob) {
			snapshot.OwnerLinks = append(snapshot.OwnerLinks, workloadanalysis.OwnerLink{UID: string(item.UID), OwnerUID: string(owner.UID)})
			continue
		}
		if options.resolvedKinds[workloadanalysis.KindJob] && owner == nil && !terminalJob(item) && selector.Matches(labels.Set(item.Labels)) {
			snapshot.Workloads = append(snapshot.Workloads, jobWorkload(item))
		}
	}

	pods, err := listPods(ctx, client, options.namespace, metav1.ListOptions{Limit: podListPageSize})
	if err != nil {
		if !isPermissionDenied(err) {
			return fmt.Errorf("list Pods: %w", err)
		}
		snapshot.PodsKnown = false
		markWorkloadInventoryPartial(&snapshot, "Pod")
	} else {
		snapshot.Pods = pods
		if options.resolvedKinds[workloadanalysis.KindPod] {
			for index := range pods {
				item := &pods[index]
				if metav1.GetControllerOf(item) == nil && !terminalPod(item) && !isMirrorPod(item) && selector.Matches(labels.Set(item.Labels)) {
					snapshot.Workloads = append(snapshot.Workloads, podWorkload(item))
				}
			}
		}
	}

	if options.resolvedKinds[workloadanalysis.KindDeployment] {
		items, listErr := listWorkloadDeployments(ctx, client, options.namespace, options.selector)
		if listErr != nil {
			if !isPermissionDenied(listErr) {
				return fmt.Errorf("list Deployments: %w", listErr)
			}
			markWorkloadInventoryPartial(&snapshot, "Deployment")
		} else {
			for index := range items {
				snapshot.Workloads = append(snapshot.Workloads, deploymentWorkload(&items[index], replicaSetsKnown))
			}
		}
	}
	if options.resolvedKinds[workloadanalysis.KindStatefulSet] {
		items, listErr := listWorkloadStatefulSets(ctx, client, options.namespace, options.selector)
		if listErr != nil {
			if !isPermissionDenied(listErr) {
				return fmt.Errorf("list StatefulSets: %w", listErr)
			}
			markWorkloadInventoryPartial(&snapshot, "StatefulSet")
		} else {
			for index := range items {
				snapshot.Workloads = append(snapshot.Workloads, statefulSetWorkload(&items[index]))
			}
		}
	}
	if options.resolvedKinds[workloadanalysis.KindDaemonSet] {
		items, listErr := listWorkloadDaemonSets(ctx, client, options.namespace, options.selector)
		if listErr != nil {
			if !isPermissionDenied(listErr) {
				return fmt.Errorf("list DaemonSets: %w", listErr)
			}
			markWorkloadInventoryPartial(&snapshot, "DaemonSet")
		} else {
			for index := range items {
				snapshot.Workloads = append(snapshot.Workloads, daemonSetWorkload(&items[index]))
			}
		}
	}
	if options.resolvedKinds[workloadanalysis.KindCronJob] {
		items, listErr := listWorkloadCronJobs(ctx, client, options.namespace, options.selector)
		if listErr != nil {
			if !isPermissionDenied(listErr) {
				return fmt.Errorf("list CronJobs: %w", listErr)
			}
			markWorkloadInventoryPartial(&snapshot, "CronJob")
		} else {
			for index := range items {
				snapshot.Workloads = append(snapshot.Workloads, cronJobWorkload(&items[index], jobsKnown))
			}
		}
	}

	report := workloadanalysis.AnalyzeResources(snapshot, workloadanalysis.Options{ResourceClass: workloadanalysis.ResourceClass(options.resourceClass)})
	writer, err := options.root.dependencies.writerFactory(options.root.outputFormat)
	if err != nil {
		return err
	}
	if err := writer.WriteWorkloadResources(options.root.streams.Out, report); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if options.root.outputFormat != "json" {
		for _, warning := range report.Warnings {
			fmt.Fprintf(options.root.streams.ErrOut, "warning: %s\n", warning)
		}
	}
	return nil
}

func markWorkloadInventoryPartial(snapshot *workloadanalysis.Snapshot, kind string) {
	snapshot.InventoryComplete = false
	snapshot.Warnings = append(snapshot.Warnings, fmt.Sprintf("%s resources are unavailable because they could not be listed.", kind))
}

func desiredReplicas(value *int32) int32 {
	if value == nil {
		return 1
	}
	return *value
}

func deploymentWorkload(item *appsv1.Deployment, actualKnown bool) workloadanalysis.Workload {
	return workloadanalysis.Workload{
		Namespace: item.Namespace, Kind: workloadanalysis.KindDeployment, Name: item.Name, UID: string(item.UID),
		PlannedPods: desiredReplicas(item.Spec.Replicas), Template: *item.Spec.Template.DeepCopy(), ActualKnown: actualKnown,
	}
}

func statefulSetWorkload(item *appsv1.StatefulSet) workloadanalysis.Workload {
	return workloadanalysis.Workload{
		Namespace: item.Namespace, Kind: workloadanalysis.KindStatefulSet, Name: item.Name, UID: string(item.UID),
		PlannedPods: desiredReplicas(item.Spec.Replicas), Template: *item.Spec.Template.DeepCopy(), ActualKnown: true,
	}
}

func daemonSetWorkload(item *appsv1.DaemonSet) workloadanalysis.Workload {
	return workloadanalysis.Workload{
		Namespace: item.Namespace, Kind: workloadanalysis.KindDaemonSet, Name: item.Name, UID: string(item.UID),
		PlannedPods: item.Status.DesiredNumberScheduled, Template: *item.Spec.Template.DeepCopy(), ActualKnown: true,
	}
}

func replicaSetWorkload(item *appsv1.ReplicaSet) workloadanalysis.Workload {
	return workloadanalysis.Workload{
		Namespace: item.Namespace, Kind: workloadanalysis.KindReplicaSet, Name: item.Name, UID: string(item.UID),
		PlannedPods: desiredReplicas(item.Spec.Replicas), Template: *item.Spec.Template.DeepCopy(), ActualKnown: true,
	}
}

func jobWorkload(item *batchv1.Job) workloadanalysis.Workload {
	return workloadanalysis.Workload{
		Namespace: item.Namespace, Kind: workloadanalysis.KindJob, Name: item.Name, UID: string(item.UID),
		PlannedPods: desiredJobPods(item), Template: *item.Spec.Template.DeepCopy(), ActualKnown: true,
	}
}

func cronJobWorkload(item *batchv1.CronJob, actualKnown bool) workloadanalysis.Workload {
	planned := desiredReplicas(item.Spec.JobTemplate.Spec.Parallelism)
	if item.Spec.Suspend != nil && *item.Spec.Suspend {
		planned = 0
	}
	return workloadanalysis.Workload{
		Namespace: item.Namespace, Kind: workloadanalysis.KindCronJob, Name: item.Name, UID: string(item.UID),
		PlannedPods: planned, Template: *item.Spec.JobTemplate.Spec.Template.DeepCopy(), ActualKnown: actualKnown,
	}
}

func podWorkload(item *corev1.Pod) workloadanalysis.Workload {
	return workloadanalysis.Workload{
		Namespace: item.Namespace, Kind: workloadanalysis.KindPod, Name: item.Name, UID: string(item.UID),
		PlannedPods: 1, Template: corev1.PodTemplateSpec{Spec: *item.Spec.DeepCopy()}, ActualKnown: true,
	}
}

func desiredJobPods(item *batchv1.Job) int32 {
	if item.Spec.Suspend != nil && *item.Spec.Suspend {
		return 0
	}
	desired := desiredReplicas(item.Spec.Parallelism)
	if item.Spec.Completions != nil {
		remaining := *item.Spec.Completions - item.Status.Succeeded
		if remaining < 0 {
			remaining = 0
		}
		if remaining < desired {
			desired = remaining
		}
	}
	return desired
}

func terminalJob(item *batchv1.Job) bool {
	for _, condition := range item.Status.Conditions {
		if (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func terminalPod(item *corev1.Pod) bool {
	return item.Status.Phase == corev1.PodSucceeded || item.Status.Phase == corev1.PodFailed
}

func isMirrorPod(item *corev1.Pod) bool {
	return item.Annotations[corev1.MirrorPodAnnotationKey] != ""
}

func listWorkloadDeployments(ctx context.Context, client kubernetes.Interface, namespace, selector string) ([]appsv1.Deployment, error) {
	options := metav1.ListOptions{LabelSelector: selector, Limit: podListPageSize}
	var result []appsv1.Deployment
	for {
		page, err := client.AppsV1().Deployments(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if page.Continue == "" {
			return result, nil
		}
		options.Continue = page.Continue
	}
}

func listWorkloadStatefulSets(ctx context.Context, client kubernetes.Interface, namespace, selector string) ([]appsv1.StatefulSet, error) {
	options := metav1.ListOptions{LabelSelector: selector, Limit: podListPageSize}
	var result []appsv1.StatefulSet
	for {
		page, err := client.AppsV1().StatefulSets(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if page.Continue == "" {
			return result, nil
		}
		options.Continue = page.Continue
	}
}

func listWorkloadDaemonSets(ctx context.Context, client kubernetes.Interface, namespace, selector string) ([]appsv1.DaemonSet, error) {
	options := metav1.ListOptions{LabelSelector: selector, Limit: podListPageSize}
	var result []appsv1.DaemonSet
	for {
		page, err := client.AppsV1().DaemonSets(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if page.Continue == "" {
			return result, nil
		}
		options.Continue = page.Continue
	}
}

func listWorkloadReplicaSets(ctx context.Context, client kubernetes.Interface, namespace string) ([]appsv1.ReplicaSet, error) {
	options := metav1.ListOptions{Limit: podListPageSize}
	var result []appsv1.ReplicaSet
	for {
		page, err := client.AppsV1().ReplicaSets(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if page.Continue == "" {
			return result, nil
		}
		options.Continue = page.Continue
	}
}

func listWorkloadJobs(ctx context.Context, client kubernetes.Interface, namespace string) ([]batchv1.Job, error) {
	options := metav1.ListOptions{Limit: podListPageSize}
	var result []batchv1.Job
	for {
		page, err := client.BatchV1().Jobs(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if page.Continue == "" {
			return result, nil
		}
		options.Continue = page.Continue
	}
}

func listWorkloadCronJobs(ctx context.Context, client kubernetes.Interface, namespace, selector string) ([]batchv1.CronJob, error) {
	options := metav1.ListOptions{LabelSelector: selector, Limit: podListPageSize}
	var result []batchv1.CronJob
	for {
		page, err := client.BatchV1().CronJobs(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if page.Continue == "" {
			return result, nil
		}
		options.Continue = page.Continue
	}
}
