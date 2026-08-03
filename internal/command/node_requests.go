package command

import (
	"context"
	"fmt"

	nodeanalysis "github.com/alanhuangch/kubectl-ops/internal/node"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/validation"
)

type nodeRequestsOptions struct {
	root         *rootOptions
	node         string
	top          int
	resource     string
	showExtended bool
	showPods     bool
	namespace    string
	onlyResource bool
}

func newNodeRequestsCommand(root *rootOptions) *cobra.Command {
	options := &nodeRequestsOptions{
		root:     root,
		top:      10,
		resource: string(corev1.ResourceCPU),
	}
	cmd := &cobra.Command{
		Use:   "requests NODE",
		Short: "Show allocatable capacity, Pod requests, and top consumers",
		Long:  "Compare Node allocatable resources with requests from active assigned Pods. This reports scheduling requests, not live utilization.",
		Example: `  kubectl ops node requests worker-07
  kubectl ops node requests worker-07 --top 10
  kubectl ops node requests worker-07 --resource memory
  kubectl ops node requests worker-07 --extended
  kubectl ops node requests worker-07 --pods
  kubectl ops node requests worker-07 --pods -n production
  kubectl ops node requests worker-07 -A --resource nvidia.com/gpu --only-resource --pods --top 0`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.node = args[0]
			if err := options.complete(cmd); err != nil {
				return err
			}
			if err := options.validate(cmd); err != nil {
				return err
			}
			return options.run(cmd.Context())
		},
	}
	cmd.Flags().IntVar(&options.top, "top", 10, "Number of top requesting Pods to show")
	cmd.Flags().StringVar(&options.resource, "resource", string(corev1.ResourceCPU), "Resource used to rank top consumers")
	cmd.Flags().BoolVar(&options.showExtended, "extended", false, "Show Pods requesting scalar extended resources")
	cmd.Flags().BoolVar(&options.showPods, "pods", false, "Show per-Pod requests and limits for all resource types")
	cmd.Flags().BoolVar(&options.onlyResource, "only-resource", false, "Only show the selected --resource in summaries and Pod details")
	return cmd
}

func (options *nodeRequestsOptions) complete(cmd *cobra.Command) error {
	if options.root.allNamespaces {
		if cmd.Flags().Changed("namespace") {
			return fmt.Errorf("--namespace and --all-namespaces cannot be used together")
		}
		options.namespace = metav1.NamespaceAll
		return nil
	}
	if !cmd.Flags().Changed("namespace") {
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

func (options *nodeRequestsOptions) validate(cmd *cobra.Command) error {
	if options.top < 0 {
		return fmt.Errorf("--top must be zero or greater")
	}
	if options.showExtended && options.showPods {
		return fmt.Errorf("--extended cannot be combined with --pods because --pods already includes extended resources")
	}
	if options.onlyResource && !cmd.Flags().Changed("resource") {
		return fmt.Errorf("--only-resource requires an explicit --resource")
	}
	if errors := validation.IsQualifiedName(options.resource); len(errors) > 0 {
		return fmt.Errorf("invalid resource name %q: %s", options.resource, errors[0])
	}
	if _, err := options.root.dependencies.writerFactory(options.root.outputFormat); err != nil {
		return err
	}
	return nil
}

func (options *nodeRequestsOptions) run(ctx context.Context) error {
	client, err := options.root.dependencies.clientFactory()
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	item, err := client.CoreV1().Nodes().Get(ctx, options.node, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Node %q: %w", options.node, err)
	}

	podsKnown := true
	pods, err := listPods(ctx, client, options.namespace, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", options.node).String(),
		Limit:         podListPageSize,
	})
	if err != nil {
		if apierrors.IsForbidden(err) {
			podsKnown = false
			pods = nil
		} else {
			return fmt.Errorf("list Pods assigned to Node %q: %w", options.node, err)
		}
	}

	resourceFilter := corev1.ResourceName("")
	if options.onlyResource {
		resourceFilter = corev1.ResourceName(options.resource)
	}
	report := nodeanalysis.AnalyzeRequests(item, pods, nodeanalysis.RequestsOptions{
		Now:            options.root.dependencies.now(),
		Namespace:      options.namespace,
		Top:            options.top,
		TopResource:    corev1.ResourceName(options.resource),
		PodsKnown:      podsKnown,
		ShowExtended:   options.showExtended,
		ShowPods:       options.showPods,
		ResourceFilter: resourceFilter,
	})
	writer, err := options.root.dependencies.writerFactory(options.root.outputFormat)
	if err != nil {
		return err
	}
	if err := writer.WriteNodeRequests(options.root.streams.Out, report); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if options.root.outputFormat != "json" {
		for _, warning := range report.Warnings {
			fmt.Fprintf(options.root.streams.ErrOut, "warning: %s\n", warning)
		}
	}
	return nil
}
