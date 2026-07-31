package command

import "github.com/spf13/cobra"

func newEventsCommand(root *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Inspect Kubernetes Events",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newEventsTimelineCommand(root))
	return cmd
}
