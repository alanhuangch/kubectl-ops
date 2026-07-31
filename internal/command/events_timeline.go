package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	timeline "github.com/alanhuangch/kubectl-ops/internal/events"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

type eventsTimelineOptions struct {
	root      *rootOptions
	namespace string
	since     time.Duration
	limit     int
	reverse   bool
	kind      string
	name      string
	uid       string
	reason    string
	eventType string
	source    string
}

func newEventsTimelineCommand(root *rootOptions) *cobra.Command {
	options := &eventsTimelineOptions{root: root, since: time.Hour, limit: 100}
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Show a stable cross-object Event timeline",
		Long:  "Show Kubernetes Events as a chronological timeline. Event Series are represented once with their first time, latest time, and occurrence count.",
		Example: `  kubectl ops events timeline -A --since 30m
  kubectl ops events timeline -n production --kind Pod --name api-7d8f
  kubectl ops events timeline -A --uid 2c50d5c4-...
  kubectl ops events timeline -A --type Warning --reverse -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := options.complete(cmd); err != nil {
				return err
			}
			if err := options.validate(); err != nil {
				return err
			}
			return options.run(cmd.Context())
		},
	}
	cmd.Flags().DurationVar(&options.since, "since", time.Hour, "Only include Events observed within this duration")
	cmd.Flags().IntVar(&options.limit, "limit", 100, "Maximum number of Events to return")
	cmd.Flags().BoolVar(&options.reverse, "reverse", false, "Sort newest Events first")
	cmd.Flags().StringVar(&options.kind, "kind", "", "Filter by regarding object Kind")
	cmd.Flags().StringVar(&options.name, "name", "", "Filter by regarding object name")
	cmd.Flags().StringVar(&options.uid, "uid", "", "Filter by regarding object UID using a server-side field selector")
	cmd.Flags().StringVar(&options.reason, "reason", "", "Filter by Event reason")
	cmd.Flags().StringVar(&options.eventType, "type", "", "Filter by Event type: Normal or Warning")
	cmd.Flags().StringVar(&options.source, "source", "", "Filter by reporting controller")
	return cmd
}

func (options *eventsTimelineOptions) complete(cmd *cobra.Command) error {
	if options.root.allNamespaces {
		if cmd.Flags().Changed("namespace") {
			return fmt.Errorf("--namespace and --all-namespaces cannot be used together")
		}
		options.namespace = metav1.NamespaceAll
		return nil
	}
	namespace, _, err := options.root.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("resolve namespace: %w", err)
	}
	options.namespace = namespace
	return nil
}

func (options *eventsTimelineOptions) validate() error {
	if options.since <= 0 {
		return fmt.Errorf("--since must be greater than zero")
	}
	if options.limit <= 0 {
		return fmt.Errorf("--limit must be greater than zero")
	}
	if options.eventType != "" && !strings.EqualFold(options.eventType, corev1.EventTypeNormal) && !strings.EqualFold(options.eventType, corev1.EventTypeWarning) {
		return fmt.Errorf("invalid Event type %q: expected Normal or Warning", options.eventType)
	}
	if _, err := options.root.dependencies.writerFactory(options.root.outputFormat); err != nil {
		return err
	}
	return nil
}

func (options *eventsTimelineOptions) run(ctx context.Context) error {
	client, err := options.root.dependencies.clientFactory()
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	analyzerOptions := timeline.Options{
		Now:                 options.root.dependencies.now(),
		Since:               options.since,
		Limit:               options.limit,
		Reverse:             options.reverse,
		Kind:                options.kind,
		Name:                options.name,
		UID:                 options.uid,
		Reason:              options.reason,
		Type:                options.eventType,
		ReportingController: options.source,
	}

	items, primaryErr := listEventsV1(ctx, client, options.namespace, options.uid)
	var report timeline.Report
	if primaryErr == nil {
		report = timeline.AnalyzeV1(items, analyzerOptions)
	} else if canFallbackToCoreEvents(primaryErr) {
		coreItems, fallbackErr := listCoreEvents(ctx, client, options.namespace, options.uid)
		if fallbackErr != nil {
			return fmt.Errorf("list Events using events.k8s.io/v1 and core/v1: %w", errors.Join(primaryErr, fallbackErr))
		}
		report = timeline.AnalyzeCore(coreItems, analyzerOptions)
	} else {
		return fmt.Errorf("list Events: %w", primaryErr)
	}

	writer, err := options.root.dependencies.writerFactory(options.root.outputFormat)
	if err != nil {
		return err
	}
	if err := writer.WriteEventsTimeline(options.root.streams.Out, report); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if options.root.outputFormat != "json" && report.IgnoredMissingTimestamp > 0 {
		fmt.Fprintf(options.root.streams.ErrOut, "warning: ignored %d Event(s) without a usable timestamp\n", report.IgnoredMissingTimestamp)
	}
	return nil
}

func listEventsV1(ctx context.Context, client kubernetes.Interface, namespace, uid string) ([]eventsv1.Event, error) {
	options := metav1.ListOptions{Limit: podListPageSize}
	if uid != "" {
		options.FieldSelector = fields.OneTermEqualSelector("regarding.uid", uid).String()
	}
	var result []eventsv1.Event
	for {
		page, err := client.EventsV1().Events(namespace).List(ctx, options)
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

func listCoreEvents(ctx context.Context, client kubernetes.Interface, namespace, uid string) ([]corev1.Event, error) {
	options := metav1.ListOptions{Limit: podListPageSize}
	if uid != "" {
		options.FieldSelector = fields.OneTermEqualSelector("involvedObject.uid", uid).String()
	}
	var result []corev1.Event
	for {
		page, err := client.CoreV1().Events(namespace).List(ctx, options)
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

func canFallbackToCoreEvents(err error) bool {
	return apierrors.IsForbidden(err) || apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err)
}
