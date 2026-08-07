package command

import "github.com/spf13/cobra"

func newWorkloadCommand(root *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workload",
		Aliases: []string{"workloads"},
		Short:   "Inspect workload resources and placement",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newWorkloadResourcesCommand(root))
	return cmd
}
