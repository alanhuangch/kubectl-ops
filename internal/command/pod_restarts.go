package command

import (
	"context"
	"fmt"
	"time"

	"github.com/alanhuangch/kubectl-ops/internal/pod"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

type podRestartsOptions struct {
	root      *rootOptions
	node      string
	since     time.Duration
	selector  string
	namespace string
}

func newPodRestartsCommand(root *rootOptions) *cobra.Command {
	options := &podRestartsOptions{
		root:  root,
		since: time.Hour,
	}
	cmd := &cobra.Command{
		Use:   "restarts",
		Short: "List recently restarted containers",
		Long:  "List regular, init, and ephemeral containers whose most recent termination happened within the selected duration. Kubernetes Pod status only retains the most recent terminated state.",
		Example: `  kubectl ops pod restarts -A --since 1h
  kubectl ops pod restarts --node worker-07
  kubectl ops pod restarts -n production -l app=payment`,
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
	cmd.Flags().DurationVar(&options.since, "since", time.Hour, "Only include the most recent termination within this duration")
	cmd.Flags().StringVarP(&options.selector, "selector", "l", "", "Selector (label query) to filter Pods")
	return cmd
}

func (options *podRestartsOptions) complete(cmd *cobra.Command) error {
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

func (options *podRestartsOptions) validate() error {
	if options.since <= 0 {
		return fmt.Errorf("--since must be greater than zero")
	}
	if _, err := labels.Parse(options.selector); err != nil {
		return fmt.Errorf("invalid label selector: %w", err)
	}
	if _, err := options.root.dependencies.writerFactory(options.root.outputFormat); err != nil {
		return err
	}
	return nil
}

func (options *podRestartsOptions) run(ctx context.Context) error {
	client, err := options.root.dependencies.clientFactory()
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}

	pods, err := listPods(ctx, client, options.namespace, options.listOptions())
	if err != nil {
		return fmt.Errorf("list Pods: %w", err)
	}
	report := pod.AnalyzeRestarts(pods, pod.RestartOptions{
		Now:   options.root.dependencies.now(),
		Since: options.since,
		Node:  options.node,
	})
	writer, err := options.root.dependencies.writerFactory(options.root.outputFormat)
	if err != nil {
		return err
	}
	if err := writer.WriteRestarts(options.root.streams.Out, report); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if options.root.outputFormat != "json" && report.IgnoredMissingFinishedAt > 0 {
		fmt.Fprintf(options.root.streams.ErrOut, "warning: ignored %d restarted container(s) without a latest termination finishedAt time\n", report.IgnoredMissingFinishedAt)
	}
	return nil
}

func (options *podRestartsOptions) listOptions() metav1.ListOptions {
	listOptions := metav1.ListOptions{
		LabelSelector: options.selector,
		Limit:         podListPageSize,
	}
	if options.node != "" {
		listOptions.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", options.node).String()
	}
	return listOptions
}
