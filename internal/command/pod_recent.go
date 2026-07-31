package command

import (
	"context"
	"fmt"
	"time"

	"github.com/alanhuangch/kubectl-ops/internal/pod"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

const podListPageSize int64 = 500

type podRecentOptions struct {
	root          *rootOptions
	node          string
	since         time.Duration
	limit         int
	selector      string
	phase         string
	scheduler     string
	includeStatic bool
	namespace     string
}

func newPodRecentCommand(root *rootOptions) *cobra.Command {
	options := &podRecentOptions{
		root:  root,
		since: time.Hour,
		limit: 50,
	}
	cmd := &cobra.Command{
		Use:   "recent",
		Short: "List recently scheduled Pods",
		Long:  "List Pods using the PodScheduled=True transition time. TimeToScheduled includes queueing, gates, retries, and scheduler processing.",
		Example: `  kubectl ops pod recent --node worker-07
  kubectl ops pod recent --node worker-07 --since 30m --limit 20
  kubectl ops pod recent -A --since 1h -l app=payment`,
		Args: cobra.NoArgs,
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
	cmd.Flags().StringVar(&options.node, "node", "", "Filter by node name using a server-side field selector")
	cmd.Flags().DurationVar(&options.since, "since", time.Hour, "Only include Pods scheduled within this duration")
	cmd.Flags().IntVar(&options.limit, "limit", 50, "Maximum number of Pods to return")
	cmd.Flags().StringVarP(&options.selector, "selector", "l", "", "Selector (label query) to filter Pods")
	cmd.Flags().StringVar(&options.phase, "phase", "", "Filter by Pod phase")
	cmd.Flags().StringVar(&options.scheduler, "scheduler", "", "Filter by schedulerName")
	cmd.Flags().BoolVar(&options.includeStatic, "include-static", false, "Include Static/Mirror Pods")
	return cmd
}

func (options *podRecentOptions) complete(cmd *cobra.Command) error {
	if options.root.allNamespaces {
		if cmd.Flags().Changed("namespace") {
			return fmt.Errorf("--namespace and --all-namespaces cannot be used together")
		}
		options.namespace = metav1.NamespaceAll
		return nil
	}

	namespace, _, err := options.root.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("resolve namespace: %w", err)
	}
	options.namespace = namespace
	return nil
}

func (options *podRecentOptions) validate() error {
	if options.since <= 0 {
		return fmt.Errorf("--since must be greater than zero")
	}
	if options.limit <= 0 {
		return fmt.Errorf("--limit must be greater than zero")
	}
	if _, err := labels.Parse(options.selector); err != nil {
		return fmt.Errorf("invalid label selector: %w", err)
	}
	if options.phase != "" {
		switch corev1.PodPhase(options.phase) {
		case corev1.PodPending, corev1.PodRunning, corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown:
		default:
			return fmt.Errorf("invalid Pod phase %q", options.phase)
		}
	}
	if _, err := options.root.dependencies.writerFactory(options.root.outputFormat); err != nil {
		return err
	}
	return nil
}

func (options *podRecentOptions) run(ctx context.Context) error {
	client, err := options.root.dependencies.clientFactory()
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}

	pods, err := listPods(ctx, client, options.namespace, options.listOptions())
	if err != nil {
		return fmt.Errorf("list Pods: %w", err)
	}

	report := pod.AnalyzeRecent(pods, pod.RecentOptions{
		Now:           options.root.dependencies.now(),
		Since:         options.since,
		Limit:         options.limit,
		Node:          options.node,
		Phase:         corev1.PodPhase(options.phase),
		Scheduler:     options.scheduler,
		IncludeStatic: options.includeStatic,
	})
	writer, err := options.root.dependencies.writerFactory(options.root.outputFormat)
	if err != nil {
		return err
	}
	if err := writer.WriteRecent(options.root.streams.Out, report); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if options.root.outputFormat != "json" {
		if report.IgnoredMissingScheduled > 0 {
			fmt.Fprintf(options.root.streams.ErrOut, "warning: ignored %d Pod(s) without a PodScheduled=True transition time\n", report.IgnoredMissingScheduled)
		}
		if report.ExcludedStatic > 0 {
			fmt.Fprintf(options.root.streams.ErrOut, "warning: excluded %d Static/Mirror Pod(s); use --include-static to include them\n", report.ExcludedStatic)
		}
	}
	return nil
}

func (options *podRecentOptions) listOptions() metav1.ListOptions {
	listOptions := metav1.ListOptions{
		LabelSelector: options.selector,
		Limit:         podListPageSize,
	}
	if options.node != "" {
		listOptions.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", options.node).String()
	}
	return listOptions
}

func listPods(ctx context.Context, client kubernetes.Interface, namespace string, options metav1.ListOptions) ([]corev1.Pod, error) {
	var result []corev1.Pod
	for {
		page, err := client.CoreV1().Pods(namespace).List(ctx, options)
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
