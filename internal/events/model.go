package events

import "time"

type ObjectReference struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	UID        string
}

type TimelineEvent struct {
	Namespace           string
	Name                string
	UID                 string
	Type                string
	Reason              string
	Action              string
	Note                string
	ReportingController string
	ReportingInstance   string
	Regarding           ObjectReference
	Related             *ObjectReference
	FirstObservedAt     time.Time
	LastObservedAt      time.Time
	Count               int32
	Series              bool
}

type Options struct {
	Now                 time.Time
	Since               time.Duration
	Limit               int
	Reverse             bool
	Kind                string
	Name                string
	UID                 string
	Reason              string
	Type                string
	ReportingController string
}

type Report struct {
	CapturedAt              time.Time
	APIVersion              string
	Completeness            string
	Items                   []TimelineEvent
	IgnoredMissingTimestamp int
}
