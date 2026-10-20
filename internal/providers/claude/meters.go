package claude

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/jsonfield"
)

type rateLimit struct {
	Kind     jsonfield.Field[string]     `json:"kind"`
	Percent  jsonfield.Field[float64]    `json:"percent"`
	ResetsAt jsonfield.Field[string]     `json:"resets_at"`
	Scope    jsonfield.Field[limitScope] `json:"scope"`
}

type limitScope struct {
	Model *struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
}

type extraUsage struct {
	IsEnabled    jsonfield.Field[bool]    `json:"is_enabled"`
	MonthlyLimit jsonfield.Field[float64] `json:"monthly_limit"`
	UsedCredits  jsonfield.Field[float64] `json:"used_credits"`
	Currency     jsonfield.Field[string]  `json:"currency"`
}

var knownLabels = map[string]string{
	"session":       "Current session",
	"weekly_all":    "Current week (all models)",
	"weekly_scoped": "Current week",
}

var nonSlugCharacters = regexp.MustCompile(`[^a-z0-9]+`)

// meter maps one provider limit. Unknown kinds keep their provider name, so a
// new Claude limit shows up without a parser change. A limit whose kind or
// scope cannot be read has no identity and becomes only an issue.
func (limit rateLimit) meter(index int, observedAt time.Time) (*contracts.Meter, []contracts.ObservationIssue) {
	path := fmt.Sprintf("rate_limits.limits[%d]", index)
	switch {
	case limit.Kind.Unreadable:
		return nil, []contracts.ObservationIssue{issue("field_unreadable", "", path+".kind")}
	case limit.Kind.Value == nil || strings.TrimSpace(*limit.Kind.Value) == "":
		return nil, []contracts.ObservationIssue{issue("limit_kind_missing", "", path+".kind")}
	case limit.Scope.Unreadable:
		return nil, []contracts.ObservationIssue{issue("field_unreadable", "", path+".scope")}
	}

	kind := *limit.Kind.Value
	model := limit.model()
	meter := contracts.Meter{
		ID:    meterID(kind, model),
		Label: meterLabel(kind, model),
		Kind:  contracts.MeterRollingWindow,
		Model: model,
	}
	if model != "" {
		meter.Kind = contracts.MeterModelLimit
	}

	var issues []contracts.ObservationIssue
	switch percent := limit.Percent.Value; {
	case limit.Percent.Unreadable:
		issues = append(issues, issue("field_unreadable", meter.ID, "used_percentage"))
	case percent == nil:
		issues = append(issues, issue("percent_missing", meter.ID, "used_percentage"))
	case *percent < 0 || *percent > 100:
		issues = append(issues, issue("percent_out_of_range", meter.ID, "used_percentage"))
	default:
		meter.UsedPercentage = percent
	}

	if limit.ResetsAt.Unreadable {
		return &meter, append(issues, issue("field_unreadable", meter.ID, "window.resets_at"))
	}
	window, resetIssue := resetWindow(limit.ResetsAt.Value, observedAt)
	meter.Window = window
	if resetIssue != "" {
		issues = append(issues, issue(resetIssue, meter.ID, "window.resets_at"))
	}
	return &meter, issues
}

func (limit rateLimit) model() string {
	scope := limit.Scope.Value
	if scope == nil || scope.Model == nil {
		return ""
	}
	return strings.TrimSpace(scope.Model.DisplayName)
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

const extraUsageID = "extra_usage"

// meters returns the extra usage credit meter when the provider reports it enabled.
func (extra *extraUsage) meters() ([]contracts.Meter, []contracts.ObservationIssue) {
	switch {
	case extra == nil:
		return nil, nil
	case extra.IsEnabled.Unreadable:
		return nil, []contracts.ObservationIssue{issue("field_unreadable", extraUsageID, "rate_limits.extra_usage.is_enabled")}
	case extra.IsEnabled.Value == nil || !*extra.IsEnabled.Value:
		return nil, nil
	}
	meter, issues := extra.meter()
	return []contracts.Meter{meter}, issues
}

func (extra extraUsage) meter() (contracts.Meter, []contracts.ObservationIssue) {
	meter := contracts.Meter{
		ID:    extraUsageID,
		Label: "Extra usage",
		Kind:  contracts.MeterCreditBalance,
	}
	var issues []contracts.ObservationIssue
	if extra.Currency.Unreadable {
		issues = append(issues, issue("field_unreadable", extraUsageID, "unit"))
	} else if extra.Currency.Value != nil {
		meter.Unit = *extra.Currency.Value
	}
	meter.Used, issues = nonNegative(extra.UsedCredits, extraUsageID, "used", issues)
	meter.Entitlement, issues = nonNegative(extra.MonthlyLimit, extraUsageID, "entitlement", issues)
	return meter, issues
}

func nonNegative(field jsonfield.Field[float64], meterID, name string, issues []contracts.ObservationIssue) (*float64, []contracts.ObservationIssue) {
	switch {
	case field.Unreadable:
		return nil, append(issues, issue("field_unreadable", meterID, name))
	case field.Value != nil && *field.Value < 0:
		return nil, append(issues, issue("value_out_of_range", meterID, name))
	default:
		return field.Value, issues
	}
}
