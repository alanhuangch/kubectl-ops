package output

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alanhuangch/kubectl-ops/internal/drain"
	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	nodeanalysis "github.com/alanhuangch/kubectl-ops/internal/node"
	"github.com/alanhuangch/kubectl-ops/internal/pending"
	"github.com/alanhuangch/kubectl-ops/internal/pod"
	"github.com/alanhuangch/kubectl-ops/internal/rollout"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestRecentWritersMatchGoldenFiles(t *testing.T) {
	report := pod.RecentReport{
		CapturedAt: time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC),
		Items: []pod.RecentPod{{
			Namespace:       "production",
			Name:            "api-123",
			UID:             "uid-1",
			Node:            "worker-07",
			Phase:           "Running",
			Scheduler:       "default-scheduler",
			CreatedAt:       time.Date(2026, time.July, 30, 9, 59, 45, 200_000_000, time.UTC),
			ScheduledAt:     time.Date(2026, time.July, 30, 9, 59, 48, 0, time.UTC),
			TimeToScheduled: 2800 * time.Millisecond,
		}},
		IgnoredMissingScheduled: 2,
		ExcludedStatic:          1,
	}

	tests := []struct {
		name   string
		format string
		golden string
	}{
		{name: "table", format: FormatTable, golden: "recent_table.golden"},
		{name: "json", format: FormatJSON, golden: "recent_json.golden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, err := NewWriter(test.format)
			if err != nil {
				t.Fatal(err)
			}
			var actual bytes.Buffer
			if err := writer.WriteRecent(&actual, report); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if actual.String() != string(expected) {
				t.Fatalf("output mismatch\n--- actual ---\n%s--- expected ---\n%s", actual.String(), expected)
			}
		})
	}
}

func TestNewWriterRejectsUnknownFormat(t *testing.T) {
	if _, err := NewWriter("yaml"); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestRestartWritersMatchGoldenFiles(t *testing.T) {
	report := pod.RestartReport{
		CapturedAt: time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC),
		Items: []pod.RestartedContainer{{
			Namespace:      "production",
			Pod:            "payment-123",
			UID:            "uid-1",
			Node:           "worker-07",
			Container:      "payment",
			Kind:           pod.ContainerKindRegular,
			RestartCount:   12,
			LastReason:     "OOMKilled",
			Classification: pod.RestartOOMKilled,
			ExitCode:       137,
			FinishedAt:     time.Date(2026, time.July, 30, 9, 57, 0, 0, time.UTC),
		}},
		IgnoredMissingFinishedAt: 2,
	}
	tests := []struct {
		name   string
		format string
		golden string
	}{
		{name: "table", format: FormatTable, golden: "restarts_table.golden"},
		{name: "json", format: FormatJSON, golden: "restarts_json.golden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, err := NewWriter(test.format)
			if err != nil {
				t.Fatal(err)
			}
			var actual bytes.Buffer
			if err := writer.WriteRestarts(&actual, report); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if actual.String() != string(expected) {
				t.Fatalf("output mismatch\n--- actual ---\n%s--- expected ---\n%s", actual.String(), expected)
			}
		})
	}
}

func TestNodeRequestWritersMatchGoldenFiles(t *testing.T) {
	cpuAllocatable := resource.MustParse("4")
	cpuRequested := resource.MustParse("1")
	cpuAvailable := resource.MustParse("3")
	podsAllocatable := resource.MustParse("10")
	podsRequested := resource.MustParse("2")
	podsAvailable := resource.MustParse("8")
	report := nodeanalysis.RequestsReport{
		CapturedAt:   time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC),
		Node:         "worker-07",
		Completeness: nodeanalysis.CompletenessComplete,
		Resources: []nodeanalysis.ResourceUsage{
			{Resource: corev1.ResourceCPU, Allocatable: cpuAllocatable, Requested: cpuRequested, Available: cpuAvailable, RequestsKnown: true, Ratio: 25, RatioKnown: true},
			{Resource: corev1.ResourcePods, Allocatable: podsAllocatable, Requested: podsRequested, Available: podsAvailable, RequestsKnown: true, Ratio: 20, RatioKnown: true},
		},
		TopResource: corev1.ResourceCPU,
		Consumers: []nodeanalysis.Consumer{{
			Namespace: "production",
			Pod:       "api",
			UID:       "uid-1",
			Request:   resource.MustParse("750m"),
			Owner:     "ReplicaSet/api-7d8f",
		}},
		Warnings: []string{},
	}
	tests := []struct {
		name   string
		format string
		golden string
	}{
		{name: "table", format: FormatTable, golden: "node_requests_table.golden"},
		{name: "json", format: FormatJSON, golden: "node_requests_json.golden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, err := NewWriter(test.format)
			if err != nil {
				t.Fatal(err)
			}
			var actual bytes.Buffer
			if err := writer.WriteNodeRequests(&actual, report); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if actual.String() != string(expected) {
				t.Fatalf("output mismatch\n--- actual ---\n%s--- expected ---\n%s", actual.String(), expected)
			}
		})
	}
}

func TestNodeDrainCheckWritersMatchGoldenFiles(t *testing.T) {
	report := drain.Report{
		CapturedAt:   time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC),
		Node:         "worker-07",
		Drainability: drain.DrainabilityBlocked,
		Completeness: drain.CompletenessComplete,
		Options: drain.AppliedOptions{
			IgnoreDaemonSets:   true,
			Force:              true,
			DeleteEmptyDirData: false,
		},
		Pods: []drain.PodImpact{
			{Namespace: "kube-system", Pod: "kube-proxy", UID: "uid-proxy", Owner: "DaemonSet/kube-proxy", Phase: "Running", Action: drain.PodActionSkip, Impacts: []drain.ImpactCode{drain.ImpactDaemonSetManaged}},
			{Namespace: "production", Pod: "api", UID: "uid-api", Owner: "ReplicaSet/api-rs", Phase: "Running", Action: drain.PodActionBlocked, Impacts: []drain.ImpactCode{drain.ImpactLocalStorage, drain.ImpactPDBViolation}, Blockers: []drain.ImpactCode{drain.ImpactLocalStorage, drain.ImpactPDBViolation}},
			{Namespace: "production", Pod: "completed", UID: "uid-completed", Owner: "Job/batch", Phase: "Failed", Action: drain.PodActionDelete, Impacts: []drain.ImpactCode{drain.ImpactTerminalPod}},
		},
		PDBs: []drain.PDBImpact{{
			Namespace: "production", Name: "api", MatchedTargets: 1, DisruptionsAllowed: 0, Blocked: true, SelectorValid: true,
		}},
		Summary:  drain.Summary{Pods: 3, Delete: 1, Skip: 1, Blocked: 1},
		Warnings: []string{},
	}
	tests := []struct {
		name   string
		format string
		golden string
	}{
		{name: "table", format: FormatTable, golden: "node_drain_check_table.golden"},
		{name: "json", format: FormatJSON, golden: "node_drain_check_json.golden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, err := NewWriter(test.format)
			if err != nil {
				t.Fatal(err)
			}
			var actual bytes.Buffer
			if err := writer.WriteNodeDrainCheck(&actual, report); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if actual.String() != string(expected) {
				t.Fatalf("output mismatch\n--- actual ---\n%s--- expected ---\n%s", actual.String(), expected)
			}
		})
	}
}

func TestRolloutExplainWritersMatchGoldenFiles(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	report := rollout.Report{
		CapturedAt: now, Status: rollout.StatusDegraded, Completeness: rollout.CompletenessComplete,
		Deployment: rollout.DeploymentInfo{
			Namespace: "production", Name: "api", UID: "deployment-uid", Generation: 2, ObservedGeneration: 2,
			Desired: 2, Current: 2, Updated: 2, Ready: 1, Available: 1, Unavailable: 1,
		},
		Conditions: []rollout.Condition{{
			Type: "Progressing", Status: "False", Reason: "ProgressDeadlineExceeded",
			Message: "ReplicaSet api-new has timed out progressing.", LastTransitionTime: now.Add(-time.Minute),
		}},
		ReplicaSets: []rollout.ReplicaSetInfo{{
			Namespace: "production", Name: "api-new", UID: "rs-new", Revision: 2, Current: true,
			Desired: 2, Replicas: 2, Ready: 1, Available: 1, CreatedAt: now.Add(-10 * time.Minute),
		}},
		Pods: []rollout.PodInfo{{
			Namespace: "production", Name: "api-new-a", UID: "pod-a", ReplicaSet: "api-new", Node: "worker-07",
			Phase: "Running", Ready: false, Restarts: 3, Reasons: []string{"CrashLoopBackOff"}, CreatedAt: now.Add(-9 * time.Minute),
		}},
		Findings: []rollout.Finding{{
			Severity: rollout.SeverityError, Code: "ContainerWaiting", Object: "Pod/api-new-a", Message: "CrashLoopBackOff",
		}},
		Events: []rollout.EventInfo{{
			Type: "Warning", Reason: "BackOff", RegardingKind: "Pod", RegardingName: "api-new-a",
			Note: "back-off restarting failed container", Count: 4, LastObservedAt: now.Add(-30 * time.Second), ReportingController: "kubelet",
		}},
		Warnings: []string{},
	}
	tests := []struct {
		name   string
		format string
		golden string
	}{
		{name: "table", format: FormatTable, golden: "rollout_explain_table.golden"},
		{name: "json", format: FormatJSON, golden: "rollout_explain_json.golden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, err := NewWriter(test.format)
			if err != nil {
				t.Fatal(err)
			}
			var actual bytes.Buffer
			if err := writer.WriteRolloutExplain(&actual, report); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if actual.String() != string(expected) {
				t.Fatalf("output mismatch\n--- actual ---\n%s--- expected ---\n%s", actual.String(), expected)
			}
		})
	}
}

func TestPendingWritersMatchGoldenFiles(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	failure := pending.Failure{
		Code: "InsufficientCPU", Category: "Resources", Source: pending.SourceCurrentState, Message: "insufficient cpu",
		Details: map[string]string{"requested": "2", "available": "1"},
	}
	node := pending.NodeResult{
		Node: "worker-07", Failures: []pending.Failure{failure}, HardFailureCount: 1, ResourceGapScore: 0.5,
		LimitingResource: "cpu", TopConsumers: []pending.Consumer{{Namespace: "production", Pod: "busy", Request: "3"}},
	}
	report := pending.Report{
		CapturedAt: now, Namespace: "production", Pod: "api", UID: "uid-1", Phase: corev1.PodPending,
		Scheduler: corev1.DefaultSchedulerName, Completeness: pending.CompletenessPartial,
		Condition:             &pending.ObservedCondition{Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "0/1 nodes are available", LastTransitionTime: now.Add(-time.Minute)},
		Events:                []pending.ObservedEvent{{Type: corev1.EventTypeWarning, Reason: "FailedScheduling", Message: "0/1 nodes are available", Count: 3, ObservedAt: now.Add(-30 * time.Second)}},
		CurrentStateAvailable: true,
		Summary:               []pending.FailureSummary{{Code: "InsufficientCPU", Category: "Resources", Count: 1, Nodes: []string{"worker-07"}}},
		Nodes:                 []pending.NodeResult{node},
		ClosestNodes:          []pending.NodeResult{node},
		Unsupported:           []pending.Unsupported{{Code: "Preemption", Message: "preemption is not evaluated"}},
		Warnings:              []string{},
	}
	tests := []struct {
		name   string
		format string
		golden string
	}{
		{name: "table", format: FormatTable, golden: "pending_table.golden"},
		{name: "json", format: FormatJSON, golden: "pending_json.golden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, err := NewWriter(test.format)
			if err != nil {
				t.Fatal(err)
			}
			var actual bytes.Buffer
			if err := writer.WritePending(&actual, report); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if actual.String() != string(expected) {
				t.Fatalf("output mismatch\n--- actual ---\n%s--- expected ---\n%s", actual.String(), expected)
			}
		})
	}
}

func TestEventsTimelineWritersMatchGoldenFiles(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	report := timeline.Report{
		CapturedAt:   now,
		APIVersion:   "events.k8s.io/v1",
		Completeness: "Complete",
		Items: []timeline.TimelineEvent{{
			Namespace:           "production",
			Name:                "api.backoff",
			UID:                 "event-uid",
			Type:                corev1.EventTypeWarning,
			Reason:              "BackOff",
			Action:              "BackOff",
			Note:                "back-off restarting failed container",
			ReportingController: "kubelet",
			ReportingInstance:   "worker-07",
			Regarding: timeline.ObjectReference{
				APIVersion: "v1", Kind: "Pod", Namespace: "production", Name: "api", UID: "pod-uid",
			},
			FirstObservedAt: now.Add(-10 * time.Minute),
			LastObservedAt:  now.Add(-time.Minute),
			Count:           4,
			Series:          true,
		}},
		IgnoredMissingTimestamp: 2,
	}
	tests := []struct {
		name   string
		format string
		golden string
	}{
		{name: "table", format: FormatTable, golden: "events_timeline_table.golden"},
		{name: "json", format: FormatJSON, golden: "events_timeline_json.golden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, err := NewWriter(test.format)
			if err != nil {
				t.Fatal(err)
			}
			var actual bytes.Buffer
			if err := writer.WriteEventsTimeline(&actual, report); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if actual.String() != string(expected) {
				t.Fatalf("output mismatch\n--- actual ---\n%s--- expected ---\n%s", actual.String(), expected)
			}
		})
	}
}
