package command

import "github.com/spf13/cobra"

func newNodeCommand(root *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Inspect Node capacity and workloads",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newNodeRequestsCommand(root))
	cmd.AddCommand(newNodeDrainCheckCommand(root))
	return cmd
}
