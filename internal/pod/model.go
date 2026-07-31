package pod

import "time"

type RecentPod struct {
	Namespace       string
	Name            string
	UID             string
	Node            string
	Phase           string
	Scheduler       string
	CreatedAt       time.Time
	ScheduledAt     time.Time
	TimeToScheduled time.Duration
	Static          bool
}

type RecentReport struct {
	CapturedAt              time.Time
	Items                   []RecentPod
	IgnoredMissingScheduled int
	ExcludedStatic          int
}

type ContainerKind string

const (
	ContainerKindRegular   ContainerKind = "Container"
	ContainerKindInit      ContainerKind = "InitContainer"
	ContainerKindEphemeral ContainerKind = "EphemeralContainer"
)

type RestartClassification string

const (
	RestartOOMKilled RestartClassification = "OOMKilled"
	RestartSIGKILL   RestartClassification = "SIGKILL"
	RestartSIGTERM   RestartClassification = "SIGTERM"
	RestartCompleted RestartClassification = "Completed"
	RestartError     RestartClassification = "Error"
	RestartUnknown   RestartClassification = "Unknown"
)

type RestartedContainer struct {
	Namespace      string
	Pod            string
	UID            string
	Node           string
	Container      string
	Kind           ContainerKind
	RestartCount   int32
	LastReason     string
	ExitCode       int32
	Signal         int32
	FinishedAt     time.Time
	Classification RestartClassification
}

type RestartReport struct {
	CapturedAt               time.Time
	Items                    []RestartedContainer
	IgnoredMissingFinishedAt int
}
