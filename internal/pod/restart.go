package pod

import (
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
)

type RestartOptions struct {
	Now   time.Time
	Since time.Duration
	Node  string
}

func AnalyzeRestarts(pods []corev1.Pod, options RestartOptions) RestartReport {
	report := RestartReport{CapturedAt: options.Now}
	cutoff := options.Now.Add(-options.Since)

	for i := range pods {
		item := &pods[i]
		if options.Node != "" && item.Spec.NodeName != options.Node {
			continue
		}
		appendRestartStatuses(&report, item, item.Status.ContainerStatuses, ContainerKindRegular, cutoff, options.Now)
		appendRestartStatuses(&report, item, item.Status.InitContainerStatuses, ContainerKindInit, cutoff, options.Now)
		appendRestartStatuses(&report, item, item.Status.EphemeralContainerStatuses, ContainerKindEphemeral, cutoff, options.Now)
	}

	sort.Slice(report.Items, func(i, j int) bool {
		left, right := report.Items[i], report.Items[j]
		if !left.FinishedAt.Equal(right.FinishedAt) {
			return left.FinishedAt.After(right.FinishedAt)
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Pod != right.Pod {
			return left.Pod < right.Pod
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Container != right.Container {
			return left.Container < right.Container
		}
		return left.UID < right.UID
	})
	return report
}

func appendRestartStatuses(
	report *RestartReport,
	item *corev1.Pod,
	statuses []corev1.ContainerStatus,
	kind ContainerKind,
	cutoff time.Time,
	now time.Time,
) {
	for _, status := range statuses {
		if status.RestartCount <= 0 {
			continue
		}
		terminated := status.LastTerminationState.Terminated
		if terminated == nil || terminated.FinishedAt.IsZero() {
			report.IgnoredMissingFinishedAt++
			continue
		}
		finishedAt := terminated.FinishedAt.Time
		if finishedAt.Before(cutoff) || finishedAt.After(now) {
			continue
		}

		reason := terminated.Reason
		if reason == "" {
			reason = string(classifyRestart(terminated))
		}
		report.Items = append(report.Items, RestartedContainer{
			Namespace:      item.Namespace,
			Pod:            item.Name,
			UID:            string(item.UID),
			Node:           item.Spec.NodeName,
			Container:      status.Name,
			Kind:           kind,
			RestartCount:   status.RestartCount,
			LastReason:     reason,
			ExitCode:       terminated.ExitCode,
			Signal:         terminated.Signal,
			FinishedAt:     finishedAt,
			Classification: classifyRestart(terminated),
		})
	}
}

func classifyRestart(terminated *corev1.ContainerStateTerminated) RestartClassification {
	if terminated.Reason == "OOMKilled" {
		return RestartOOMKilled
	}
	if terminated.Signal == 9 || terminated.ExitCode == 128+9 {
		return RestartSIGKILL
	}
	if terminated.Signal == 15 || terminated.ExitCode == 128+15 {
		return RestartSIGTERM
	}
	if terminated.ExitCode == 0 {
		if terminated.Reason == "" || terminated.Reason == "Completed" {
			return RestartCompleted
		}
		return RestartError
	}
	if terminated.ExitCode != 0 || terminated.Reason != "" {
		return RestartError
	}
	return RestartUnknown
}
