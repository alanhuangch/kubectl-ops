package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/alanhuangch/kubectl-ops/internal/drain"
	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	nodeanalysis "github.com/alanhuangch/kubectl-ops/internal/node"
	"github.com/alanhuangch/kubectl-ops/internal/pending"
	"github.com/alanhuangch/kubectl-ops/internal/pod"
	"github.com/alanhuangch/kubectl-ops/internal/rollout"
	workloadanalysis "github.com/alanhuangch/kubectl-ops/internal/workload"
	corev1 "k8s.io/api/core/v1"
)

type tableWriter struct {
	wide bool
}

func (writer tableWriter) WriteRecent(out io.Writer, report pod.RecentReport) error {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if writer.wide {
		if _, err := fmt.Fprintln(table, "NAMESPACE\tPOD\tNODE\tSCHEDULED\tWAIT\tPHASE\tSCHEDULER\tSTATIC"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(table, "NAMESPACE\tPOD\tNODE\tSCHEDULED\tWAIT\tPHASE"); err != nil {
		return err
	}

	for _, item := range report.Items {
		if writer.wide {
			if _, err := fmt.Fprintf(
				table,
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\n",
				item.Namespace,
				item.Name,
				item.Node,
				relativeTime(report.CapturedAt, item.ScheduledAt),
				humanDuration(item.TimeToScheduled),
				item.Phase,
				item.Scheduler,
				item.Static,
			); err != nil {
				return err
			}
			continue
		}

		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			item.Namespace,
			item.Name,
			item.Node,
			relativeTime(report.CapturedAt, item.ScheduledAt),
			humanDuration(item.TimeToScheduled),
			item.Phase,
		); err != nil {
			return err
		}
	}

	return table.Flush()
}

func (writer tableWriter) WriteRestarts(out io.Writer, report pod.RestartReport) error {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if writer.wide {
		if _, err := fmt.Fprintln(table, "NAMESPACE\tPOD\tCONTAINER\tTYPE\tNODE\tRESTARTS\tLAST-REASON\tCLASS\tEXIT\tSIGNAL\tFINISHED"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(table, "NAMESPACE\tPOD\tCONTAINER\tRESTARTS\tLAST-REASON\tEXIT\tFINISHED"); err != nil {
		return err
	}

	for _, item := range report.Items {
		if writer.wide {
			if _, err := fmt.Fprintf(
				table,
				"%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%d\t%d\t%s\n",
				item.Namespace,
				item.Pod,
				item.Container,
				item.Kind,
				item.Node,
				item.RestartCount,
				item.LastReason,
				item.Classification,
				item.ExitCode,
				item.Signal,
				relativeTime(report.CapturedAt, item.FinishedAt),
			); err != nil {
				return err
			}
			continue
		}

		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%d\t%s\t%d\t%s\n",
			item.Namespace,
			item.Pod,
			item.Container,
			item.RestartCount,
			item.LastReason,
			item.ExitCode,
			relativeTime(report.CapturedAt, item.FinishedAt),
		); err != nil {
			return err
		}
	}
	return table.Flush()
}

func (writer tableWriter) WriteNodeRequests(out io.Writer, report nodeanalysis.RequestsReport) error {
	if report.Namespace != "" {
		if _, err := fmt.Fprintf(out, "NAMESPACE: %s\n\n", report.Namespace); err != nil {
			return err
		}
	}
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "RESOURCE\tALLOCATABLE\tREQUESTED\tAVAILABLE\tRATIO"); err != nil {
		return err
	}
	for _, usage := range report.Resources {
		requested, available, ratio := "-", "-", "-"
		if usage.RequestsKnown {
			requested = usage.Requested.String()
		}
		if usage.AvailableKnown {
			available = usage.Available.String()
		}
		if usage.RatioKnown {
			ratio = fmt.Sprintf("%.1f%%", usage.Ratio)
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", usage.Resource, usage.Allocatable.String(), requested, available, ratio); err != nil {
			return err
		}
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if len(report.Consumers) > 0 {
		if _, err := fmt.Fprintf(out, "\nTOP CONSUMERS (%s)\n", report.TopResource); err != nil {
			return err
		}
		resourceColumns := topConsumerResourceColumns(report)
		consumers := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprint(consumers, "NAMESPACE\tPOD\t"); err != nil {
			return err
		}
		for _, name := range resourceColumns {
			if _, err := fmt.Fprintf(consumers, "%s\t", resourceColumnName(name)); err != nil {
				return err
			}
		}
		if writer.wide {
			if _, err := fmt.Fprintln(consumers, "CREATED\tSCHEDULED\tOWNER\tDAEMONSET"); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintln(consumers, "CREATED\tSCHEDULED\tOWNER"); err != nil {
			return err
		}
		for _, consumer := range report.Consumers {
			createdAt := relativeTimeOrDash(report.CapturedAt, consumer.CreatedAt)
			scheduledAt := relativeTimeOrDash(report.CapturedAt, consumer.ScheduledAt)
			if _, err := fmt.Fprintf(consumers, "%s\t%s\t", consumer.Namespace, consumer.Pod); err != nil {
				return err
			}
			for _, name := range resourceColumns {
				usage, found := findResourceUsage(consumer.Resources, name)
				cell := "-"
				if found {
					cell = compactPodResourceCell(usage, false)
				}
				if _, err := fmt.Fprintf(consumers, "%s\t", cell); err != nil {
					return err
				}
			}
			if writer.wide {
				if _, err := fmt.Fprintf(consumers, "%s\t%s\t%s\t%t\n", createdAt, scheduledAt, consumer.Owner, consumer.DaemonSet); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(consumers, "%s\t%s\t%s\n", createdAt, scheduledAt, consumer.Owner); err != nil {
				return err
			}
		}
		if err := consumers.Flush(); err != nil {
			return err
		}
	}

	if report.ShowPods {
		if err := writer.writePodResources(out, report); err != nil {
			return err
		}
	}
	if !report.ShowExtended {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\nEXTENDED RESOURCE PODS"); err != nil {
		return err
	}
	extended := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if writer.wide {
		if _, err := fmt.Fprintln(extended, "RESOURCE\tNAMESPACE\tPOD\tREQUEST\tCREATED\tSCHEDULED\tOWNER\tDAEMONSET"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(extended, "RESOURCE\tNAMESPACE\tPOD\tREQUEST\tCREATED\tSCHEDULED\tOWNER"); err != nil {
		return err
	}
	for _, consumer := range report.ExtendedConsumers {
		createdAt := relativeTimeOrDash(report.CapturedAt, consumer.CreatedAt)
		scheduledAt := relativeTimeOrDash(report.CapturedAt, consumer.ScheduledAt)
		if writer.wide {
			if _, err := fmt.Fprintf(extended, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\n", consumer.Resource, consumer.Namespace, consumer.Pod, consumer.Request.String(), createdAt, scheduledAt, consumer.Owner, consumer.DaemonSet); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(extended, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", consumer.Resource, consumer.Namespace, consumer.Pod, consumer.Request.String(), createdAt, scheduledAt, consumer.Owner); err != nil {
			return err
		}
	}
	return extended.Flush()
}

func (writer tableWriter) WriteWorkloadResources(out io.Writer, report workloadanalysis.Report) error {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if writer.wide {
		if _, err := fmt.Fprintln(table, "NAMESPACE\tKIND\tWORKLOAD\tUID\tPODS(PLAN/ACTUAL)\tCPU-REQUESTS(PLAN/ACTUAL)\tMEMORY-REQUESTS(PLAN/ACTUAL)\tGPU-REQUESTS(PLAN/ACTUAL)\tNODE-SELECTOR\tREQUIRED-NODE-AFFINITY\tPREFERRED-NODE-AFFINITY"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(table, "NAMESPACE\tKIND\tWORKLOAD\tPODS(PLAN/ACTUAL)\tCPU-REQUESTS(PLAN/ACTUAL)\tMEMORY-REQUESTS(PLAN/ACTUAL)\tGPU-REQUESTS(PLAN/ACTUAL)\tNODE-AFFINITY"); err != nil {
		return err
	}
	for _, item := range report.Items {
		pods := fmt.Sprintf("%d/-", item.Pods.Planned)
		if item.Pods.ActualKnown {
			pods = fmt.Sprintf("%d/%d", item.Pods.Planned, item.Pods.Actual)
		}
		cpu := workloadResourcePairCell(item.CPU)
		memory := workloadResourcePairCell(item.Memory)
		gpus := workloadGPUCell(item.GPUs, item.CPU.ActualKnown)
		if writer.wide {
			if _, err := fmt.Fprintf(
				table,
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				item.Namespace,
				item.Kind,
				item.Workload,
				item.UID,
				pods,
				cpu,
				memory,
				gpus,
				formatNodeSelector(item.Placement.NodeSelector),
				formatRequiredNodeAffinity(item.Placement.Required),
				formatPreferredNodeAffinity(item.Placement.Preferred),
			); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			item.Namespace,
			item.Kind,
			item.Workload,
			pods,
			cpu,
			memory,
			gpus,
			formatNodePlacement(item.Placement),
		); err != nil {
			return err
		}
	}
	return table.Flush()
}

func workloadResourcePairCell(item workloadanalysis.ResourcePair) string {
	if !item.ActualKnown {
		return item.Planned.String() + "/-"
	}
	return item.Planned.String() + "/" + item.Actual.String()
}

func workloadGPUCell(items []workloadanalysis.GPUResourcePair, actualKnown bool) string {
	if len(items) == 0 {
		if !actualKnown {
			return "0/-"
		}
		return "-"
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		value := item.Planned.String() + "/-"
		if item.ActualKnown {
			value = item.Planned.String() + "/" + item.Actual.String()
		}
		values = append(values, string(item.Resource)+"="+value)
	}
	return strings.Join(values, ",")
}

func formatNodePlacement(item workloadanalysis.NodePlacement) string {
	var sections []string
	if value := formatNodeSelector(item.NodeSelector); value != "-" {
		sections = append(sections, "selector: "+value)
	}
	if value := formatRequiredNodeAffinity(item.Required); value != "-" {
		sections = append(sections, "required: "+value)
	}
	if value := formatPreferredNodeAffinity(item.Preferred); value != "-" {
		sections = append(sections, "preferred: "+value)
	}
	if len(sections) == 0 {
		return "-"
	}
	return strings.Join(sections, "; ")
}

func formatNodeSelector(items []workloadanalysis.KeyValue) string {
	if len(items) == 0 {
		return "-"
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.Key+"="+item.Value)
	}
	return strings.Join(values, ",")
}

func formatRequiredNodeAffinity(items []workloadanalysis.NodeSelectorTerm) string {
	if len(items) == 0 {
		return "-"
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, "("+formatNodeSelectorTerm(item)+")")
	}
	return strings.Join(values, " OR ")
}

func formatPreferredNodeAffinity(items []workloadanalysis.PreferredNodeSelectorTerm) string {
	if len(items) == 0 {
		return "-"
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprintf("%d:(%s)", item.Weight, formatNodeSelectorTerm(item.Preference)))
	}
	return strings.Join(values, ", ")
}

func formatNodeSelectorTerm(item workloadanalysis.NodeSelectorTerm) string {
	values := make([]string, 0, len(item.MatchExpressions)+len(item.MatchFields))
	for _, requirement := range item.MatchExpressions {
		values = append(values, formatNodeSelectorRequirement(requirement, ""))
	}
	for _, requirement := range item.MatchFields {
		values = append(values, formatNodeSelectorRequirement(requirement, "field:"))
	}
	if len(values) == 0 {
		return "<empty>"
	}
	return strings.Join(values, " AND ")
}

func formatNodeSelectorRequirement(item workloadanalysis.NodeSelectorRequirement, prefix string) string {
	value := prefix + item.Key + " " + string(item.Operator)
	if len(item.Values) > 0 {
		value += " [" + strings.Join(item.Values, ",") + "]"
	}
	return value
}

func (writer tableWriter) writePodResources(out io.Writer, report nodeanalysis.RequestsReport) error {
	title := "\nPOD RESOURCES (REQUEST, NODE-RATIO)"
	if writer.wide {
		title = "\nPOD RESOURCES (REQUEST/LIMIT, NODE-RATIO)"
	}
	if _, err := fmt.Fprintln(out, title); err != nil {
		return err
	}
	resourceColumns := podResourceColumns(report)
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprint(table, "NAMESPACE\tPOD\t"); err != nil {
		return err
	}
	if writer.wide {
		if _, err := fmt.Fprint(table, "UID\t"); err != nil {
			return err
		}
	}
	for _, name := range resourceColumns {
		if _, err := fmt.Fprintf(table, "%s\t", resourceColumnName(name)); err != nil {
			return err
		}
	}
	if writer.wide {
		if _, err := fmt.Fprintln(table, "CREATED\tSCHEDULED\tOWNER\tDAEMONSET"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(table, "CREATED\tSCHEDULED\tOWNER"); err != nil {
		return err
	}

	for _, pod := range report.PodResources {
		createdAt := relativeTimeOrDash(report.CapturedAt, pod.CreatedAt)
		scheduledAt := relativeTimeOrDash(report.CapturedAt, pod.ScheduledAt)
		if _, err := fmt.Fprintf(table, "%s\t%s\t", pod.Namespace, pod.Pod); err != nil {
			return err
		}
		if writer.wide {
			if _, err := fmt.Fprintf(table, "%s\t", pod.UID); err != nil {
				return err
			}
		}
		for _, name := range resourceColumns {
			resource, found := findPodResource(pod, name)
			cell := "-"
			if found {
				cell = compactPodResourceCell(resource, writer.wide)
			}
			if _, err := fmt.Fprintf(table, "%s\t", cell); err != nil {
				return err
			}
		}
		if writer.wide {
			if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%t\n", createdAt, scheduledAt, pod.Owner, pod.DaemonSet); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", createdAt, scheduledAt, pod.Owner); err != nil {
			return err
		}
	}
	return table.Flush()
}

func podResourceColumns(report nodeanalysis.RequestsReport) []corev1.ResourceName {
	if len(report.Resources) == 1 {
		return []corev1.ResourceName{report.Resources[0].Resource}
	}
	seen := make(map[corev1.ResourceName]bool)
	columns := make([]corev1.ResourceName, 0)
	add := func(name corev1.ResourceName) {
		if seen[name] {
			return
		}
		seen[name] = true
		columns = append(columns, name)
	}
	for _, usage := range report.Resources {
		if usage.Resource == corev1.ResourceCPU || usage.Resource == corev1.ResourceMemory {
			add(usage.Resource)
		}
	}
	for _, usage := range report.Resources {
		if strings.Contains(string(usage.Resource), "/") {
			add(usage.Resource)
		}
	}
	for _, pod := range report.PodResources {
		for _, usage := range pod.Resources {
			if strings.Contains(string(usage.Resource), "/") {
				add(usage.Resource)
			}
		}
	}
	return columns
}

func topConsumerResourceColumns(report nodeanalysis.RequestsReport) []corev1.ResourceName {
	columns := podResourceColumns(report)
	for _, name := range columns {
		if name == report.TopResource {
			return columns
		}
	}
	return append(columns, report.TopResource)
}

func findPodResource(pod nodeanalysis.PodResourceBreakdown, name corev1.ResourceName) (nodeanalysis.PodResource, bool) {
	return findResourceUsage(pod.Resources, name)
}

func findResourceUsage(resources []nodeanalysis.PodResource, name corev1.ResourceName) (nodeanalysis.PodResource, bool) {
	for _, usage := range resources {
		if usage.Resource == name {
			return usage, true
		}
	}
	return nodeanalysis.PodResource{}, false
}

func compactPodResourceCell(usage nodeanalysis.PodResource, wide bool) string {
	request, limit, ratio := "-", "-", "-"
	if usage.RequestSet {
		request = usage.Request.String()
	}
	if usage.LimitSet {
		limit = usage.Limit.String()
	}
	if usage.RatioKnown {
		ratio = fmt.Sprintf("%.1f%%", usage.RequestRatio)
	}
	if wide {
		return fmt.Sprintf("%s/%s (%s)", request, limit, ratio)
	}
	if !usage.RequestSet {
		return "-"
	}
	return fmt.Sprintf("%s (%s)", request, ratio)
}

func resourceColumnName(name corev1.ResourceName) string {
	switch name {
	case corev1.ResourceCPU:
		return "CPU"
	case corev1.ResourceMemory:
		return "MEMORY"
	case corev1.ResourceEphemeralStorage:
		return "EPHEMERAL-STORAGE"
	case corev1.ResourcePods:
		return "PODS"
	default:
		return string(name)
	}
}

func (writer tableWriter) WritePending(out io.Writer, report pending.Report) error {
	if _, err := fmt.Fprintln(out, "SCHEDULER OBSERVED"); err != nil {
		return err
	}
	conditionTable := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(conditionTable, "CONDITION\tSTATUS\tREASON\tTRANSITION\tMESSAGE"); err != nil {
		return err
	}
	if report.Condition == nil {
		if _, err := fmt.Fprintln(conditionTable, "PodScheduled\tUnknown\t-\t-\tcondition not present"); err != nil {
			return err
		}
	} else {
		transition := "-"
		if !report.Condition.LastTransitionTime.IsZero() {
			transition = relativeTime(report.CapturedAt, report.Condition.LastTransitionTime)
		}
		if _, err := fmt.Fprintf(conditionTable, "PodScheduled\t%s\t%s\t%s\t%s\n", report.Condition.Status, displayValue(report.Condition.Reason), transition, oneLine(report.Condition.Message)); err != nil {
			return err
		}
	}
	if err := conditionTable.Flush(); err != nil {
		return err
	}
	if len(report.Events) > 0 {
		events := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(events, "EVENT\tTYPE\tCOUNT\tLAST\tMESSAGE"); err != nil {
			return err
		}
		for _, event := range report.Events {
			observedAt := "-"
			if !event.ObservedAt.IsZero() {
				observedAt = relativeTime(report.CapturedAt, event.ObservedAt)
			}
			if _, err := fmt.Fprintf(events, "%s\t%s\t%d\t%s\t%s\n", event.Reason, event.Type, event.Count, observedAt, oneLine(event.Message)); err != nil {
				return err
			}
		}
		if err := events.Flush(); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(out, "\nCURRENT-STATE ANALYSIS"); err != nil {
		return err
	}
	summary := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(summary, "NODES\tCODE\tCATEGORY"); err != nil {
		return err
	}
	if !report.CurrentStateAvailable {
		if _, err := fmt.Fprintln(summary, "-\tUnavailable\t-"); err != nil {
			return err
		}
	} else if len(report.Summary) == 0 {
		if _, err := fmt.Fprintln(summary, "0\tNone\t-"); err != nil {
			return err
		}
	} else {
		for _, item := range report.Summary {
			if _, err := fmt.Fprintf(summary, "%d\t%s\t%s\n", item.Count, item.Code, item.Category); err != nil {
				return err
			}
		}
	}
	if err := summary.Flush(); err != nil {
		return err
	}

	nodes := report.ClosestNodes
	title := "CLOSEST NODES"
	if report.ShowAllNodes {
		nodes = report.Nodes
		title = "ALL NODES"
	}
	if _, err := fmt.Fprintf(out, "\n%s\n", title); err != nil {
		return err
	}
	nodeTable := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(nodeTable, "NODE\tFAILURES\tNON-RESOURCE\tRESOURCE-GAP\tREASONS"); err != nil {
		return err
	}
	for _, node := range nodes {
		if _, err := fmt.Fprintf(nodeTable, "%s\t%d\t%d\t%.3f\t%s\n", node.Node, node.HardFailureCount, node.NonResourceFailures, node.ResourceGapScore, failureCodes(node.Failures)); err != nil {
			return err
		}
	}
	if err := nodeTable.Flush(); err != nil {
		return err
	}
	for _, node := range nodes {
		if len(node.TopConsumers) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(out, "\nTOP CONSUMERS (%s/%s)\n", node.Node, node.LimitingResource); err != nil {
			return err
		}
		consumers := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(consumers, "NAMESPACE\tPOD\tREQUEST"); err != nil {
			return err
		}
		for _, consumer := range node.TopConsumers {
			if _, err := fmt.Fprintf(consumers, "%s\t%s\t%s\n", consumer.Namespace, consumer.Pod, consumer.Request); err != nil {
				return err
			}
		}
		if err := consumers.Flush(); err != nil {
			return err
		}
	}

	if len(report.Unsupported) > 0 {
		if _, err := fmt.Fprintln(out, "\nUNSUPPORTED"); err != nil {
			return err
		}
		unsupported := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(unsupported, "CODE\tMESSAGE"); err != nil {
			return err
		}
		for _, item := range report.Unsupported {
			if _, err := fmt.Fprintf(unsupported, "%s\t%s\n", item.Code, item.Message); err != nil {
				return err
			}
		}
		return unsupported.Flush()
	}
	return nil
}

func displayValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func oneLine(value string) string {
	if value == "" {
		return "-"
	}
	return strings.Join(strings.Fields(value), " ")
}

func failureCodes(failures []pending.Failure) string {
	if len(failures) == 0 {
		return "None"
	}
	seen := map[string]bool{}
	var codes []string
	for _, failure := range failures {
		if !seen[failure.Code] {
			seen[failure.Code] = true
			codes = append(codes, failure.Code)
		}
	}
	return strings.Join(codes, ",")
}

func (writer tableWriter) WriteEventsTimeline(out io.Writer, report timeline.Report) error {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if writer.wide {
		if _, err := fmt.Fprintln(table, "LAST\tFIRST\tTYPE\tNAMESPACE\tOBJECT\tREASON\tACTION\tCOUNT\tSOURCE\tRELATED\tEVENT\tMESSAGE"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(table, "TIME\tTYPE\tNAMESPACE\tOBJECT\tREASON\tCOUNT\tSOURCE\tMESSAGE"); err != nil {
		return err
	}
	for _, item := range report.Items {
		object := eventObject(item.Regarding)
		if writer.wide {
			first := "-"
			if !item.FirstObservedAt.IsZero() {
				first = relativeTime(report.CapturedAt, item.FirstObservedAt)
			}
			if _, err := fmt.Fprintf(
				table,
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
				relativeTime(report.CapturedAt, item.LastObservedAt),
				first,
				item.Type,
				item.Namespace,
				object,
				item.Reason,
				displayValue(item.Action),
				item.Count,
				displayValue(item.ReportingController),
				eventRelated(item.Related),
				item.Name,
				oneLine(item.Note),
			); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			relativeTime(report.CapturedAt, item.LastObservedAt),
			item.Type,
			item.Namespace,
			object,
			item.Reason,
			item.Count,
			displayValue(item.ReportingController),
			oneLine(item.Note),
		); err != nil {
			return err
		}
	}
	return table.Flush()
}

func eventObject(item timeline.ObjectReference) string {
	if item.Kind == "" {
		return displayValue(item.Name)
	}
	return item.Kind + "/" + item.Name
}

func eventRelated(item *timeline.ObjectReference) string {
	if item == nil {
		return "-"
	}
	return eventObject(*item)
}

func (writer tableWriter) WriteNodeDrainCheck(out io.Writer, report drain.Report) error {
	summary := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(summary, "NODE\tDRAINABILITY\tCOMPLETENESS\tPODS\tEVICT\tDELETE\tSKIP\tBLOCKED"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		summary,
		"%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\n",
		report.Node,
		report.Drainability,
		report.Completeness,
		report.Summary.Pods,
		report.Summary.Evict,
		report.Summary.Delete,
		report.Summary.Skip,
		report.Summary.Blocked,
	); err != nil {
		return err
	}
	if err := summary.Flush(); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(out, "\nOPTIONS"); err != nil {
		return err
	}
	optionTable := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(optionTable, "IGNORE-DAEMONSETS\tFORCE\tDELETE-EMPTYDIR-DATA"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(optionTable, "%t\t%t\t%t\n", report.Options.IgnoreDaemonSets, report.Options.Force, report.Options.DeleteEmptyDirData); err != nil {
		return err
	}
	if err := optionTable.Flush(); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(out, "\nPOD IMPACTS"); err != nil {
		return err
	}
	pods := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if writer.wide {
		if _, err := fmt.Fprintln(pods, "NAMESPACE\tPOD\tUID\tOWNER\tPHASE\tACTION\tIMPACTS\tBLOCKERS"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(pods, "NAMESPACE\tPOD\tOWNER\tPHASE\tACTION\tIMPACTS\tBLOCKERS"); err != nil {
		return err
	}
	for _, pod := range report.Pods {
		if writer.wide {
			if _, err := fmt.Fprintf(pods, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", pod.Namespace, pod.Pod, pod.UID, pod.Owner, pod.Phase, pod.Action, drainCodes(pod.Impacts), drainCodes(pod.Blockers)); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(pods, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", pod.Namespace, pod.Pod, pod.Owner, pod.Phase, pod.Action, drainCodes(pod.Impacts), drainCodes(pod.Blockers)); err != nil {
			return err
		}
	}
	if err := pods.Flush(); err != nil {
		return err
	}

	if len(report.PDBs) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\nPDB IMPACTS"); err != nil {
		return err
	}
	pdbs := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(pdbs, "NAMESPACE\tPDB\tTARGETS\tALLOWED\tUNHEALTHY-EXEMPT\tBLOCKED\tSELECTOR-VALID"); err != nil {
		return err
	}
	for _, pdb := range report.PDBs {
		if _, err := fmt.Fprintf(pdbs, "%s\t%s\t%d\t%d\t%d\t%t\t%t\n", pdb.Namespace, pdb.Name, pdb.MatchedTargets, pdb.DisruptionsAllowed, pdb.UnhealthyExempt, pdb.Blocked, pdb.SelectorValid); err != nil {
			return err
		}
	}
	return pdbs.Flush()
}

func drainCodes(codes []drain.ImpactCode) string {
	if len(codes) == 0 {
		return "None"
	}
	values := make([]string, 0, len(codes))
	for _, code := range codes {
		values = append(values, string(code))
	}
	return strings.Join(values, ",")
}

func (writer tableWriter) WriteRolloutExplain(out io.Writer, report rollout.Report) error {
	deployment := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(deployment, "NAMESPACE\tDEPLOYMENT\tSTATUS\tCOMPLETENESS\tGENERATION\tOBSERVED\tDESIRED\tUPDATED\tREADY\tAVAILABLE\tUNAVAILABLE"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		deployment,
		"%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
		report.Deployment.Namespace,
		report.Deployment.Name,
		report.Status,
		report.Completeness,
		report.Deployment.Generation,
		report.Deployment.ObservedGeneration,
		report.Deployment.Desired,
		report.Deployment.Updated,
		report.Deployment.Ready,
		report.Deployment.Available,
		report.Deployment.Unavailable,
	); err != nil {
		return err
	}
	if err := deployment.Flush(); err != nil {
		return err
	}

	if len(report.Conditions) > 0 {
		if _, err := fmt.Fprintln(out, "\nCONDITIONS"); err != nil {
			return err
		}
		conditions := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(conditions, "TYPE\tSTATUS\tREASON\tTRANSITION\tMESSAGE"); err != nil {
			return err
		}
		for _, condition := range report.Conditions {
			if _, err := fmt.Fprintf(conditions, "%s\t%s\t%s\t%s\t%s\n", condition.Type, condition.Status, displayValue(condition.Reason), optionalRelativeTime(report.CapturedAt, condition.LastTransitionTime), oneLine(condition.Message)); err != nil {
				return err
			}
		}
		if err := conditions.Flush(); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(out, "\nREPLICA SETS"); err != nil {
		return err
	}
	replicaSets := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if writer.wide {
		if _, err := fmt.Fprintln(replicaSets, "REPLICASET\tUID\tREVISION\tCURRENT\tDESIRED\tREPLICAS\tREADY\tAVAILABLE\tAGE"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(replicaSets, "REPLICASET\tREVISION\tCURRENT\tDESIRED\tREPLICAS\tREADY\tAVAILABLE"); err != nil {
		return err
	}
	for _, item := range report.ReplicaSets {
		if writer.wide {
			if _, err := fmt.Fprintf(replicaSets, "%s\t%s\t%d\t%t\t%d\t%d\t%d\t%d\t%s\n", item.Name, item.UID, item.Revision, item.Current, item.Desired, item.Replicas, item.Ready, item.Available, optionalRelativeTime(report.CapturedAt, item.CreatedAt)); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(replicaSets, "%s\t%d\t%t\t%d\t%d\t%d\t%d\n", item.Name, item.Revision, item.Current, item.Desired, item.Replicas, item.Ready, item.Available); err != nil {
			return err
		}
	}
	if err := replicaSets.Flush(); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(out, "\nPODS"); err != nil {
		return err
	}
	pods := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if writer.wide {
		if _, err := fmt.Fprintln(pods, "POD\tUID\tREPLICASET\tNODE\tPHASE\tREADY\tRESTARTS\tREASONS\tAGE"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(pods, "POD\tREPLICASET\tPHASE\tREADY\tRESTARTS\tREASONS"); err != nil {
		return err
	}
	for _, item := range report.Pods {
		reasons := displayStrings(item.Reasons)
		if writer.wide {
			if _, err := fmt.Fprintf(pods, "%s\t%s\t%s\t%s\t%s\t%t\t%d\t%s\t%s\n", item.Name, item.UID, item.ReplicaSet, displayValue(item.Node), item.Phase, item.Ready, item.Restarts, reasons, optionalRelativeTime(report.CapturedAt, item.CreatedAt)); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(pods, "%s\t%s\t%s\t%t\t%d\t%s\n", item.Name, item.ReplicaSet, item.Phase, item.Ready, item.Restarts, reasons); err != nil {
			return err
		}
	}
	if err := pods.Flush(); err != nil {
		return err
	}

	if len(report.Findings) > 0 {
		if _, err := fmt.Fprintln(out, "\nFINDINGS"); err != nil {
			return err
		}
		findings := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(findings, "SEVERITY\tCODE\tOBJECT\tMESSAGE"); err != nil {
			return err
		}
		for _, item := range report.Findings {
			if _, err := fmt.Fprintf(findings, "%s\t%s\t%s\t%s\n", item.Severity, item.Code, item.Object, oneLine(item.Message)); err != nil {
				return err
			}
		}
		if err := findings.Flush(); err != nil {
			return err
		}
	}

	if len(report.Events) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\nRELATED EVENTS"); err != nil {
		return err
	}
	events := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(events, "TIME\tTYPE\tOBJECT\tREASON\tCOUNT\tSOURCE\tMESSAGE"); err != nil {
		return err
	}
	for _, item := range report.Events {
		if _, err := fmt.Fprintf(events, "%s\t%s\t%s/%s\t%s\t%d\t%s\t%s\n", relativeTime(report.CapturedAt, item.LastObservedAt), item.Type, item.RegardingKind, item.RegardingName, item.Reason, item.Count, displayValue(item.ReportingController), oneLine(item.Note)); err != nil {
			return err
		}
	}
	return events.Flush()
}

func displayStrings(values []string) string {
	if len(values) == 0 {
		return "None"
	}
	return strings.Join(values, ",")
}

func optionalRelativeTime(now, value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return relativeTime(now, value)
}
