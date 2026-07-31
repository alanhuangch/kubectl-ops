package command

import (
	"encoding/json"
	"fmt"

	"github.com/alanhuangch/kubectl-ops/internal/output"
	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

type versionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

func newVersionCommand(root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := versionInfo{Version: version, Commit: commit, BuildDate: buildDate}
			switch root.outputFormat {
			case output.FormatJSON:
				encoder := json.NewEncoder(root.streams.Out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(info)
			case "", output.FormatTable, output.FormatWide:
				_, err := fmt.Fprintf(root.streams.Out, "kubectl-ops %s (commit: %s, built: %s)\n", info.Version, info.Commit, info.BuildDate)
				return err
			default:
				return fmt.Errorf("unsupported output format %q: expected table, wide, or json", root.outputFormat)
			}
		},
	}
}
