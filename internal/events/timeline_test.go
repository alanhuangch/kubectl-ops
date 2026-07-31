package events

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAnalyzeV1NormalizesSeriesAndFilters(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	items := []eventsv1.Event{
		eventV1("older", "Pod", "api", "pod-uid", "Scheduled", corev1.EventTypeNormal, now.Add(-20*time.Minute), now.Add(-10*time.Minute), 4),
		eventV1("newer", "Pod", "api", "pod-uid", "FailedScheduling", corev1.EventTypeWarning, now.Add(-5*time.Minute), now.Add(-time.Minute), 3),
		eventV1("other", "Node", "worker-07", "node-uid", "Ready", corev1.EventTypeNormal, now.Add(-2*time.Minute), now.Add(-2*time.Minute), 1),
	}

	report := AnalyzeV1(items, Options{
		Now: now, Since: time.Hour, Limit: 100, Kind: "pod", Name: "api", UID: "pod-uid",
	})
	if report.APIVersion != "events.k8s.io/v1" || len(report.Items) != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Items[0].Name != "older" || report.Items[1].Name != "newer" {
		t.Fatalf("unexpected order: %#v", report.Items)
	}
	if report.Items[1].Count != 3 || !report.Items[1].Series || !report.Items[1].LastObservedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("series was not normalized: %#v", report.Items[1])
	}
}

func TestAnalyzeKeepsLatestLimitInChronologicalOrder(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	items := []eventsv1.Event{
		eventV1("one", "Pod", "api", "uid", "One", corev1.EventTypeNormal, now.Add(-3*time.Minute), now.Add(-3*time.Minute), 1),
		eventV1("two", "Pod", "api", "uid", "Two", corev1.EventTypeNormal, now.Add(-2*time.Minute), now.Add(-2*time.Minute), 1),
		eventV1("three", "Pod", "api", "uid", "Three", corev1.EventTypeNormal, now.Add(-time.Minute), now.Add(-time.Minute), 1),
	}
	report := AnalyzeV1(items, Options{Now: now, Since: time.Hour, Limit: 2})
	if len(report.Items) != 2 || report.Items[0].Name != "two" || report.Items[1].Name != "three" {
		t.Fatalf("unexpected limited timeline: %#v", report.Items)
	}
	reverse := AnalyzeV1(items, Options{Now: now, Since: time.Hour, Limit: 2, Reverse: true})
	if reverse.Items[0].Name != "three" || reverse.Items[1].Name != "two" {
		t.Fatalf("unexpected reverse timeline: %#v", reverse.Items)
	}
}

func TestAnalyzeCoreUsesDeprecatedAndSeriesFields(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	item := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default", Name: "event", UID: types.UID("event")},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api", Namespace: "default", UID: types.UID("pod")},
		Reason:         "BackOff",
		Message:        "back-off restarting failed container",
		Type:           corev1.EventTypeWarning,
		FirstTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
		Series:         &corev1.EventSeries{Count: 7, LastObservedTime: metav1.NewMicroTime(now.Add(-time.Minute))},
		Source:         corev1.EventSource{Component: "kubelet"},
	}
	report := AnalyzeCore([]corev1.Event{item}, Options{Now: now, Since: time.Hour, Limit: 100, Type: "warning"})
	if report.APIVersion != "v1" || len(report.Items) != 1 || report.Items[0].Count != 7 || report.Items[0].ReportingController != "kubelet" {
		t.Fatalf("unexpected core report: %#v", report)
	}
}

func TestAnalyzeIgnoresEventsWithoutTimestamps(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	report := AnalyzeV1([]eventsv1.Event{{ObjectMeta: metav1.ObjectMeta{Name: "missing"}}}, Options{Now: now, Since: time.Hour, Limit: 100})
	if report.IgnoredMissingTimestamp != 1 || len(report.Items) != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func eventV1(name, kind, objectName, objectUID, reason, eventType string, first, last time.Time, count int32) eventsv1.Event {
	return eventsv1.Event{
		ObjectMeta:          metav1.ObjectMeta{Namespace: "default", Name: name, UID: types.UID(name)},
		EventTime:           metav1.NewMicroTime(first),
		Series:              &eventsv1.EventSeries{Count: count, LastObservedTime: metav1.NewMicroTime(last)},
		ReportingController: "test-controller",
		Action:              "Testing",
		Reason:              reason,
		Regarding:           corev1.ObjectReference{Kind: kind, Namespace: "default", Name: objectName, UID: types.UID(objectUID)},
		Note:                reason + " note",
		Type:                eventType,
	}
}
