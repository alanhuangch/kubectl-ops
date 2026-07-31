package command

import (
	"context"
	"fmt"

	drainanalysis "github.com/alanhuangch/kubectl-ops/internal/drain"
	"github.com/spf13/cobra"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

type nodeDrainCheckOptions struct {
	root               *rootOptions
	node               string
	ignoreDaemonSets   bool
	force              bool
	deleteEmptyDirData bool
}

func newNodeDrainCheckCommand(root *rootOptions) *cobra.Command {
	options := &nodeDrainCheckOptions{root: root}
	cmd := &cobra.Command{
		Use:   "drain-check NODE",
		Short: "Check whether Pods on a Node can be drained",
		Long:  "Perform a read-only pre-drain analysis of Mirror Pods, DaemonSet Pods, unmanaged Pods, emptyDir data, terminal Pods, and PodDisruptionBudgets.",
		Example: `  kubectl ops node drain-check worker-07
  kubectl ops node drain-check worker-07 --ignore-daemonsets
  kubectl ops node drain-check worker-07 --ignore-daemonsets --force --delete-emptydir-data -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.node = args[0]
			if err := options.validate(); err != nil {
				return err
			}
			return options.run(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&options.ignoreDaemonSets, "ignore-daemonsets", false, "Allow drain to continue while skipping DaemonSet-managed Pods")
	cmd.Flags().BoolVar(&options.force, "force", false, "Allow deletion of Pods without a controller owner")
	cmd.Flags().BoolVar(&options.deleteEmptyDirData, "delete-emptydir-data", false, "Allow deletion of Pods using emptyDir volumes")
	return cmd
}

func (options *nodeDrainCheckOptions) validate() error {
	if _, err := options.root.dependencies.writerFactory(options.root.outputFormat); err != nil {
		return err
	}
	return nil
}

func (options *nodeDrainCheckOptions) run(ctx context.Context) error {
	client, err := options.root.dependencies.clientFactory()
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	if _, err := client.CoreV1().Nodes().Get(ctx, options.node, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("get Node %q: %w", options.node, err)
	}

	pods, err := listPods(ctx, client, metav1.NamespaceAll, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", options.node).String(),
		Limit:         podListPageSize,
	})
	if err != nil {
		return fmt.Errorf("list Pods assigned to Node %q: %w", options.node, err)
	}

	pdbsKnown := true
	var pdbs []policyv1.PodDisruptionBudget
	pdbOptions := metav1.ListOptions{Limit: podListPageSize}
	for {
		page, listErr := client.PolicyV1().PodDisruptionBudgets(metav1.NamespaceAll).List(ctx, pdbOptions)
		if listErr != nil {
			if apierrors.IsForbidden(listErr) || apierrors.IsUnauthorized(listErr) {
				pdbsKnown = false
				pdbs = nil
				break
			}
			return fmt.Errorf("list PodDisruptionBudgets: %w", listErr)
		}
		pdbs = append(pdbs, page.Items...)
		if page.Continue == "" {
			break
		}
		pdbOptions.Continue = page.Continue
	}

	report := drainanalysis.Analyze(options.node, pods, pdbs, drainanalysis.Options{
		Now:                options.root.dependencies.now(),
		IgnoreDaemonSets:   options.ignoreDaemonSets,
		Force:              options.force,
		DeleteEmptyDirData: options.deleteEmptyDirData,
		PDBsKnown:          pdbsKnown,
	})
	writer, err := options.root.dependencies.writerFactory(options.root.outputFormat)
	if err != nil {
		return err
	}
	if err := writer.WriteNodeDrainCheck(options.root.streams.Out, report); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if options.root.outputFormat != "json" {
		for _, warning := range report.Warnings {
			fmt.Fprintf(options.root.streams.ErrOut, "warning: %s\n", warning)
		}
	}
	return nil
}
