package rollout

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const deploymentRevisionAnnotation = "deployment.kubernetes.io/revision"

func Analyze(snapshot Snapshot, options Options) Report {
	deployment := snapshot.Deployment
	report := Report{
		CapturedAt:   snapshot.CapturedAt,
		Status:       StatusProgressing,
		Completeness: CompletenessComplete,
		Warnings:     append([]string(nil), snapshot.Warnings...),
		Deployment: DeploymentInfo{
			Namespace:          deployment.Namespace,
			Name:               deployment.Name,
			UID:                string(deployment.UID),
			Generation:         deployment.Generation,
			ObservedGeneration: deployment.Status.ObservedGeneration,
			Desired:            desiredReplicas(deployment),
			Current:            deployment.Status.Replicas,
			Updated:            deployment.Status.UpdatedReplicas,
			Ready:              deployment.Status.ReadyReplicas,
			Available:          deployment.Status.AvailableReplicas,
			Unavailable:        deployment.Status.UnavailableReplicas,
		},
	}
	if !snapshot.ReplicaSetsKnown || !snapshot.PodsKnown || !snapshot.EventsKnown {
		report.Completeness = CompletenessPartial
	}

	analyzeDeploymentConditions(&report, deployment)
	replicaSetUIDs := analyzeReplicaSets(&report, deployment, snapshot.ReplicaSets, snapshot.ReplicaSetsKnown)
	podUIDs := analyzePods(&report, snapshot.Pods, snapshot.PodsKnown, snapshot.ReplicaSetsKnown, replicaSetUIDs)
	analyzeEvents(&report, snapshot.Events, snapshot.EventsKnown, deployment, replicaSetUIDs, podUIDs, options.EventLimit)
	finalizeStatus(&report)
	sortFindings(report.Findings)
	return report
}

func analyzeDeploymentConditions(report *Report, deployment *appsv1.Deployment) {
	conditions := append([]appsv1.DeploymentCondition(nil), deployment.Status.Conditions...)
	sort.Slice(conditions, func(i, j int) bool {
		return conditions[i].Type < conditions[j].Type
	})
	for _, condition := range conditions {
		report.Conditions = append(report.Conditions, Condition{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time,
		})
		switch {
		case condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionFalse && condition.Reason == "ProgressDeadlineExceeded":
			addFinding(report, SeverityError, "ProgressDeadlineExceeded", deploymentObject(deployment), condition.Message)
		case condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue:
			addFinding(report, SeverityError, "ReplicaFailure", deploymentObject(deployment), condition.Message)
		}
	}
	if deployment.Status.ObservedGeneration < deployment.Generation {
		addFinding(report, SeverityWarning, "ObservedGenerationLag", deploymentObject(deployment), fmt.Sprintf("controller observed generation %d, current generation is %d", deployment.Status.ObservedGeneration, deployment.Generation))
	}
	if deployment.Status.UnavailableReplicas > 0 {
		addFinding(report, SeverityWarning, "UnavailableReplicas", deploymentObject(deployment), fmt.Sprintf("%d replica(s) are unavailable", deployment.Status.UnavailableReplicas))
	}
}

func analyzeReplicaSets(report *Report, deployment *appsv1.Deployment, items []appsv1.ReplicaSet, known bool) map[types.UID]string {
	result := map[types.UID]string{}
	if !known {
		return result
	}
	replicaSets := make([]ReplicaSetInfo, 0, len(items))
	for index := range items {
		item := &items[index]
		owner := metav1.GetControllerOf(item)
		if owner == nil || owner.UID != deployment.UID {
			continue
		}
		revision := revisionOf(item)
		result[item.UID] = item.Name
		replicaSets = append(replicaSets, ReplicaSetInfo{
			Namespace: item.Namespace,
			Name:      item.Name,
			UID:       string(item.UID),
			Revision:  revision,
			Desired:   desiredReplicaSetReplicas(item),
			Replicas:  item.Status.Replicas,
			Ready:     item.Status.ReadyReplicas,
			Available: item.Status.AvailableReplicas,
			CreatedAt: item.CreationTimestamp.Time,
		})
	}
	sort.Slice(replicaSets, func(i, j int) bool {
		if replicaSets[i].Revision != replicaSets[j].Revision {
			return replicaSets[i].Revision > replicaSets[j].Revision
		}
		if !replicaSets[i].CreatedAt.Equal(replicaSets[j].CreatedAt) {
			return replicaSets[i].CreatedAt.After(replicaSets[j].CreatedAt)
		}
		return replicaSets[i].Name < replicaSets[j].Name
	})
	if len(replicaSets) > 0 {
		replicaSets[0].Current = true
	}
	for _, item := range replicaSets {
		report.ReplicaSets = append(report.ReplicaSets, item)
		if item.Desired > 0 && item.Ready < item.Desired {
			addFinding(report, SeverityWarning, "ReplicaSetNotReady", "ReplicaSet/"+item.Name, fmt.Sprintf("%d of %d desired replica(s) are ready", item.Ready, item.Desired))
		}
	}
	return result
}

func analyzePods(report *Report, items []corev1.Pod, known, replicaSetsKnown bool, replicaSetUIDs map[types.UID]string) map[types.UID]string {
	result := map[types.UID]string{}
	if !known {
		return result
	}
	pods := make([]PodInfo, 0, len(items))
	for index := range items {
		item := &items[index]
		owner := metav1.GetControllerOf(item)
		if owner == nil || owner.Kind != "ReplicaSet" {
			continue
		}
		replicaSetName, found := replicaSetUIDs[owner.UID]
		if replicaSetsKnown && !found {
			continue
		}
		if !found {
			replicaSetName = owner.Name
		}
		reasons := podReasons(item)
		info := PodInfo{
			Namespace:  item.Namespace,
			Name:       item.Name,
			UID:        string(item.UID),
			ReplicaSet: replicaSetName,
			Node:       item.Spec.NodeName,
			Phase:      string(item.Status.Phase),
			Ready:      podReady(item),
			Restarts:   podRestarts(item),
			Reasons:    reasons,
			CreatedAt:  item.CreationTimestamp.Time,
		}
		pods = append(pods, info)
		result[item.UID] = item.Name
		switch item.Status.Phase {
		case corev1.PodFailed:
			addFinding(report, SeverityError, "PodFailed", "Pod/"+item.Name, displayPodMessage(item))
		case corev1.PodPending:
			addFinding(report, SeverityWarning, "PodPending", "Pod/"+item.Name, displayPodMessage(item))
		case corev1.PodRunning:
			if !info.Ready {
				addFinding(report, SeverityWarning, "PodNotReady", "Pod/"+item.Name, "Pod is Running but not Ready")
			}
		}
		for _, reason := range reasons {
			severity := SeverityWarning
			if criticalWaitingReason(reason) {
				severity = SeverityError
			}
			addFinding(report, severity, "ContainerWaiting", "Pod/"+item.Name, reason)
		}
		if info.Restarts > 0 {
			addFinding(report, SeverityWarning, "ContainerRestarts", "Pod/"+item.Name, fmt.Sprintf("containers have restarted %d time(s)", info.Restarts))
		}
	}
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].ReplicaSet != pods[j].ReplicaSet {
			return pods[i].ReplicaSet < pods[j].ReplicaSet
		}
		if !pods[i].CreatedAt.Equal(pods[j].CreatedAt) {
			return pods[i].CreatedAt.Before(pods[j].CreatedAt)
		}
		return pods[i].Name < pods[j].Name
	})
	report.Pods = pods
	return result
}

func analyzeEvents(report *Report, items []timeline.TimelineEvent, known bool, deployment *appsv1.Deployment, replicaSetUIDs, podUIDs map[types.UID]string, limit int) {
	if !known {
		return
	}
	knownUIDs := map[string]bool{string(deployment.UID): true}
	for uid := range replicaSetUIDs {
		knownUIDs[string(uid)] = true
	}
	for uid := range podUIDs {
		knownUIDs[string(uid)] = true
	}
	for _, item := range items {
		if !knownUIDs[item.Regarding.UID] {
			continue
		}
		report.Events = append(report.Events, EventInfo{
			Type:                item.Type,
			Reason:              item.Reason,
			RegardingKind:       item.Regarding.Kind,
			RegardingName:       item.Regarding.Name,
			Note:                item.Note,
			Count:               item.Count,
			LastObservedAt:      item.LastObservedAt,
			ReportingController: item.ReportingController,
		})
	}
	sort.Slice(report.Events, func(i, j int) bool {
		if !report.Events[i].LastObservedAt.Equal(report.Events[j].LastObservedAt) {
			return report.Events[i].LastObservedAt.After(report.Events[j].LastObservedAt)
		}
		if report.Events[i].RegardingKind != report.Events[j].RegardingKind {
			return report.Events[i].RegardingKind < report.Events[j].RegardingKind
		}
		if report.Events[i].RegardingName != report.Events[j].RegardingName {
			return report.Events[i].RegardingName < report.Events[j].RegardingName
		}
		return report.Events[i].Reason < report.Events[j].Reason
	})
	if limit > 0 && len(report.Events) > limit {
		report.Events = report.Events[:limit]
	}
}

func finalizeStatus(report *Report) {
	for _, finding := range report.Findings {
		if finding.Severity == SeverityError {
			report.Status = StatusDegraded
			return
		}
	}
	if report.Completeness == CompletenessPartial {
		report.Status = StatusUnknown
		return
	}
	deployment := report.Deployment
	if deployment.ObservedGeneration >= deployment.Generation &&
		deployment.Updated >= deployment.Desired &&
		deployment.Available >= deployment.Desired &&
		deployment.Unavailable == 0 {
		report.Status = StatusHealthy
		return
	}
	report.Status = StatusProgressing
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		leftRank, rightRank := severityRank(findings[i].Severity), severityRank(findings[j].Severity)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		if findings[i].Object != findings[j].Object {
			return findings[i].Object < findings[j].Object
		}
		return findings[i].Message < findings[j].Message
	})
}

func severityRank(severity Severity) int {
	if severity == SeverityError {
		return 0
	}
	return 1
}

func addFinding(report *Report, severity Severity, code, object, message string) {
	if strings.TrimSpace(message) == "" {
		message = code
	}
	report.Findings = append(report.Findings, Finding{Severity: severity, Code: code, Object: object, Message: strings.Join(strings.Fields(message), " ")})
}

func desiredReplicas(item *appsv1.Deployment) int32 {
	if item.Spec.Replicas == nil {
		return 1
	}
	return *item.Spec.Replicas
}

func desiredReplicaSetReplicas(item *appsv1.ReplicaSet) int32 {
	if item.Spec.Replicas == nil {
		return 1
	}
	return *item.Spec.Replicas
}

func revisionOf(item *appsv1.ReplicaSet) int64 {
	value, err := strconv.ParseInt(item.Annotations[deploymentRevisionAnnotation], 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func podReady(item *corev1.Pod) bool {
	for _, condition := range item.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func podRestarts(item *corev1.Pod) int32 {
	var total int32
	for _, status := range item.Status.InitContainerStatuses {
		total += status.RestartCount
	}
	for _, status := range item.Status.ContainerStatuses {
		total += status.RestartCount
	}
	for _, status := range item.Status.EphemeralContainerStatuses {
		total += status.RestartCount
	}
	return total
}

func podReasons(item *corev1.Pod) []string {
	seen := map[string]bool{}
	var result []string
	appendStatuses := func(statuses []corev1.ContainerStatus) {
		for _, status := range statuses {
			if status.State.Waiting == nil || status.State.Waiting.Reason == "" || seen[status.State.Waiting.Reason] {
				continue
			}
			seen[status.State.Waiting.Reason] = true
			result = append(result, status.State.Waiting.Reason)
		}
	}
	appendStatuses(item.Status.InitContainerStatuses)
	appendStatuses(item.Status.ContainerStatuses)
	appendStatuses(item.Status.EphemeralContainerStatuses)
	sort.Strings(result)
	return result
}

func criticalWaitingReason(reason string) bool {
	switch reason {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
		return true
	default:
		return false
	}
}

func displayPodMessage(item *corev1.Pod) string {
	if item.Status.Message != "" {
		return item.Status.Message
	}
	if item.Status.Reason != "" {
		return item.Status.Reason
	}
	return string(item.Status.Phase)
}

func deploymentObject(item *appsv1.Deployment) string {
	return "Deployment/" + item.Name
}
