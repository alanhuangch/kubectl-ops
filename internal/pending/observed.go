package pending

import (
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func scheduledCondition(item *corev1.Pod) *ObservedCondition {
	for _, condition := range item.Status.Conditions {
		if condition.Type != corev1.PodScheduled {
			continue
		}
		return &ObservedCondition{
			Status:             condition.Status,
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time,
		}
	}
	return nil
}

func observedSchedulingEvents(events []corev1.Event) []ObservedEvent {
	result := make([]ObservedEvent, 0, len(events))
	for _, event := range events {
		if event.Reason != "FailedScheduling" {
			continue
		}
		count := event.Count
		if event.Series != nil && event.Series.Count > count {
			count = event.Series.Count
		}
		result = append(result, ObservedEvent{
			Type:       event.Type,
			Reason:     event.Reason,
			Message:    event.Message,
			Count:      count,
			ObservedAt: eventObservedAt(&event),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].ObservedAt.Equal(result[j].ObservedAt) {
			return result[i].ObservedAt.After(result[j].ObservedAt)
		}
		if result[i].Reason != result[j].Reason {
			return result[i].Reason < result[j].Reason
		}
		return result[i].Message < result[j].Message
	})
	return result
}

func eventObservedAt(event *corev1.Event) time.Time {
	if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
		return event.Series.LastObservedTime.Time
	}
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	return event.CreationTimestamp.Time
}
