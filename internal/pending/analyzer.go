package pending

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
)

func Analyze(snapshot Snapshot, options Options) Report {
	report := Report{
		CapturedAt:            snapshot.CapturedAt,
		Namespace:             snapshot.TargetPod.Namespace,
		Pod:                   snapshot.TargetPod.Name,
		UID:                   string(snapshot.TargetPod.UID),
		Phase:                 snapshot.TargetPod.Status.Phase,
		Scheduler:             snapshot.TargetPod.Spec.SchedulerName,
		Completeness:          CompletenessComplete,
		ShowAllNodes:          options.AllNodes,
		CurrentStateAvailable: snapshot.NodesKnown,
		Warnings:              append([]string(nil), snapshot.Warnings...),
	}
	report.Condition = scheduledCondition(snapshot.TargetPod)
	report.Events = observedSchedulingEvents(snapshot.Events)
	report.Unsupported = detectUnsupported(snapshot.TargetPod)
	if !snapshot.NodesKnown || !snapshot.PodsKnown || !snapshot.EventsKnown || len(report.Unsupported) > 0 || len(report.Warnings) > 0 {
		report.Completeness = CompletenessPartial
	}

	if snapshot.NodesKnown {
		nodes := append([]corev1.Node(nil), snapshot.Nodes...)
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		for i := range nodes {
			report.Nodes = append(report.Nodes, analyzeNode(snapshot, &nodes[i], options.TopConsumers))
		}
	}
	report.Summary = summarizeFailures(report.Nodes)
	report.ClosestNodes = append([]NodeResult(nil), report.Nodes...)
	sort.Slice(report.ClosestNodes, func(i, j int) bool {
		left, right := report.ClosestNodes[i], report.ClosestNodes[j]
		if left.HardFailureCount != right.HardFailureCount {
			return left.HardFailureCount < right.HardFailureCount
		}
		if left.NonResourceFailures != right.NonResourceFailures {
			return left.NonResourceFailures < right.NonResourceFailures
		}
		if left.ResourceGapScore != right.ResourceGapScore {
			return left.ResourceGapScore < right.ResourceGapScore
		}
		return left.Node < right.Node
	})
	if options.Closest > 0 && len(report.ClosestNodes) > options.Closest {
		report.ClosestNodes = report.ClosestNodes[:options.Closest]
	}
	return report
}

func analyzeNode(snapshot Snapshot, node *corev1.Node, topConsumers int) NodeResult {
	result := NodeResult{Node: node.Name}
	result.Failures = append(result.Failures, placementFailures(snapshot.TargetPod, node)...)
	result.Failures = append(result.Failures, taintFailures(snapshot.TargetPod, node)...)
	if snapshot.PodsKnown {
		resourceFailures, gap, limitingResource, consumers := analyzeResources(
			snapshot.TargetPod,
			node,
			snapshot.PodsByNode[node.Name],
			topConsumers,
		)
		result.Failures = append(result.Failures, resourceFailures...)
		result.ResourceGapScore = gap
		result.LimitingResource = limitingResource
		result.TopConsumers = consumers
		result.Failures = append(result.Failures, hostPortFailures(snapshot.TargetPod, snapshot.PodsByNode[node.Name])...)
	}
	sort.Slice(result.Failures, func(i, j int) bool {
		left, right := result.Failures[i], result.Failures[j]
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})
	result.HardFailureCount = len(result.Failures)
	for _, failure := range result.Failures {
		if failure.Category != "Resources" {
			result.NonResourceFailures++
		}
	}
	return result
}

func summarizeFailures(nodes []NodeResult) []FailureSummary {
	type key struct{ code, category string }
	groups := map[key][]string{}
	for _, node := range nodes {
		seen := map[key]bool{}
		for _, failure := range node.Failures {
			item := key{code: failure.Code, category: failure.Category}
			if seen[item] {
				continue
			}
			seen[item] = true
			groups[item] = append(groups[item], node.Node)
		}
	}
	result := make([]FailureSummary, 0, len(groups))
	for item, nodes := range groups {
		sort.Strings(nodes)
		result = append(result, FailureSummary{Code: item.code, Category: item.category, Count: len(nodes), Nodes: nodes})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].Code < result[j].Code
	})
	return result
}

func IndexPodsByNode(pods []corev1.Pod) map[string][]corev1.Pod {
	result := map[string][]corev1.Pod{}
	for i := range pods {
		if pods[i].Spec.NodeName == "" {
			continue
		}
		result[pods[i].Spec.NodeName] = append(result[pods[i].Spec.NodeName], pods[i])
	}
	return result
}
