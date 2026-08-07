package output

import (
	"fmt"
	"io"

	"github.com/alanhuangch/kubectl-ops/internal/drain"
	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	nodeanalysis "github.com/alanhuangch/kubectl-ops/internal/node"
	"github.com/alanhuangch/kubectl-ops/internal/pending"
	"github.com/alanhuangch/kubectl-ops/internal/pod"
	"github.com/alanhuangch/kubectl-ops/internal/rollout"
	workloadanalysis "github.com/alanhuangch/kubectl-ops/internal/workload"
)

const (
	FormatTable = "table"
	FormatJSON  = "json"
	FormatWide  = "wide"
)

type Writer interface {
	WriteRecent(io.Writer, pod.RecentReport) error
	WriteRestarts(io.Writer, pod.RestartReport) error
	WriteNodeRequests(io.Writer, nodeanalysis.RequestsReport) error
	WriteWorkloadResources(io.Writer, workloadanalysis.Report) error
	WriteNodeDrainCheck(io.Writer, drain.Report) error
	WritePending(io.Writer, pending.Report) error
	WriteEventsTimeline(io.Writer, timeline.Report) error
	WriteRolloutExplain(io.Writer, rollout.Report) error
}

type WriterFactory func(string) (Writer, error)

func NewWriter(format string) (Writer, error) {
	switch format {
	case "", FormatTable:
		return tableWriter{}, nil
	case FormatWide:
		return tableWriter{wide: true}, nil
	case FormatJSON:
		return jsonWriter{}, nil
	default:
		return nil, fmt.Errorf("unsupported output format %q: expected table, wide, or json", format)
	}
}
