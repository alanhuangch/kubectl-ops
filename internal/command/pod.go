package command

import "github.com/spf13/cobra"

func newPodCommand(root *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pod",
		Short: "Diagnose and inspect Pods",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newPodPendingCommand(root))
	cmd.AddCommand(newPodRecentCommand(root))
	cmd.AddCommand(newPodRestartsCommand(root))
	return cmd
}
