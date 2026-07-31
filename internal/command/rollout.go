package command

import "github.com/spf13/cobra"

func newRolloutCommand(root *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollout",
		Short: "Explain workload rollouts across related resources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newRolloutExplainCommand(root))
	return cmd
}
