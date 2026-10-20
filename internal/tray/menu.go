package tray

import (
	"strings"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/display"
)

// MenuLines lists every provider and meter for the tray menu, one line each.
func MenuLines(snapshot contracts.Snapshot, now time.Time, loc *time.Location) []string {
	var lines []string
	for _, p := range snapshot.Providers {
		lines = append(lines, providerLine(p, now))
		for _, meter := range p.Meters {
			lines = append(lines, "    "+meterLine(meter, now, loc))
		}
	}
	return lines
}

func providerLine(p contracts.ProviderSnapshot, now time.Time) string {
	if failedStates[p.State] {
		return string(p.Provider) + " · " + string(p.State) + ": " + display.ErrorText(p.Error)
	}
	parts := []string{string(p.Provider), "read " + display.HumanDuration(now.Sub(p.ObservedAt)) + " ago"}
	if p.Source != nil {
		parts = append(parts, string(p.Source.Name))
	}
	if refusesUsage(p) {
		parts = append(parts, usageNotAllowed)
	}
	if p.State == contracts.ProviderStale || p.Freshness.State != contracts.FreshnessCurrent {
		parts = append(parts, string(p.Freshness.State))
	}
	if p.Error != nil {
		parts = append(parts, "last read failed: "+display.ErrorText(p.Error))
	}
	return strings.Join(parts, " · ")
}

func meterLine(meter contracts.Meter, now time.Time, loc *time.Location) string {
	line := meter.Label + ": " + display.MeterValue(meter)
	if reset := display.ResetText(meter.Window, now, loc); reset != "" {
		line += " · " + reset
	}
	return line
}
