package output

import (
	"encoding/json"
	"io"
	"time"

	"github.com/alanhuangch/kubectl-ops/internal/drain"
	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	nodeanalysis "github.com/alanhuangch/kubectl-ops/internal/node"
	"github.com/alanhuangch/kubectl-ops/internal/pending"
	"github.com/alanhuangch/kubectl-ops/internal/pod"
	"github.com/alanhuangch/kubectl-ops/internal/rollout"
	workloadanalysis "github.com/alanhuangch/kubectl-ops/internal/workload"
)

type jsonWriter struct{}

type recentJSONReport struct {
	CapturedAt   string            `json:"capturedAt"`
	Completeness string            `json:"completeness"`
	Items        []recentJSONItem  `json:"items"`
	Summary      recentJSONSummary `json:"summary"`
}

type recentJSONItem struct {
	Namespace       string `json:"namespace"`
	Pod             string `json:"pod"`
	UID             string `json:"uid"`
	Node            string `json:"node"`
	ScheduledAt     string `json:"scheduledAt"`
	TimeToScheduled string `json:"timeToScheduled"`
	Phase           string `json:"phase"`
	Scheduler       string `json:"scheduler"`
	Static          bool   `json:"static"`
}

type recentJSONSummary struct {
	Returned                int `json:"returned"`
	IgnoredMissingScheduled int `json:"ignoredMissingScheduled"`
	ExcludedStatic          int `json:"excludedStatic"`
}

func (jsonWriter) WriteRecent(out io.Writer, report pod.RecentReport) error {
	items := make([]recentJSONItem, 0, len(report.Items))
	for _, item := range report.Items {
		items = append(items, recentJSONItem{
			Namespace:       item.Namespace,
			Pod:             item.Name,
			UID:             item.UID,
			Node:            item.Node,
			ScheduledAt:     item.ScheduledAt.UTC().Format(time.RFC3339Nano),
			TimeToScheduled: item.TimeToScheduled.String(),
			Phase:           item.Phase,
			Scheduler:       item.Scheduler,
			Static:          item.Static,
		})
	}

	payload := recentJSONReport{
		CapturedAt:   report.CapturedAt.UTC().Format(time.RFC3339Nano),
		Completeness: "Complete",
		Items:        items,
		Summary: recentJSONSummary{
			Returned:                len(items),
			IgnoredMissingScheduled: report.IgnoredMissingScheduled,
			ExcludedStatic:          report.ExcludedStatic,
		},
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

type restartJSONReport struct {
	CapturedAt   string             `json:"capturedAt"`
	Completeness string             `json:"completeness"`
	Items        []restartJSONItem  `json:"items"`
	Summary      restartJSONSummary `json:"summary"`
	Limitations  []string           `json:"limitations"`
}

type restartJSONItem struct {
	Namespace      string `json:"namespace"`
	Pod            string `json:"pod"`
	UID            string `json:"uid"`
	Node           string `json:"node"`
	Container      string `json:"container"`
	Type           string `json:"type"`
	Restarts       int32  `json:"restarts"`
	LastReason     string `json:"lastReason"`
	Classification string `json:"classification"`
	ExitCode       int32  `json:"exitCode"`
	Signal         int32  `json:"signal"`
	FinishedAt     string `json:"finishedAt"`
}

type restartJSONSummary struct {
	Returned                 int `json:"returned"`
	IgnoredMissingFinishedAt int `json:"ignoredMissingFinishedAt"`
}

func (jsonWriter) WriteRestarts(out io.Writer, report pod.RestartReport) error {
	items := make([]restartJSONItem, 0, len(report.Items))
	for _, item := range report.Items {
		items = append(items, restartJSONItem{
			Namespace:      item.Namespace,
			Pod:            item.Pod,
			UID:            item.UID,
			Node:           item.Node,
			Container:      item.Container,
			Type:           string(item.Kind),
			Restarts:       item.RestartCount,
			LastReason:     item.LastReason,
			Classification: string(item.Classification),
			ExitCode:       item.ExitCode,
			Signal:         item.Signal,
			FinishedAt:     item.FinishedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	payload := restartJSONReport{
		CapturedAt:   report.CapturedAt.UTC().Format(time.RFC3339Nano),
		Completeness: "Complete",
		Items:        items,
		Summary: restartJSONSummary{
			Returned:                 len(items),
			IgnoredMissingFinishedAt: report.IgnoredMissingFinishedAt,
		},
		Limitations: []string{"Kubernetes Pod status only retains the most recent terminated state for each container."},
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

type nodeRequestsJSONReport struct {
	CapturedAt                string                      `json:"capturedAt"`
	Node                      string                      `json:"node"`
	Namespace                 string                      `json:"namespace,omitempty"`
	Source                    string                      `json:"source"`
	Completeness              string                      `json:"completeness"`
	Resources                 []nodeResourceJSON          `json:"resources"`
	TopResource               string                      `json:"topResource"`
	Consumers                 []nodeConsumerJSON          `json:"consumers"`
	ExtendedResourceConsumers *[]nodeExtendedConsumerJSON `json:"extendedResourceConsumers,omitempty"`
	PodResources              *[]nodePodResourcesJSON     `json:"podResources,omitempty"`
	Warnings                  []string                    `json:"warnings"`
}

type nodeResourceJSON struct {
	Resource    string   `json:"resource"`
	Allocatable string   `json:"allocatable"`
	Requested   *string  `json:"requested"`
	Available   *string  `json:"available"`
	Ratio       *float64 `json:"ratio"`
}

type nodeConsumerJSON struct {
	Namespace   string                `json:"namespace"`
	Pod         string                `json:"pod"`
	UID         string                `json:"uid"`
	CreatedAt   *string               `json:"createdAt"`
	ScheduledAt *string               `json:"scheduledAt"`
	Request     string                `json:"request"`
	Resources   []nodePodResourceJSON `json:"resources"`
	Owner       string                `json:"owner"`
	DaemonSet   bool                  `json:"daemonSet"`
}

type nodeExtendedConsumerJSON struct {
	Resource    string  `json:"resource"`
	Namespace   string  `json:"namespace"`
	Pod         string  `json:"pod"`
	UID         string  `json:"uid"`
	CreatedAt   *string `json:"createdAt"`
	ScheduledAt *string `json:"scheduledAt"`
	Request     string  `json:"request"`
	Owner       string  `json:"owner"`
	DaemonSet   bool    `json:"daemonSet"`
}

type nodePodResourcesJSON struct {
	Namespace   string                `json:"namespace"`
	Pod         string                `json:"pod"`
	UID         string                `json:"uid"`
	CreatedAt   *string               `json:"createdAt"`
	ScheduledAt *string               `json:"scheduledAt"`
	Owner       string                `json:"owner"`
	DaemonSet   bool                  `json:"daemonSet"`
	Resources   []nodePodResourceJSON `json:"resources"`
}

type nodePodResourceJSON struct {
	Resource     string   `json:"resource"`
	Request      *string  `json:"request"`
	Limit        *string  `json:"limit"`
	RequestRatio *float64 `json:"requestRatio"`
}

func (jsonWriter) WriteNodeRequests(out io.Writer, report nodeanalysis.RequestsReport) error {
	resources := make([]nodeResourceJSON, 0, len(report.Resources))
	for _, usage := range report.Resources {
		item := nodeResourceJSON{
			Resource:    string(usage.Resource),
			Allocatable: usage.Allocatable.String(),
		}
		if usage.RequestsKnown {
			requested := usage.Requested.String()
			item.Requested = &requested
		}
		if usage.AvailableKnown {
			available := usage.Available.String()
			item.Available = &available
		}
		if usage.RatioKnown {
			ratio := usage.Ratio
			item.Ratio = &ratio
		}
		resources = append(resources, item)
	}
	consumers := make([]nodeConsumerJSON, 0, len(report.Consumers))
	for _, consumer := range report.Consumers {
		consumers = append(consumers, nodeConsumerJSON{
			Namespace:   consumer.Namespace,
			Pod:         consumer.Pod,
			UID:         consumer.UID,
			CreatedAt:   optionalTimestamp(consumer.CreatedAt),
			ScheduledAt: optionalTimestamp(consumer.ScheduledAt),
			Request:     consumer.Request.String(),
			Resources:   podResourcesJSON(consumer.Resources),
			Owner:       consumer.Owner,
			DaemonSet:   consumer.DaemonSet,
		})
	}
	var extendedConsumers *[]nodeExtendedConsumerJSON
	if report.ShowExtended {
		var items []nodeExtendedConsumerJSON
		if report.ExtendedKnown {
			items = make([]nodeExtendedConsumerJSON, 0, len(report.ExtendedConsumers))
		}
		for _, consumer := range report.ExtendedConsumers {
			items = append(items, nodeExtendedConsumerJSON{
				Resource:    string(consumer.Resource),
				Namespace:   consumer.Namespace,
				Pod:         consumer.Pod,
				UID:         consumer.UID,
				CreatedAt:   optionalTimestamp(consumer.CreatedAt),
				ScheduledAt: optionalTimestamp(consumer.ScheduledAt),
				Request:     consumer.Request.String(),
				Owner:       consumer.Owner,
				DaemonSet:   consumer.DaemonSet,
			})
		}
		extendedConsumers = &items
	}
	var podResources *[]nodePodResourcesJSON
	if report.ShowPods {
		var items []nodePodResourcesJSON
		if report.PodResourcesKnown {
			items = make([]nodePodResourcesJSON, 0, len(report.PodResources))
		}
		for _, pod := range report.PodResources {
			items = append(items, nodePodResourcesJSON{
				Namespace:   pod.Namespace,
				Pod:         pod.Pod,
				UID:         pod.UID,
				CreatedAt:   optionalTimestamp(pod.CreatedAt),
				ScheduledAt: optionalTimestamp(pod.ScheduledAt),
				Owner:       pod.Owner,
				DaemonSet:   pod.DaemonSet,
				Resources:   podResourcesJSON(pod.Resources),
			})
		}
		podResources = &items
	}
	payload := nodeRequestsJSONReport{
		CapturedAt:                report.CapturedAt.UTC().Format(time.RFC3339Nano),
		Node:                      report.Node,
		Namespace:                 report.Namespace,
		Source:                    "CurrentState",
		Completeness:              string(report.Completeness),
		Resources:                 resources,
		TopResource:               string(report.TopResource),
		Consumers:                 consumers,
		ExtendedResourceConsumers: extendedConsumers,
		PodResources:              podResources,
		Warnings:                  append([]string(nil), report.Warnings...),
	}
	if payload.Warnings == nil {
		payload.Warnings = []string{}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func podResourcesJSON(usages []nodeanalysis.PodResource) []nodePodResourceJSON {
	resources := make([]nodePodResourceJSON, 0, len(usages))
	for _, usage := range usages {
		resource := nodePodResourceJSON{Resource: string(usage.Resource)}
		if usage.RequestSet {
			request := usage.Request.String()
			resource.Request = &request
		}
		if usage.LimitSet {
			limit := usage.Limit.String()
			resource.Limit = &limit
		}
		if usage.RatioKnown {
			ratio := usage.RequestRatio
			resource.RequestRatio = &ratio
		}
		resources = append(resources, resource)
	}
	return resources
}

func optionalTimestamp(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

type workloadResourcesJSONReport struct {
	CapturedAt        string                     `json:"capturedAt"`
	Namespace         string                     `json:"namespace,omitempty"`
	Source            string                     `json:"source"`
	PlannedDefinition string                     `json:"plannedDefinition"`
	ActualDefinition  string                     `json:"actualDefinition"`
	Completeness      string                     `json:"completeness"`
	ResourceClass     string                     `json:"resourceClass"`
	Items             []workloadResourceJSONItem `json:"items"`
	Warnings          []string                   `json:"warnings"`
}

type workloadResourceJSONItem struct {
	Namespace string                    `json:"namespace"`
	Kind      string                    `json:"kind"`
	Workload  string                    `json:"workload"`
	UID       string                    `json:"uid"`
	Pods      workloadPodCountsJSON     `json:"pods"`
	Resources workloadResourcesJSON     `json:"resources"`
	Placement workloadNodePlacementJSON `json:"nodePlacement"`
}

type workloadPodCountsJSON struct {
	Planned int32 `json:"planned"`
	Actual  *int  `json:"actual"`
}

type workloadResourcesJSON struct {
	CPU    workloadResourcePairJSON `json:"cpu"`
	Memory workloadResourcePairJSON `json:"memory"`
	GPUs   []workloadGPUJSON        `json:"gpus"`
}

type workloadResourcePairJSON struct {
	Planned string  `json:"planned"`
	Actual  *string `json:"actual"`
}

type workloadGPUJSON struct {
	Resource string  `json:"resource"`
	Planned  string  `json:"planned"`
	Actual   *string `json:"actual"`
}

type workloadNodePlacementJSON struct {
	NodeSelector map[string]string                   `json:"nodeSelector"`
	Required     []workloadNodeSelectorTermJSON      `json:"required"`
	Preferred    []workloadPreferredSelectorTermJSON `json:"preferred"`
}

type workloadPreferredSelectorTermJSON struct {
	Weight     int32                        `json:"weight"`
	Preference workloadNodeSelectorTermJSON `json:"preference"`
}

type workloadNodeSelectorTermJSON struct {
	MatchExpressions []workloadNodeSelectorRequirementJSON `json:"matchExpressions"`
	MatchFields      []workloadNodeSelectorRequirementJSON `json:"matchFields"`
}

type workloadNodeSelectorRequirementJSON struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

func (jsonWriter) WriteWorkloadResources(out io.Writer, report workloadanalysis.Report) error {
	payload := workloadResourcesJSONReport{
		CapturedAt:        report.CapturedAt.UTC().Format(time.RFC3339Nano),
		Namespace:         report.Namespace,
		Source:            "PodRequests",
		PlannedDefinition: "Workload-specific desired concurrent Pods multiplied by current Pod template requests; CronJob plans are per run",
		ActualDefinition:  "Active Pods represented by or owned by the reported workload and assigned to a Node",
		Completeness:      string(report.Completeness),
		ResourceClass:     string(report.ResourceClass),
		Items:             make([]workloadResourceJSONItem, 0, len(report.Items)),
		Warnings:          append([]string(nil), report.Warnings...),
	}
	if payload.Warnings == nil {
		payload.Warnings = []string{}
	}
	for _, item := range report.Items {
		entry := workloadResourceJSONItem{
			Namespace: item.Namespace,
			Kind:      string(item.Kind),
			Workload:  item.Workload,
			UID:       item.UID,
			Pods: workloadPodCountsJSON{
				Planned: item.Pods.Planned,
			},
			Resources: workloadResourcesJSON{
				CPU:    workloadResourcePairJSON{Planned: item.CPU.Planned.String()},
				Memory: workloadResourcePairJSON{Planned: item.Memory.Planned.String()},
				GPUs:   make([]workloadGPUJSON, 0, len(item.GPUs)),
			},
			Placement: workloadPlacementJSON(item.Placement),
		}
		if item.Pods.ActualKnown {
			actual := item.Pods.Actual
			entry.Pods.Actual = &actual
		}
		if item.CPU.ActualKnown {
			actual := item.CPU.Actual.String()
			entry.Resources.CPU.Actual = &actual
		}
		if item.Memory.ActualKnown {
			actual := item.Memory.Actual.String()
			entry.Resources.Memory.Actual = &actual
		}
		for _, gpu := range item.GPUs {
			resource := workloadGPUJSON{Resource: string(gpu.Resource), Planned: gpu.Planned.String()}
			if gpu.ActualKnown {
				actual := gpu.Actual.String()
				resource.Actual = &actual
			}
			entry.Resources.GPUs = append(entry.Resources.GPUs, resource)
		}
		payload.Items = append(payload.Items, entry)
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func workloadPlacementJSON(item workloadanalysis.NodePlacement) workloadNodePlacementJSON {
	result := workloadNodePlacementJSON{
		NodeSelector: make(map[string]string, len(item.NodeSelector)),
		Required:     make([]workloadNodeSelectorTermJSON, 0, len(item.Required)),
		Preferred:    make([]workloadPreferredSelectorTermJSON, 0, len(item.Preferred)),
	}
	for _, selector := range item.NodeSelector {
		result.NodeSelector[selector.Key] = selector.Value
	}
	for _, term := range item.Required {
		result.Required = append(result.Required, workloadTermJSON(term))
	}
	for _, preferred := range item.Preferred {
		result.Preferred = append(result.Preferred, workloadPreferredSelectorTermJSON{
			Weight:     preferred.Weight,
			Preference: workloadTermJSON(preferred.Preference),
		})
	}
	return result
}

func workloadTermJSON(item workloadanalysis.NodeSelectorTerm) workloadNodeSelectorTermJSON {
	result := workloadNodeSelectorTermJSON{
		MatchExpressions: make([]workloadNodeSelectorRequirementJSON, 0, len(item.MatchExpressions)),
		MatchFields:      make([]workloadNodeSelectorRequirementJSON, 0, len(item.MatchFields)),
	}
	for _, requirement := range item.MatchExpressions {
		result.MatchExpressions = append(result.MatchExpressions, workloadRequirementJSON(requirement))
	}
	for _, requirement := range item.MatchFields {
		result.MatchFields = append(result.MatchFields, workloadRequirementJSON(requirement))
	}
	return result
}

func workloadRequirementJSON(item workloadanalysis.NodeSelectorRequirement) workloadNodeSelectorRequirementJSON {
	values := append([]string(nil), item.Values...)
	if values == nil {
		values = []string{}
	}
	return workloadNodeSelectorRequirementJSON{Key: item.Key, Operator: string(item.Operator), Values: values}
}

type pendingJSONReport struct {
	CapturedAt   string                   `json:"capturedAt"`
	Namespace    string                   `json:"namespace"`
	Pod          string                   `json:"pod"`
	UID          string                   `json:"uid"`
	Phase        string                   `json:"phase"`
	Scheduler    string                   `json:"scheduler"`
	Completeness string                   `json:"completeness"`
	Observed     pendingObservedJSON      `json:"observed"`
	CurrentState pendingCurrentStateJSON  `json:"currentState"`
	Unsupported  []pendingUnsupportedJSON `json:"unsupported"`
	Warnings     []string                 `json:"warnings"`
}

type pendingObservedJSON struct {
	Condition *pendingConditionJSON `json:"condition"`
	Events    []pendingEventJSON    `json:"events"`
}

type pendingConditionJSON struct {
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

type pendingEventJSON struct {
	Type       string `json:"type"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	Count      int32  `json:"count"`
	ObservedAt string `json:"observedAt"`
}

type pendingCurrentStateJSON struct {
	Source       string                      `json:"source"`
	Available    bool                        `json:"available"`
	Summary      []pendingFailureSummaryJSON `json:"summary"`
	Nodes        []pendingNodeJSON           `json:"nodes"`
	ClosestNodes []pendingNodeJSON           `json:"closestNodes"`
}

type pendingFailureSummaryJSON struct {
	Code     string   `json:"code"`
	Category string   `json:"category"`
	Count    int      `json:"count"`
	Nodes    []string `json:"nodes"`
}

type pendingNodeJSON struct {
	Node                string                `json:"node"`
	HardFailureCount    int                   `json:"hardFailureCount"`
	NonResourceFailures int                   `json:"nonResourceFailures"`
	ResourceGapScore    float64               `json:"resourceGapScore"`
	LimitingResource    string                `json:"limitingResource,omitempty"`
	Failures            []pendingFailureJSON  `json:"failures"`
	TopConsumers        []pendingConsumerJSON `json:"topConsumers"`
}

type pendingFailureJSON struct {
	Code     string            `json:"code"`
	Category string            `json:"category"`
	Source   string            `json:"source"`
	Message  string            `json:"message"`
	Details  map[string]string `json:"details,omitempty"`
}

type pendingConsumerJSON struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Request   string `json:"request"`
}

type pendingUnsupportedJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (jsonWriter) WritePending(out io.Writer, report pending.Report) error {
	payload := pendingJSONReport{
		CapturedAt:   report.CapturedAt.UTC().Format(time.RFC3339Nano),
		Namespace:    report.Namespace,
		Pod:          report.Pod,
		UID:          report.UID,
		Phase:        string(report.Phase),
		Scheduler:    report.Scheduler,
		Completeness: string(report.Completeness),
		Observed: pendingObservedJSON{
			Events: make([]pendingEventJSON, 0, len(report.Events)),
		},
		CurrentState: pendingCurrentStateJSON{
			Source:       string(pending.SourceCurrentState),
			Available:    report.CurrentStateAvailable,
			Summary:      make([]pendingFailureSummaryJSON, 0, len(report.Summary)),
			Nodes:        pendingNodesJSON(report.Nodes),
			ClosestNodes: pendingNodesJSON(report.ClosestNodes),
		},
		Unsupported: make([]pendingUnsupportedJSON, 0, len(report.Unsupported)),
		Warnings:    append([]string(nil), report.Warnings...),
	}
	if payload.Warnings == nil {
		payload.Warnings = []string{}
	}
	if report.Condition != nil {
		payload.Observed.Condition = &pendingConditionJSON{
			Status:             string(report.Condition.Status),
			Reason:             report.Condition.Reason,
			Message:            report.Condition.Message,
			LastTransitionTime: formatOptionalTime(report.Condition.LastTransitionTime),
		}
	}
	for _, event := range report.Events {
		payload.Observed.Events = append(payload.Observed.Events, pendingEventJSON{
			Type:       event.Type,
			Reason:     event.Reason,
			Message:    event.Message,
			Count:      event.Count,
			ObservedAt: formatOptionalTime(event.ObservedAt),
		})
	}
	for _, item := range report.Summary {
		payload.CurrentState.Summary = append(payload.CurrentState.Summary, pendingFailureSummaryJSON{
			Code: item.Code, Category: item.Category, Count: item.Count, Nodes: append([]string(nil), item.Nodes...),
		})
	}
	for _, item := range report.Unsupported {
		payload.Unsupported = append(payload.Unsupported, pendingUnsupportedJSON{Code: item.Code, Message: item.Message})
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func pendingNodesJSON(nodes []pending.NodeResult) []pendingNodeJSON {
	result := make([]pendingNodeJSON, 0, len(nodes))
	for _, node := range nodes {
		item := pendingNodeJSON{
			Node:                node.Node,
			HardFailureCount:    node.HardFailureCount,
			NonResourceFailures: node.NonResourceFailures,
			ResourceGapScore:    node.ResourceGapScore,
			LimitingResource:    node.LimitingResource,
			Failures:            make([]pendingFailureJSON, 0, len(node.Failures)),
			TopConsumers:        make([]pendingConsumerJSON, 0, len(node.TopConsumers)),
		}
		for _, failure := range node.Failures {
			item.Failures = append(item.Failures, pendingFailureJSON{
				Code: failure.Code, Category: failure.Category, Source: string(failure.Source), Message: failure.Message, Details: failure.Details,
			})
		}
		for _, consumer := range node.TopConsumers {
			item.TopConsumers = append(item.TopConsumers, pendingConsumerJSON{Namespace: consumer.Namespace, Pod: consumer.Pod, Request: consumer.Request})
		}
		result = append(result, item)
	}
	return result
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

type eventsTimelineJSONReport struct {
	CapturedAt   string                    `json:"capturedAt"`
	APIVersion   string                    `json:"apiVersion"`
	Completeness string                    `json:"completeness"`
	Items        []eventsTimelineJSONItem  `json:"items"`
	Summary      eventsTimelineJSONSummary `json:"summary"`
}

type eventsTimelineJSONItem struct {
	Namespace           string                     `json:"namespace"`
	Name                string                     `json:"name"`
	UID                 string                     `json:"uid"`
	Type                string                     `json:"type"`
	Reason              string                     `json:"reason"`
	Action              string                     `json:"action"`
	Note                string                     `json:"note"`
	ReportingController string                     `json:"reportingController"`
	ReportingInstance   string                     `json:"reportingInstance"`
	Regarding           eventsObjectReferenceJSON  `json:"regarding"`
	Related             *eventsObjectReferenceJSON `json:"related"`
	FirstObservedAt     string                     `json:"firstObservedAt"`
	LastObservedAt      string                     `json:"lastObservedAt"`
	Count               int32                      `json:"count"`
	Series              bool                       `json:"series"`
}

type eventsObjectReferenceJSON struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
}

type eventsTimelineJSONSummary struct {
	Returned                int `json:"returned"`
	IgnoredMissingTimestamp int `json:"ignoredMissingTimestamp"`
}

func (jsonWriter) WriteEventsTimeline(out io.Writer, report timeline.Report) error {
	items := make([]eventsTimelineJSONItem, 0, len(report.Items))
	for _, item := range report.Items {
		entry := eventsTimelineJSONItem{
			Namespace:           item.Namespace,
			Name:                item.Name,
			UID:                 item.UID,
			Type:                item.Type,
			Reason:              item.Reason,
			Action:              item.Action,
			Note:                item.Note,
			ReportingController: item.ReportingController,
			ReportingInstance:   item.ReportingInstance,
			Regarding:           eventsReferenceJSON(item.Regarding),
			FirstObservedAt:     formatOptionalTime(item.FirstObservedAt),
			LastObservedAt:      formatOptionalTime(item.LastObservedAt),
			Count:               item.Count,
			Series:              item.Series,
		}
		if item.Related != nil {
			related := eventsReferenceJSON(*item.Related)
			entry.Related = &related
		}
		items = append(items, entry)
	}
	payload := eventsTimelineJSONReport{
		CapturedAt:   report.CapturedAt.UTC().Format(time.RFC3339Nano),
		APIVersion:   report.APIVersion,
		Completeness: report.Completeness,
		Items:        items,
		Summary: eventsTimelineJSONSummary{
			Returned:                len(items),
			IgnoredMissingTimestamp: report.IgnoredMissingTimestamp,
		},
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func eventsReferenceJSON(item timeline.ObjectReference) eventsObjectReferenceJSON {
	return eventsObjectReferenceJSON{
		APIVersion: item.APIVersion,
		Kind:       item.Kind,
		Namespace:  item.Namespace,
		Name:       item.Name,
		UID:        item.UID,
	}
}

type nodeDrainCheckJSONReport struct {
	CapturedAt   string                    `json:"capturedAt"`
	Node         string                    `json:"node"`
	Drainability string                    `json:"drainability"`
	Completeness string                    `json:"completeness"`
	Options      nodeDrainCheckJSONOptions `json:"options"`
	Pods         []nodeDrainCheckJSONPod   `json:"pods"`
	PDBs         []nodeDrainCheckJSONPDB   `json:"pdbs"`
	Summary      nodeDrainCheckJSONSummary `json:"summary"`
	Warnings     []string                  `json:"warnings"`
}

type nodeDrainCheckJSONOptions struct {
	IgnoreDaemonSets   bool `json:"ignoreDaemonSets"`
	Force              bool `json:"force"`
	DeleteEmptyDirData bool `json:"deleteEmptyDirData"`
}

type nodeDrainCheckJSONPod struct {
	Namespace string             `json:"namespace"`
	Pod       string             `json:"pod"`
	UID       string             `json:"uid"`
	Owner     string             `json:"owner"`
	Phase     string             `json:"phase"`
	Action    string             `json:"action"`
	Impacts   []drain.ImpactCode `json:"impacts"`
	Blockers  []drain.ImpactCode `json:"blockers"`
}

type nodeDrainCheckJSONPDB struct {
	Namespace          string `json:"namespace"`
	Name               string `json:"name"`
	MatchedTargets     int    `json:"matchedTargets"`
	DisruptionsAllowed int32  `json:"disruptionsAllowed"`
	UnhealthyExempt    int    `json:"unhealthyExempt"`
	Blocked            bool   `json:"blocked"`
	SelectorValid      bool   `json:"selectorValid"`
}

type nodeDrainCheckJSONSummary struct {
	Pods    int `json:"pods"`
	Evict   int `json:"evict"`
	Delete  int `json:"delete"`
	Skip    int `json:"skip"`
	Blocked int `json:"blocked"`
}

func (jsonWriter) WriteNodeDrainCheck(out io.Writer, report drain.Report) error {
	payload := nodeDrainCheckJSONReport{
		CapturedAt:   report.CapturedAt.UTC().Format(time.RFC3339Nano),
		Node:         report.Node,
		Drainability: string(report.Drainability),
		Completeness: string(report.Completeness),
		Options: nodeDrainCheckJSONOptions{
			IgnoreDaemonSets:   report.Options.IgnoreDaemonSets,
			Force:              report.Options.Force,
			DeleteEmptyDirData: report.Options.DeleteEmptyDirData,
		},
		Pods: make([]nodeDrainCheckJSONPod, 0, len(report.Pods)),
		PDBs: make([]nodeDrainCheckJSONPDB, 0, len(report.PDBs)),
		Summary: nodeDrainCheckJSONSummary{
			Pods:    report.Summary.Pods,
			Evict:   report.Summary.Evict,
			Delete:  report.Summary.Delete,
			Skip:    report.Summary.Skip,
			Blocked: report.Summary.Blocked,
		},
		Warnings: append([]string(nil), report.Warnings...),
	}
	if payload.Warnings == nil {
		payload.Warnings = []string{}
	}
	for _, pod := range report.Pods {
		impacts := append([]drain.ImpactCode(nil), pod.Impacts...)
		if impacts == nil {
			impacts = []drain.ImpactCode{}
		}
		blockers := append([]drain.ImpactCode(nil), pod.Blockers...)
		if blockers == nil {
			blockers = []drain.ImpactCode{}
		}
		payload.Pods = append(payload.Pods, nodeDrainCheckJSONPod{
			Namespace: pod.Namespace,
			Pod:       pod.Pod,
			UID:       pod.UID,
			Owner:     pod.Owner,
			Phase:     pod.Phase,
			Action:    string(pod.Action),
			Impacts:   impacts,
			Blockers:  blockers,
		})
	}
	for _, pdb := range report.PDBs {
		payload.PDBs = append(payload.PDBs, nodeDrainCheckJSONPDB{
			Namespace:          pdb.Namespace,
			Name:               pdb.Name,
			MatchedTargets:     pdb.MatchedTargets,
			DisruptionsAllowed: pdb.DisruptionsAllowed,
			UnhealthyExempt:    pdb.UnhealthyExempt,
			Blocked:            pdb.Blocked,
			SelectorValid:      pdb.SelectorValid,
		})
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

type rolloutExplainJSONReport struct {
	CapturedAt   string                  `json:"capturedAt"`
	Status       string                  `json:"status"`
	Completeness string                  `json:"completeness"`
	Deployment   rolloutDeploymentJSON   `json:"deployment"`
	Conditions   []rolloutConditionJSON  `json:"conditions"`
	ReplicaSets  []rolloutReplicaSetJSON `json:"replicaSets"`
	Pods         []rolloutPodJSON        `json:"pods"`
	Findings     []rolloutFindingJSON    `json:"findings"`
	Events       []rolloutEventJSON      `json:"events"`
	Warnings     []string                `json:"warnings"`
}

type rolloutDeploymentJSON struct {
	Namespace          string `json:"namespace"`
	Name               string `json:"name"`
	UID                string `json:"uid"`
	Generation         int64  `json:"generation"`
	ObservedGeneration int64  `json:"observedGeneration"`
	Desired            int32  `json:"desired"`
	Current            int32  `json:"current"`
	Updated            int32  `json:"updated"`
	Ready              int32  `json:"ready"`
	Available          int32  `json:"available"`
	Unavailable        int32  `json:"unavailable"`
}

type rolloutConditionJSON struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

type rolloutReplicaSetJSON struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
	Revision  int64  `json:"revision"`
	Current   bool   `json:"current"`
	Desired   int32  `json:"desired"`
	Replicas  int32  `json:"replicas"`
	Ready     int32  `json:"ready"`
	Available int32  `json:"available"`
	CreatedAt string `json:"createdAt"`
}

type rolloutPodJSON struct {
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	UID        string   `json:"uid"`
	ReplicaSet string   `json:"replicaSet"`
	Node       string   `json:"node"`
	Phase      string   `json:"phase"`
	Ready      bool     `json:"ready"`
	Restarts   int32    `json:"restarts"`
	Reasons    []string `json:"reasons"`
	CreatedAt  string   `json:"createdAt"`
}

type rolloutFindingJSON struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Object   string `json:"object"`
	Message  string `json:"message"`
}

type rolloutEventJSON struct {
	Type                string `json:"type"`
	Reason              string `json:"reason"`
	RegardingKind       string `json:"regardingKind"`
	RegardingName       string `json:"regardingName"`
	Note                string `json:"note"`
	Count               int32  `json:"count"`
	LastObservedAt      string `json:"lastObservedAt"`
	ReportingController string `json:"reportingController"`
}

func (jsonWriter) WriteRolloutExplain(out io.Writer, report rollout.Report) error {
	payload := rolloutExplainJSONReport{
		CapturedAt:   report.CapturedAt.UTC().Format(time.RFC3339Nano),
		Status:       string(report.Status),
		Completeness: string(report.Completeness),
		Deployment: rolloutDeploymentJSON{
			Namespace:          report.Deployment.Namespace,
			Name:               report.Deployment.Name,
			UID:                report.Deployment.UID,
			Generation:         report.Deployment.Generation,
			ObservedGeneration: report.Deployment.ObservedGeneration,
			Desired:            report.Deployment.Desired,
			Current:            report.Deployment.Current,
			Updated:            report.Deployment.Updated,
			Ready:              report.Deployment.Ready,
			Available:          report.Deployment.Available,
			Unavailable:        report.Deployment.Unavailable,
		},
		Conditions:  make([]rolloutConditionJSON, 0, len(report.Conditions)),
		ReplicaSets: make([]rolloutReplicaSetJSON, 0, len(report.ReplicaSets)),
		Pods:        make([]rolloutPodJSON, 0, len(report.Pods)),
		Findings:    make([]rolloutFindingJSON, 0, len(report.Findings)),
		Events:      make([]rolloutEventJSON, 0, len(report.Events)),
		Warnings:    append([]string(nil), report.Warnings...),
	}
	if payload.Warnings == nil {
		payload.Warnings = []string{}
	}
	for _, item := range report.Conditions {
		payload.Conditions = append(payload.Conditions, rolloutConditionJSON{
			Type: item.Type, Status: item.Status, Reason: item.Reason, Message: item.Message, LastTransitionTime: formatOptionalTime(item.LastTransitionTime),
		})
	}
	for _, item := range report.ReplicaSets {
		payload.ReplicaSets = append(payload.ReplicaSets, rolloutReplicaSetJSON{
			Namespace: item.Namespace, Name: item.Name, UID: item.UID, Revision: item.Revision, Current: item.Current,
			Desired: item.Desired, Replicas: item.Replicas, Ready: item.Ready, Available: item.Available, CreatedAt: formatOptionalTime(item.CreatedAt),
		})
	}
	for _, item := range report.Pods {
		reasons := append([]string(nil), item.Reasons...)
		if reasons == nil {
			reasons = []string{}
		}
		payload.Pods = append(payload.Pods, rolloutPodJSON{
			Namespace: item.Namespace, Name: item.Name, UID: item.UID, ReplicaSet: item.ReplicaSet, Node: item.Node,
			Phase: item.Phase, Ready: item.Ready, Restarts: item.Restarts, Reasons: reasons, CreatedAt: formatOptionalTime(item.CreatedAt),
		})
	}
	for _, item := range report.Findings {
		payload.Findings = append(payload.Findings, rolloutFindingJSON{Severity: string(item.Severity), Code: item.Code, Object: item.Object, Message: item.Message})
	}
	for _, item := range report.Events {
		payload.Events = append(payload.Events, rolloutEventJSON{
			Type: item.Type, Reason: item.Reason, RegardingKind: item.RegardingKind, RegardingName: item.RegardingName,
			Note: item.Note, Count: item.Count, LastObservedAt: formatOptionalTime(item.LastObservedAt), ReportingController: item.ReportingController,
		})
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
