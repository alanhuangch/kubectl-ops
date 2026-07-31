package command

import (
	"context"
	"fmt"

	"github.com/alanhuangch/kubectl-ops/internal/pending"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

type podPendingOptions struct {
	root         *rootOptions
	name         string
	namespace    string
	allNodes     bool
	closest      int
	topConsumers int
}

func newPodPendingCommand(root *rootOptions) *cobra.Command {
	options := &podPendingOptions{
		root:         root,
		closest:      5,
		topConsumers: 5,
	}
	cmd := &cobra.Command{
		Use:   "pending POD",
		Short: "Explain observed and current-state scheduling blockers",
		Long:  "Show the PodScheduled condition and FailedScheduling Events, then evaluate supported scheduling constraints against the current Node and Pod state.",
		Example: `  kubectl ops pod pending -n production api-7d8f
  kubectl ops pod pending -n production api-7d8f --all-nodes
  kubectl ops pod pending -n production api-7d8f --top-consumers 10`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.name = args[0]
			if err := options.complete(cmd); err != nil {
				return err
			}
			if err := options.validate(); err != nil {
				return err
			}
			return options.run(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&options.allNodes, "all-nodes", false, "Show analysis for every Node")
	cmd.Flags().IntVar(&options.closest, "closest", 5, "Number of closest Nodes to show")
	cmd.Flags().IntVar(&options.topConsumers, "top-consumers", 5, "Top consumers to show for a limiting resource; zero disables")
	return cmd
}

func (options *podPendingOptions) complete(cmd *cobra.Command) error {
	if options.root.allNamespaces {
		return fmt.Errorf("--all-namespaces cannot be used when selecting one Pending Pod")
	}
	namespace, _, err := options.root.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("resolve namespace: %w", err)
	}
	options.namespace = namespace
	return nil
}

func (options *podPendingOptions) validate() error {
	if options.closest <= 0 {
		return fmt.Errorf("--closest must be greater than zero")
	}
	if options.topConsumers < 0 {
		return fmt.Errorf("--top-consumers must be zero or greater")
	}
	if _, err := options.root.dependencies.writerFactory(options.root.outputFormat); err != nil {
		return err
	}
	return nil
}

func (options *podPendingOptions) run(ctx context.Context) error {
	client, err := options.root.dependencies.clientFactory()
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	target, err := client.CoreV1().Pods(options.namespace).Get(ctx, options.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Pod %s/%s: %w", options.namespace, options.name, err)
	}
	if target.Status.Phase != corev1.PodPending {
		return fmt.Errorf("Pod %s/%s is not Pending (phase: %s)", options.namespace, options.name, target.Status.Phase)
	}

	snapshot := pending.Snapshot{
		CapturedAt:  options.root.dependencies.now(),
		TargetPod:   target,
		NodesKnown:  true,
		PodsKnown:   true,
		EventsKnown: true,
	}
	events, err := listPodEvents(ctx, client, target)
	if err != nil {
		if apierrors.IsForbidden(err) {
			snapshot.EventsKnown = false
			snapshot.Warnings = append(snapshot.Warnings, "FailedScheduling Events are unavailable because Events could not be listed.")
		} else {
			return fmt.Errorf("list Events for Pod %s/%s: %w", target.Namespace, target.Name, err)
		}
	} else {
		snapshot.Events = events
	}

	nodes, err := listNodes(ctx, client)
	if err != nil {
		if apierrors.IsForbidden(err) {
			snapshot.NodesKnown = false
			snapshot.PodsKnown = false
			snapshot.Warnings = append(snapshot.Warnings, "current-state Node analysis is unavailable because Nodes could not be listed.")
		} else {
			return fmt.Errorf("list Nodes: %w", err)
		}
	} else {
		snapshot.Nodes = nodes
		pods, listErr := listPods(ctx, client, metav1.NamespaceAll, metav1.ListOptions{Limit: podListPageSize})
		if listErr != nil {
			if apierrors.IsForbidden(listErr) {
				snapshot.PodsKnown = false
				snapshot.Warnings = append(snapshot.Warnings, "resource and HostPort analysis are unavailable because Pods could not be listed cluster-wide.")
			} else {
				return fmt.Errorf("list Pods for current-state analysis: %w", listErr)
			}
		} else {
			snapshot.PodsByNode = pending.IndexPodsByNode(pods)
		}
	}

	report := pending.Analyze(snapshot, pending.Options{
		Closest:      options.closest,
		TopConsumers: options.topConsumers,
		AllNodes:     options.allNodes,
	})
	writer, err := options.root.dependencies.writerFactory(options.root.outputFormat)
	if err != nil {
		return err
	}
	if err := writer.WritePending(options.root.streams.Out, report); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if options.root.outputFormat != "json" {
		for _, warning := range report.Warnings {
			fmt.Fprintf(options.root.streams.ErrOut, "warning: %s\n", warning)
		}
	}
	return nil
}

func listPodEvents(ctx context.Context, client kubernetes.Interface, target *corev1.Pod) ([]corev1.Event, error) {
	options := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("involvedObject.uid", string(target.UID)).String(),
		Limit:         podListPageSize,
	}
	var result []corev1.Event
	for {
		page, err := client.CoreV1().Events(target.Namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		for _, event := range page.Items {
			if event.InvolvedObject.UID == target.UID {
				result = append(result, event)
			}
		}
		if page.Continue == "" {
			return result, nil
		}
		options.Continue = page.Continue
	}
}

func listNodes(ctx context.Context, client kubernetes.Interface) ([]corev1.Node, error) {
	options := metav1.ListOptions{Limit: podListPageSize}
	var result []corev1.Node
	for {
		page, err := client.CoreV1().Nodes().List(ctx, options)
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
