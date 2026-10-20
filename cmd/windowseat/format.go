package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"windowseat/internal/contracts"
)

func formatUsage(w io.Writer, snapshot contracts.Snapshot, now time.Time, loc *time.Location) {
	for _, p := range snapshot.Providers {
		name := fmt.Sprintf("%-8s", p.Provider)
		if len(p.Meters) == 0 {
			fmt.Fprintln(w, strings.TrimSpace(name+"["+string(p.State)+"] "+errorText(p.Error)))
			continue
		}
		for _, meter := range p.Meters {
			fmt.Fprintln(w, name+meterLine(meter, now, loc)+markers(p))
		}
		if p.Error != nil {
			fmt.Fprintln(w, name+"last read failed: "+errorText(p.Error))
		}
	}
}

func meterLine(meter contracts.Meter, now time.Time, loc *time.Location) string {
	line := fmt.Sprintf("%-23s", meter.Label)
	reset := resetText(meter.Window, now, loc)
	if reset == "" {
		return line + meterValue(meter)
	}
	return line + fmt.Sprintf("%-12s", meterValue(meter)) + reset
}

func meterValue(meter contracts.Meter) string {
	switch {
	case meter.UsedPercentage != nil:
		return number(*meter.UsedPercentage) + "% used"
	case meter.RemainingPercentage != nil && (meter.Unlimited == nil || !*meter.Unlimited):
		return number(*meter.RemainingPercentage) + "% left"
	case meter.Unlimited != nil && *meter.Unlimited:
		return "unlimited"
	case meter.Used != nil && meter.Entitlement != nil:
		return number(*meter.Used) + "/" + number(*meter.Entitlement) + " used"
	case meter.Remaining != nil:
		return number(*meter.Remaining) + " left"
	default:
		return "unknown"
	}
}

func resetText(window *contracts.TimeWindow, now time.Time, loc *time.Location) string {
	if window == nil || window.ResetsAt == nil {
		return ""
	}
	at := window.ResetsAt.In(loc)
	clock := at.Format("15:04")
	if at.Sub(now) >= 24*time.Hour {
		clock = at.Format("Jan 2 15:04")
	}
	if !at.After(now) {
		return "reset " + clock + " (passed)"
	}
	return "resets " + clock + " (in " + humanDuration(at.Sub(now)) + ")"
}

// markers flags anything an agent or person should not take at face value.
func markers(p contracts.ProviderSnapshot) string {
	var flags []string
	if p.State != contracts.ProviderHealthy {
		flags = append(flags, string(p.State))
	}
	if p.Freshness.State != contracts.FreshnessCurrent && string(p.Freshness.State) != string(p.State) {
		flags = append(flags, string(p.Freshness.State))
	}
	if len(flags) == 0 {
		return ""
	}
	return "  [" + strings.Join(flags, ", ") + "]"
}

func errorText(err *contracts.ProviderError) string {
	if err == nil {
		return ""
	}
	if err.RecoveryHint == "" {
		return err.Message
	}
	return strings.TrimSuffix(err.Message, ".") + ". " + err.RecoveryHint
}

func formatHealth(w io.Writer, report contracts.HealthReport, loc *time.Location) {
	readiness := "not ready"
	if report.Ready {
		readiness = "ready"
	}
	if report.Service.Version != "" {
		readiness = "version " + report.Service.Version + ", " + readiness
	}
	fmt.Fprintf(w, "%-8s%s  %s\n", "service", report.Service.State, readiness)

	for _, p := range report.Providers {
		parts := []string{string(p.OperationalState)}
		if p.State != contracts.ProviderHealthy {
			parts[0] += " [" + string(p.State) + "]"
		}
		parts = append(parts, "checked "+p.CheckedAt.In(loc).Format("15:04"))
		if p.LastError != nil {
			parts = append(parts, "last error: "+errorText(p.LastError))
		}
		fmt.Fprintf(w, "%-8s%s\n", p.Provider, strings.Join(parts, "  "))
	}
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	d = d.Round(time.Minute)
	days, hours, minutes := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

func number(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
