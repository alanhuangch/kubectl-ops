package drain

import "time"

type Completeness string

const (
	CompletenessComplete Completeness = "Complete"
	CompletenessPartial  Completeness = "Partial"
)

type Drainability string

const (
	DrainabilityReady   Drainability = "Ready"
	DrainabilityBlocked Drainability = "Blocked"
	DrainabilityUnknown Drainability = "Unknown"
)

type PodAction string

const (
	PodActionSkip    PodAction = "Skip"
	PodActionDelete  PodAction = "Delete"
	PodActionEvict   PodAction = "Evict"
	PodActionBlocked PodAction = "Blocked"
)

type ImpactCode string

const (
	ImpactMirrorPod        ImpactCode = "MirrorPod"
	ImpactDaemonSetManaged ImpactCode = "DaemonSetManaged"
	ImpactUnmanagedPod     ImpactCode = "UnmanagedPod"
	ImpactLocalStorage     ImpactCode = "LocalStorage"
	ImpactTerminalPod      ImpactCode = "TerminalPod"
	ImpactPDBViolation     ImpactCode = "PDBViolation"
)

type Options struct {
	Now                time.Time
	IgnoreDaemonSets   bool
	Force              bool
	DeleteEmptyDirData bool
	PDBsKnown          bool
}

type AppliedOptions struct {
	IgnoreDaemonSets   bool
	Force              bool
	DeleteEmptyDirData bool
}

type PodImpact struct {
	Namespace string
	Pod       string
	UID       string
	Owner     string
	Phase     string
	Action    PodAction
	Impacts   []ImpactCode
	Blockers  []ImpactCode
}

type PDBImpact struct {
	Namespace          string
	Name               string
	MatchedTargets     int
	DisruptionsAllowed int32
	UnhealthyExempt    int
	Blocked            bool
	SelectorValid      bool
}

type Summary struct {
	Pods    int
	Evict   int
	Delete  int
	Skip    int
	Blocked int
}

type Report struct {
	CapturedAt   time.Time
	Node         string
	Drainability Drainability
	Completeness Completeness
	Options      AppliedOptions
	Pods         []PodImpact
	PDBs         []PDBImpact
	Summary      Summary
	Warnings     []string
}
