package pod

import (
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const mirrorPodAnnotationKey = "kubernetes.io/config.mirror"

type RecentOptions struct {
	Now           time.Time
	Since         time.Duration
	Limit         int
	Node          string
	Phase         corev1.PodPhase
	Scheduler     string
	IncludeStatic bool
}

func AnalyzeRecent(pods []corev1.Pod, options RecentOptions) RecentReport {
	report := RecentReport{CapturedAt: options.Now}
	cutoff := options.Now.Add(-options.Since)

	for i := range pods {
		item := &pods[i]
		if options.Node != "" && item.Spec.NodeName != options.Node {
			continue
		}
		if options.Phase != "" && item.Status.Phase != options.Phase {
			continue
		}
		if options.Scheduler != "" && item.Spec.SchedulerName != options.Scheduler {
			continue
		}

		static := isStaticPod(item)
		if static && !options.IncludeStatic {
			report.ExcludedStatic++
			continue
		}

		scheduledAt, ok := scheduledTime(item)
		if !ok {
			report.IgnoredMissingScheduled++
			continue
		}
		if scheduledAt.Before(cutoff) || scheduledAt.After(options.Now) {
			continue
		}

		createdAt := item.CreationTimestamp.Time
		report.Items = append(report.Items, RecentPod{
			Namespace:       item.Namespace,
			Name:            item.Name,
			UID:             string(item.UID),
			Node:            item.Spec.NodeName,
			Phase:           string(item.Status.Phase),
			Scheduler:       item.Spec.SchedulerName,
			CreatedAt:       createdAt,
			ScheduledAt:     scheduledAt,
			TimeToScheduled: scheduledAt.Sub(createdAt),
			Static:          static,
		})
	}

	sort.Slice(report.Items, func(i, j int) bool {
		left, right := report.Items[i], report.Items[j]
		if !left.ScheduledAt.Equal(right.ScheduledAt) {
			return left.ScheduledAt.After(right.ScheduledAt)
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.UID < right.UID
	})

	if options.Limit > 0 && len(report.Items) > options.Limit {
		report.Items = report.Items[:options.Limit]
	}
	return report
}

func scheduledTime(item *corev1.Pod) (time.Time, bool) {
	for _, condition := range item.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionTrue && !condition.LastTransitionTime.IsZero() {
			return condition.LastTransitionTime.Time, true
		}
	}
	return time.Time{}, false
}

func isStaticPod(item *corev1.Pod) bool {
	_, ok := item.Annotations[mirrorPodAnnotationKey]
	return ok
}
