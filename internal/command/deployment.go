package command

import "github.com/spf13/cobra"

func newDeploymentCommand(root *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deployment",
		Aliases: []string{"deploy", "deployments"},
		Short:   "Inspect Deployment resources and placement",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newDeploymentResourcesCommand(root))
	return cmd
}
