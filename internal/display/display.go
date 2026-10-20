// Package display renders snapshot values as short human-readable text.
package display

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"windowseat/internal/contracts"
)

// MeterValue describes a meter's reading in a few words, or "unknown".
func MeterValue(meter contracts.Meter) string {
	switch {
	case meter.UsedPercentage != nil:
		return number(*meter.UsedPercentage) + "% used"
	case meter.Unlimited != nil && *meter.Unlimited:
		return "unlimited"
	case meter.Used != nil && meter.Entitlement != nil && meter.RemainingPercentage != nil:
		return number(*meter.Used) + "/" + number(*meter.Entitlement) + " used, " + number(*meter.RemainingPercentage) + "% left"
	case meter.Used != nil && meter.Entitlement != nil:
		return number(*meter.Used) + "/" + number(*meter.Entitlement) + " used"
	case meter.RemainingPercentage != nil:
		return number(*meter.RemainingPercentage) + "% left"
	case meter.Remaining != nil:
		return number(*meter.Remaining) + " left"
	default:
		return "unknown"
	}
}

// ResetText describes when a window resets relative to now, or "" when unknown.
func ResetText(window *contracts.TimeWindow, now time.Time, loc *time.Location) string {
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
	return "resets " + clock + " (in " + HumanDuration(at.Sub(now)) + ")"
}

// ErrorText joins a provider error with its recovery hint.
func ErrorText(err *contracts.ProviderError) string {
	if err == nil {
		return ""
	}
	if err.RecoveryHint == "" {
		return err.Message
	}
	return strings.TrimSuffix(err.Message, ".") + ". " + err.RecoveryHint
}

// HumanDuration renders d as "<1m", "45m", "2h10m" or "6d3h".
func HumanDuration(d time.Duration) string {
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
