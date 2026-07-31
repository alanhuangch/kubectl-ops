package events

import (
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
)

func AnalyzeV1(items []eventsv1.Event, options Options) Report {
	events := make([]TimelineEvent, 0, len(items))
	for i := range items {
		events = append(events, normalizeV1(&items[i]))
	}
	return analyze(events, options, "events.k8s.io/v1")
}

func AnalyzeCore(items []corev1.Event, options Options) Report {
	events := make([]TimelineEvent, 0, len(items))
	for i := range items {
		events = append(events, normalizeCore(&items[i]))
	}
	return analyze(events, options, "v1")
}

func analyze(items []TimelineEvent, options Options, apiVersion string) Report {
	report := Report{
		CapturedAt:   options.Now,
		APIVersion:   apiVersion,
		Completeness: "Complete",
	}
	cutoff := options.Now.Add(-options.Since)
	for _, item := range items {
		if item.LastObservedAt.IsZero() {
			report.IgnoredMissingTimestamp++
			continue
		}
		if item.LastObservedAt.Before(cutoff) || item.LastObservedAt.After(options.Now) {
			continue
		}
		if !matches(item, options) {
			continue
		}
		report.Items = append(report.Items, item)
	}
	sort.Slice(report.Items, func(i, j int) bool {
		left, right := report.Items[i], report.Items[j]
		if !left.LastObservedAt.Equal(right.LastObservedAt) {
			if options.Reverse {
				return left.LastObservedAt.After(right.LastObservedAt)
			}
			return left.LastObservedAt.Before(right.LastObservedAt)
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Regarding.Kind != right.Regarding.Kind {
			return left.Regarding.Kind < right.Regarding.Kind
		}
		if left.Regarding.Name != right.Regarding.Name {
			return left.Regarding.Name < right.Regarding.Name
		}
		if left.Reason != right.Reason {
			return left.Reason < right.Reason
		}
		return left.UID < right.UID
	})
	if options.Limit > 0 && len(report.Items) > options.Limit {
		if options.Reverse {
			report.Items = report.Items[:options.Limit]
		} else {
			report.Items = report.Items[len(report.Items)-options.Limit:]
		}
	}
	return report
}

func matches(item TimelineEvent, options Options) bool {
	if options.Kind != "" && !strings.EqualFold(item.Regarding.Kind, options.Kind) {
		return false
	}
	if options.Name != "" && item.Regarding.Name != options.Name {
		return false
	}
	if options.UID != "" && item.Regarding.UID != options.UID {
		return false
	}
	if options.Reason != "" && item.Reason != options.Reason {
		return false
	}
	if options.Type != "" && !strings.EqualFold(item.Type, options.Type) {
		return false
	}
	if options.ReportingController != "" && item.ReportingController != options.ReportingController {
		return false
	}
	return true
}

func normalizeV1(item *eventsv1.Event) TimelineEvent {
	count := item.DeprecatedCount
	if count <= 0 {
		count = 1
	}
	first := item.EventTime.Time
	if first.IsZero() {
		first = item.DeprecatedFirstTimestamp.Time
	}
	if first.IsZero() {
		first = item.CreationTimestamp.Time
	}
	last := item.EventTime.Time
	series := item.Series != nil
	if item.Series != nil {
		count = item.Series.Count
		last = item.Series.LastObservedTime.Time
	} else if !item.DeprecatedLastTimestamp.IsZero() {
		last = item.DeprecatedLastTimestamp.Time
	}
	if last.IsZero() {
		last = item.CreationTimestamp.Time
	}
	if count <= 0 {
		count = 1
	}
	return TimelineEvent{
		Namespace:           item.Namespace,
		Name:                item.Name,
		UID:                 string(item.UID),
		Type:                item.Type,
		Reason:              item.Reason,
		Action:              item.Action,
		Note:                item.Note,
		ReportingController: reportingController(item.ReportingController, item.DeprecatedSource.Component),
		ReportingInstance:   reportingController(item.ReportingInstance, item.DeprecatedSource.Host),
		Regarding:           reference(item.Regarding),
		Related:             optionalReference(item.Related),
		FirstObservedAt:     first,
		LastObservedAt:      last,
		Count:               count,
		Series:              series,
	}
}

func normalizeCore(item *corev1.Event) TimelineEvent {
	count := item.Count
	if count <= 0 {
		count = 1
	}
	first := item.FirstTimestamp.Time
	if first.IsZero() {
		first = item.EventTime.Time
	}
	if first.IsZero() {
		first = item.CreationTimestamp.Time
	}
	last := item.LastTimestamp.Time
	series := item.Series != nil
	if item.Series != nil {
		count = item.Series.Count
		last = item.Series.LastObservedTime.Time
	}
	if count <= 0 {
		count = 1
	}
	if last.IsZero() {
		last = item.EventTime.Time
	}
	if last.IsZero() {
		last = item.CreationTimestamp.Time
	}
	return TimelineEvent{
		Namespace:           item.Namespace,
		Name:                item.Name,
		UID:                 string(item.UID),
		Type:                item.Type,
		Reason:              item.Reason,
		Action:              item.Action,
		Note:                item.Message,
		ReportingController: reportingController(item.ReportingController, item.Source.Component),
		ReportingInstance:   reportingController(item.ReportingInstance, item.Source.Host),
		Regarding:           reference(item.InvolvedObject),
		Related:             optionalReference(item.Related),
		FirstObservedAt:     first,
		LastObservedAt:      last,
		Count:               count,
		Series:              series,
	}
}

func reportingController(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

func reference(item corev1.ObjectReference) ObjectReference {
	return ObjectReference{
		APIVersion: item.APIVersion,
		Kind:       item.Kind,
		Namespace:  item.Namespace,
		Name:       item.Name,
		UID:        string(item.UID),
	}
}

func optionalReference(item *corev1.ObjectReference) *ObjectReference {
	if item == nil {
		return nil
	}
	result := reference(*item)
	return &result
}
