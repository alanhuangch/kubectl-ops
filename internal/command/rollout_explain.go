package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	rolloutanalysis "github.com/alanhuangch/kubectl-ops/internal/rollout"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type rolloutExplainOptions struct {
	root       *rootOptions
	namespace  string
	name       string
	since      time.Duration
	eventLimit int
}

func newRolloutExplainCommand(root *rootOptions) *cobra.Command {
	options := &rolloutExplainOptions{root: root, since: time.Hour, eventLimit: 20}
	cmd := &cobra.Command{
		Use:   "explain DEPLOYMENT",
		Short: "Explain a Deployment rollout using ReplicaSets, Pods, and Events",
		Long:  "Correlate a Deployment with its owned ReplicaSets, Pods, conditions, and recent Events. This command is read-only and does not replace kubectl rollout operations.",
		Example: `  kubectl ops rollout explain -n production api
  kubectl ops rollout explain -n production deployment/api
  kubectl ops rollout explain -n production api --since 30m -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := deploymentName(args[0])
			if err != nil {
				return err
			}
			options.name = name
			if err := options.complete(); err != nil {
				return err
			}
			if err := options.validate(); err != nil {
				return err
			}
			return options.run(cmd.Context())
		},
	}
	cmd.Flags().DurationVar(&options.since, "since", time.Hour, "Only include related Events observed within this duration")
	cmd.Flags().IntVar(&options.eventLimit, "event-limit", 20, "Maximum number of related Events to show; zero disables Events in the report")
	return cmd
}

func (options *rolloutExplainOptions) complete() error {
	if options.root.allNamespaces {
		return fmt.Errorf("--all-namespaces cannot be used when selecting one Deployment")
	}
	namespace, _, err := options.root.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("resolve namespace: %w", err)
	}
	options.namespace = namespace
	return nil
}

func (options *rolloutExplainOptions) validate() error {
	if options.since <= 0 {
		return fmt.Errorf("--since must be greater than zero")
	}
	if options.eventLimit < 0 {
		return fmt.Errorf("--event-limit must be zero or greater")
	}
	if _, err := options.root.dependencies.writerFactory(options.root.outputFormat); err != nil {
		return err
	}
	return nil
}

func (options *rolloutExplainOptions) run(ctx context.Context) error {
	client, err := options.root.dependencies.clientFactory()
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	deployment, err := client.AppsV1().Deployments(options.namespace).Get(ctx, options.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Deployment %s/%s: %w", options.namespace, options.name, err)
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return fmt.Errorf("convert Deployment selector: %w", err)
	}

	snapshot := rolloutanalysis.Snapshot{
		CapturedAt:       options.root.dependencies.now(),
		Deployment:       deployment,
		ReplicaSetsKnown: true,
		PodsKnown:        true,
		EventsKnown:      true,
	}
	replicaSets, err := listDeploymentReplicaSets(ctx, client, options.namespace, selector.String())
	if err != nil {
		if isPermissionDenied(err) {
			snapshot.ReplicaSetsKnown = false
			snapshot.Warnings = append(snapshot.Warnings, "ReplicaSet details are unavailable because ReplicaSets could not be listed.")
		} else {
			return fmt.Errorf("list ReplicaSets for Deployment %s/%s: %w", options.namespace, options.name, err)
		}
	} else {
		snapshot.ReplicaSets = replicaSets
	}

	pods, err := listPods(ctx, client, options.namespace, metav1.ListOptions{LabelSelector: selector.String(), Limit: podListPageSize})
	if err != nil {
		if isPermissionDenied(err) {
			snapshot.PodsKnown = false
			snapshot.Warnings = append(snapshot.Warnings, "Pod details are unavailable because Pods could not be listed.")
		} else {
			return fmt.Errorf("list Pods for Deployment %s/%s: %w", options.namespace, options.name, err)
		}
	} else {
		snapshot.Pods = pods
	}

	if options.eventLimit > 0 {
		events, eventsKnown, warnings, err := listRolloutEvents(ctx, client, options.namespace, snapshot.CapturedAt, options.since)
		if err != nil {
			return fmt.Errorf("list Events for Deployment %s/%s: %w", options.namespace, options.name, err)
		}
		snapshot.Events = events
		snapshot.EventsKnown = eventsKnown
		snapshot.Warnings = append(snapshot.Warnings, warnings...)
	}

	report := rolloutanalysis.Analyze(snapshot, rolloutanalysis.Options{EventLimit: options.eventLimit})
	writer, err := options.root.dependencies.writerFactory(options.root.outputFormat)
	if err != nil {
		return err
	}
	if err := writer.WriteRolloutExplain(options.root.streams.Out, report); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if options.root.outputFormat != "json" {
		for _, warning := range report.Warnings {
			fmt.Fprintf(options.root.streams.ErrOut, "warning: %s\n", warning)
		}
	}
	return nil
}

func deploymentName(value string) (string, error) {
	parts := strings.Split(value, "/")
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], nil
	}
	if len(parts) != 2 || parts[1] == "" {
		return "", fmt.Errorf("invalid Deployment %q", value)
	}
	switch strings.ToLower(parts[0]) {
	case "deployment", "deployments", "deploy":
		return parts[1], nil
	default:
		return "", fmt.Errorf("unsupported rollout resource %q: expected deployment", parts[0])
	}
}

func listDeploymentReplicaSets(ctx context.Context, client kubernetes.Interface, namespace, labelSelector string) ([]appsv1.ReplicaSet, error) {
	options := metav1.ListOptions{LabelSelector: labelSelector, Limit: podListPageSize}
	var result []appsv1.ReplicaSet
	for {
		page, err := client.AppsV1().ReplicaSets(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Items...)
		if page.Continue == "" {
			return result, nil
		}
		options.Continue = page.Continue
	}
}

func listRolloutEvents(ctx context.Context, client kubernetes.Interface, namespace string, now time.Time, since time.Duration) ([]timeline.TimelineEvent, bool, []string, error) {
	items, primaryErr := listEventsV1(ctx, client, namespace, "")
	if primaryErr == nil {
		report := timeline.AnalyzeV1(items, timeline.Options{Now: now, Since: since})
		return report.Items, true, eventTimestampWarnings(report.IgnoredMissingTimestamp), nil
	}
	if !canFallbackToCoreEvents(primaryErr) {
		return nil, false, nil, primaryErr
	}
	coreItems, fallbackErr := listCoreEvents(ctx, client, namespace, "")
	if fallbackErr == nil {
		report := timeline.AnalyzeCore(coreItems, timeline.Options{Now: now, Since: since})
		return report.Items, true, eventTimestampWarnings(report.IgnoredMissingTimestamp), nil
	}
	if isPermissionDenied(fallbackErr) || apierrors.IsNotFound(fallbackErr) || apierrors.IsMethodNotSupported(fallbackErr) {
		return nil, false, []string{"Related Events are unavailable because Events could not be listed."}, nil
	}
	return nil, false, nil, errors.Join(primaryErr, fallbackErr)
}

func eventTimestampWarnings(ignored int) []string {
	if ignored == 0 {
		return nil
	}
	return []string{fmt.Sprintf("Ignored %d Event(s) without a usable timestamp.", ignored)}
}

func isPermissionDenied(err error) bool {
	return apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err)
}
