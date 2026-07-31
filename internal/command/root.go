package command

import (
	"fmt"
	"time"

	"github.com/alanhuangch/kubectl-ops/internal/kube"
	"github.com/alanhuangch/kubectl-ops/internal/output"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type dependencies struct {
	clientFactory kube.ClientFactory
	writerFactory output.WriterFactory
	now           func() time.Time
}

type rootOptions struct {
	configFlags   *genericclioptions.ConfigFlags
	streams       genericclioptions.IOStreams
	outputFormat  string
	allNamespaces bool
	noColor       bool
	dependencies  dependencies
}

func NewRootCommand(streams genericclioptions.IOStreams) *cobra.Command {
	return newRootCommand(streams, dependencies{})
}

func newRootCommand(streams genericclioptions.IOStreams, deps dependencies) *cobra.Command {
	configFlags := genericclioptions.NewConfigFlags(true)
	if deps.clientFactory == nil {
		deps.clientFactory = kube.NewClientFactory(configFlags)
	}
	if deps.writerFactory == nil {
		deps.writerFactory = output.NewWriter
	}
	if deps.now == nil {
		deps.now = time.Now
	}

	options := &rootOptions{
		configFlags:  configFlags,
		streams:      streams,
		dependencies: deps,
	}
	cmd := &cobra.Command{
		Use:          "kubectl-ops",
		Short:        "Operational diagnostics for Kubernetes",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "kubectl ops",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.SetIn(streams.In)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)

	configFlags.AddFlags(cmd.PersistentFlags())
	cmd.PersistentFlags().BoolVarP(&options.allNamespaces, "all-namespaces", "A", false, "If present, list the requested object(s) across all namespaces")
	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", output.FormatTable, "Output format: table, wide, or json")
	cmd.PersistentFlags().BoolVar(&options.noColor, "no-color", false, "Disable color output")

	cmd.AddCommand(newPodCommand(options))
	cmd.AddCommand(newNodeCommand(options))
	cmd.AddCommand(newEventsCommand(options))
	cmd.AddCommand(newRolloutCommand(options))
	cmd.AddCommand(newVersionCommand(options))
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("invalid arguments: %w", err)
	})
	return cmd
}
