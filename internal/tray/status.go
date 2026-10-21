// Package tray decides what the tray icon shows for a service snapshot.
package tray

import (
	"strings"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/display"
)

type Level string

const (
	LevelOK      Level = "ok"
	LevelWarning Level = "warning"
	LevelStale   Level = "stale"
	LevelError   Level = "error"
)

type Status struct {
	Level   Level
	Tooltip string
}

const title = "WindowSeat"

var failedStates = map[contracts.ProviderState]bool{
	contracts.ProviderAuthRequired:       true,
	contracts.ProviderUnsupportedVersion: true,
	contracts.ProviderUnavailable:        true,
	contracts.ProviderMalformedResponse:  true,
}

// Unreachable is the status while the service cannot be reached.
func Unreachable() Status {
	return Status{Level: LevelError, Tooltip: title + "\nreconnecting to the service"}
}

// usageNotAllowed matches the CLI wording for a provider that refuses ordinary usage.
const usageNotAllowed = "provider reports usage not allowed"

// Summarize picks the icon level and tooltip. A failed provider outranks a
// warning, a warning outranks out-of-date readings, and only current
// readings can warn. warnAt is the used percentage that counts as a warning;
// a provider that reports usage not allowed also warns.
func Summarize(snapshot contracts.Snapshot, warnAt float64, now time.Time) Status {
	lines := []string{title}
	var failed, outdated, warning bool
	var failures, refusals []string

	tightest, tightestShare := contracts.ProviderSnapshot{}, -1.0
	var tightestMeter contracts.Meter
	for _, p := range snapshot.Providers {
		if failedStates[p.State] {
			failed = true
			failures = append(failures, string(p.Provider)+": "+display.ErrorText(p.Error))
			continue
		}
		current := p.State != contracts.ProviderStale && p.Freshness.State == contracts.FreshnessCurrent
		outdated = outdated || !current
		if refusesUsage(p) {
			refusals = append(refusals, string(p.Provider)+": "+usageNotAllowed)
			warning = warning || current
		}
		for _, meter := range p.Meters {
			share, known := usedShare(meter)
			if !known {
				continue
			}
			warning = warning || (current && share*100 >= warnAt)
			if share > tightestShare {
				tightest, tightestMeter, tightestShare = p, meter, share
			}
		}
	}

	switch {
	case tightestShare >= 0:
		lines = append(lines,
			string(tightest.Provider)+" "+tightestMeter.Label+": "+display.MeterValue(tightestMeter),
			"read "+display.HumanDuration(now.Sub(tightest.ObservedAt))+" ago")
	case len(failures) == 0:
		lines = append(lines, "no readings yet")
	}
	lines = append(lines, refusals...)
	if outdated {
		lines = append(lines, "some readings are out of date")
	}
	lines = append(lines, failures...)

	return Status{Level: level(failed, warning, outdated), Tooltip: strings.Join(lines, "\n")}
}

func refusesUsage(p contracts.ProviderSnapshot) bool {
	return p.UsageAllowed != nil && !*p.UsageAllowed
}

func level(failed, warning, outdated bool) Level {
	switch {
	case failed:
		return LevelError
	case warning:
		return LevelWarning
	case outdated:
		return LevelStale
	default:
		return LevelOK
	}
}

// usedShare is the used fraction of a meter, when the meter says enough to know it.
func usedShare(meter contracts.Meter) (float64, bool) {
	switch {
	case meter.Unlimited != nil && *meter.Unlimited:
		return 0, false
	case meter.UsedPercentage != nil:
		return *meter.UsedPercentage / 100, true
	case meter.Used != nil && meter.Entitlement != nil && *meter.Entitlement > 0:
		return *meter.Used / *meter.Entitlement, true
	case meter.RemainingPercentage != nil:
		return 1 - *meter.RemainingPercentage/100, true
	default:
		return 0, false
	}
}
