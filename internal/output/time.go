package output

import (
	"fmt"
	"time"
)

func humanDuration(value time.Duration) string {
	negative := value < 0
	if negative {
		value = -value
	}

	var rounded time.Duration
	switch {
	case value < time.Second:
		rounded = value.Round(time.Millisecond)
	case value < 10*time.Second:
		rounded = value.Round(100 * time.Millisecond)
	case value < time.Minute:
		rounded = value.Round(time.Second)
	case value < time.Hour:
		rounded = value.Round(time.Second)
	case value < 24*time.Hour:
		rounded = value.Round(time.Minute)
	default:
		return fmt.Sprintf("%dd", value/(24*time.Hour))
	}

	if rounded >= time.Hour && rounded%time.Hour == 0 {
		if negative {
			return fmt.Sprintf("-%dh", rounded/time.Hour)
		}
		return fmt.Sprintf("%dh", rounded/time.Hour)
	}
	if rounded >= time.Minute && rounded%time.Minute == 0 {
		if negative {
			return fmt.Sprintf("-%dm", rounded/time.Minute)
		}
		return fmt.Sprintf("%dm", rounded/time.Minute)
	}
	if negative {
		return "-" + rounded.String()
	}
	return rounded.String()
}

func relativeTime(now, value time.Time) string {
	delta := now.Sub(value)
	if delta < 0 {
		return "in " + humanDuration(-delta)
	}
	return humanDuration(delta) + " ago"
}

func relativeTimeOrDash(now, value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return relativeTime(now, value)
}
