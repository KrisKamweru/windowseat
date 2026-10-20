package claude

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"windowseat/internal/contracts"
)

type rateLimit struct {
	Kind     string   `json:"kind"`
	Percent  *float64 `json:"percent"`
	ResetsAt *string  `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

type extraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Currency     string   `json:"currency"`
}

var knownLabels = map[string]string{
	"session":       "Current session",
	"weekly_all":    "Current week (all models)",
	"weekly_scoped": "Current week",
}

var nonSlugCharacters = regexp.MustCompile(`[^a-z0-9]+`)

// meter maps one provider limit. Unknown kinds keep their provider name, so a
// new Claude limit shows up without a parser change.
func (limit rateLimit) meter(index int, observedAt time.Time) (*contracts.Meter, []contracts.ObservationIssue) {
	if strings.TrimSpace(limit.Kind) == "" {
		return nil, []contracts.ObservationIssue{issue("limit_kind_missing", "", fmt.Sprintf("rate_limits.limits[%d].kind", index))}
	}

	model := limit.model()
	meter := contracts.Meter{
		ID:    meterID(limit.Kind, model),
		Label: meterLabel(limit.Kind, model),
		Kind:  contracts.MeterRollingWindow,
		Model: model,
	}
	if model != "" {
		meter.Kind = contracts.MeterModelLimit
	}

	var issues []contracts.ObservationIssue
	switch {
	case limit.Percent == nil:
		issues = append(issues, issue("percent_missing", meter.ID, "used_percentage"))
	case *limit.Percent < 0 || *limit.Percent > 100:
		issues = append(issues, issue("percent_out_of_range", meter.ID, "used_percentage"))
	default:
		meter.UsedPercentage = limit.Percent
	}

	window, resetIssue := resetWindow(limit.ResetsAt, observedAt)
	meter.Window = window
	if resetIssue != "" {
		issues = append(issues, issue(resetIssue, meter.ID, "window.resets_at"))
	}
	return &meter, issues
}

func (limit rateLimit) model() string {
	if limit.Scope == nil || limit.Scope.Model == nil {
		return ""
	}
	return strings.TrimSpace(limit.Scope.Model.DisplayName)
}

// resetWindow treats a null reset as an allowed omission. An unreadable or
// past reset keeps the raw text and returns the issue code.
func resetWindow(raw *string, observedAt time.Time) (*contracts.TimeWindow, string) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, ""
	}
	resetsAt, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return &contracts.TimeWindow{RawResetAt: *raw}, "reset_time_unreadable"
	}
	if !resetsAt.After(observedAt) {
		return &contracts.TimeWindow{RawResetAt: *raw}, "reset_time_not_future"
	}
	return &contracts.TimeWindow{ResetsAt: &resetsAt}, ""
}

// meterID gives weekly_scoped + "Fable" the ID weekly_fable.
func meterID(kind, model string) string {
	if model == "" {
		return kind
	}
	return strings.TrimSuffix(kind, "_scoped") + "_" + slug(model)
}

func meterLabel(kind, model string) string {
	label, known := knownLabels[kind]
	if !known {
		label = kind
	}
	if model != "" {
		label += " (" + model + ")"
	}
	return label
}

func slug(text string) string {
	return strings.Trim(nonSlugCharacters.ReplaceAllString(strings.ToLower(text), "_"), "_")
}

func (extra extraUsage) meter() (contracts.Meter, []contracts.ObservationIssue) {
	const id = "extra_usage"
	meter := contracts.Meter{
		ID:    id,
		Label: "Extra usage",
		Kind:  contracts.MeterCreditBalance,
		Unit:  extra.Currency,
	}
	var issues []contracts.ObservationIssue
	meter.Used, issues = nonNegative(extra.UsedCredits, id, "used", issues)
	meter.Entitlement, issues = nonNegative(extra.MonthlyLimit, id, "entitlement", issues)
	return meter, issues
}

func nonNegative(value *float64, meterID, field string, issues []contracts.ObservationIssue) (*float64, []contracts.ObservationIssue) {
	if value != nil && *value < 0 {
		return nil, append(issues, issue("value_out_of_range", meterID, field))
	}
	return value, issues
}
