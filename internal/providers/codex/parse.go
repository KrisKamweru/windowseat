package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/jsonfield"
)

const defaultLimitID = "codex"

var errNoUsableMeters = errors.New("rate-limit response has no usable meters")

// The response also carries accountId and rateLimitResetCredits. They are
// deliberately not decoded so they cannot reach a snapshot.
type rateLimitsResult struct {
	OrdinaryUsageAllowed jsonfield.Field[bool]    `json:"ordinaryUsageAllowed"`
	RateLimits           *limitSnapshot           `json:"rateLimits"`
	RateLimitsByLimitID  map[string]limitSnapshot `json:"rateLimitsByLimitId"`
}

type limitSnapshot struct {
	LimitID         jsonfield.Field[string] `json:"limitId"`
	LimitName       jsonfield.Field[string] `json:"limitName"`
	NormalModelSlug jsonfield.Field[string] `json:"normalModelSlug"`
	PlanType        jsonfield.Field[string] `json:"planType"`
	Primary         *window                 `json:"primary"`
	Secondary       *window                 `json:"secondary"`
	Credits         *credits                `json:"credits"`
}

type window struct {
	UsedPercent        jsonfield.Field[float64] `json:"usedPercent"`
	WindowDurationMins jsonfield.Field[int64]   `json:"windowDurationMins"`
	ResetsAt           jsonfield.Field[int64]   `json:"resetsAt"`
}

type credits struct {
	HasCredits jsonfield.Field[bool]   `json:"hasCredits"`
	Unlimited  jsonfield.Field[bool]   `json:"unlimited"`
	Balance    jsonfield.Field[string] `json:"balance"`
}

type reading struct {
	plan         string
	usageAllowed *bool
	meters       []contracts.Meter
	issues       []contracts.ObservationIssue
}

// parseRateLimits maps an account/rateLimits/read result to meters. Fields
// that fail to parse become issues; a response with no usable meter fails.
func parseRateLimits(result json.RawMessage, observedAt time.Time) (reading, error) {
	var response rateLimitsResult
	if err := json.Unmarshal(result, &response); err != nil {
		return reading{}, fmt.Errorf("decode rate limits: %w", err)
	}

	limits := response.limits()
	if len(limits) == 0 {
		return reading{}, errNoUsableMeters
	}

	parsed := reading{plan: limits[0].PlanType.OrZero(), usageAllowed: response.OrdinaryUsageAllowed.Value}
	if limits[0].PlanType.Unreadable {
		parsed.issues = append(parsed.issues, unreadable("", "plan"))
	}
	if response.OrdinaryUsageAllowed.Unreadable {
		parsed.issues = append(parsed.issues, unreadable("", "usage_allowed"))
	}
	seen := map[string]bool{}
	for _, limit := range limits {
		meters, issues := limit.toMeters(observedAt)
		parsed.issues = append(parsed.issues, issues...)
		for _, meter := range meters {
			if seen[meter.ID] {
				parsed.issues = append(parsed.issues, issue("duplicate_meter", meter.ID, "id", "The provider returned two windows with the same duration."))
				continue
			}
			seen[meter.ID] = true
			parsed.meters = append(parsed.meters, meter)
		}
	}
	if !slices.ContainsFunc(parsed.meters, hasReading) {
		return reading{}, errNoUsableMeters
	}
	return parsed, nil
}

// hasReading reports whether a meter carries a value an agent can act on.
func hasReading(meter contracts.Meter) bool {
	hasReset := meter.Window != nil && meter.Window.ResetsAt != nil
	isUnlimited := meter.Unlimited != nil && *meter.Unlimited
	return meter.UsedPercentage != nil || meter.Remaining != nil || isUnlimited || hasReset
}

// limits lists the default Codex limit first, then any other limits by ID.
// Older app-servers only return the single rateLimits view.
func (response rateLimitsResult) limits() []limitSnapshot {
	if len(response.RateLimitsByLimitID) == 0 {
		if response.RateLimits == nil {
			return nil
		}
		return []limitSnapshot{*response.RateLimits}
	}

	ids := make([]string, 0, len(response.RateLimitsByLimitID))
	for id := range response.RateLimitsByLimitID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if (ids[i] == defaultLimitID) != (ids[j] == defaultLimitID) {
			return ids[i] == defaultLimitID
		}
		return ids[i] < ids[j]
	})

	limits := make([]limitSnapshot, len(ids))
	for index, id := range ids {
		limit := response.RateLimitsByLimitID[id]
		limit.LimitID = jsonfield.Field[string]{Value: &id}
		limits[index] = limit
	}
	return limits
}

func (limit limitSnapshot) toMeters(observedAt time.Time) ([]contracts.Meter, []contracts.ObservationIssue) {
	var meters []contracts.Meter
	var issues []contracts.ObservationIssue
	if limit.LimitID.Unreadable {
		issues = append(issues, unreadable("", "limit_id"))
	}
	if limit.LimitName.Unreadable {
		issues = append(issues, unreadable("", "label"))
	}
	slots := []struct {
		name   string
		window *window
	}{{"primary", limit.Primary}, {"secondary", limit.Secondary}}

	for _, slot := range slots {
		if slot.window == nil {
			continue
		}
		meter, windowIssues := limit.windowMeter(slot.name, *slot.window, observedAt)
		meters = append(meters, meter)
		issues = append(issues, windowIssues...)
	}
	if limit.Credits != nil && limit.Credits.present() {
		meter, creditIssues := limit.creditMeter(*limit.Credits)
		meters = append(meters, meter)
		issues = append(issues, creditIssues...)
	}
	return meters, issues
}

func (limit limitSnapshot) windowMeter(slot string, source window, observedAt time.Time) (contracts.Meter, []contracts.ObservationIssue) {
	id, label := windowName(slot, source.WindowDurationMins.Value)
	meter := contracts.Meter{
		ID:     limit.meterID(id),
		Label:  limit.meterLabel(label),
		Kind:   contracts.MeterRollingWindow,
		Model:  limit.NormalModelSlug.OrZero(),
		Window: &contracts.TimeWindow{},
	}
	var issues []contracts.ObservationIssue
	if limit.NormalModelSlug.Unreadable {
		issues = append(issues, unreadable(meter.ID, "model"))
	}

	switch duration := source.WindowDurationMins.Value; {
	case source.WindowDurationMins.Unreadable:
		issues = append(issues, unreadable(meter.ID, "window.duration_seconds"))
	case duration != nil && *duration > 0:
		seconds := *duration * 60
		meter.Window.DurationSeconds = &seconds
	default:
		issues = append(issues, issue("missing_field", meter.ID, "window.duration_seconds", "The provider did not report a usable window duration."))
	}

	switch percent := source.UsedPercent.Value; {
	case source.UsedPercent.Unreadable:
		issues = append(issues, unreadable(meter.ID, "used_percentage"))
	case percent == nil:
		issues = append(issues, issue("missing_field", meter.ID, "used_percentage", "The provider did not report a used percentage."))
	case *percent < 0 || *percent > 100 || math.IsNaN(*percent):
		issues = append(issues, issue("out_of_range", meter.ID, "used_percentage", "The provider reported a used percentage outside 0 to 100."))
	default:
		meter.UsedPercentage = percent
	}

	if source.ResetsAt.Unreadable {
		issues = append(issues, unreadable(meter.ID, "window.resets_at"))
	}
	if source.ResetsAt.Value != nil {
		resetsAt := time.Unix(*source.ResetsAt.Value, 0).UTC()
		if resetsAt.After(observedAt) {
			meter.Window.ResetsAt = &resetsAt
		} else {
			meter.Window.RawResetAt = resetsAt.Format(time.RFC3339)
			issues = append(issues, issue("reset_time_not_future", meter.ID, "window.resets_at", "The provider reset time was not later than the observation time."))
		}
	}

	if *meter.Window == (contracts.TimeWindow{}) {
		meter.Window = nil
	}
	return meter, issues
}

func (limit limitSnapshot) creditMeter(source credits) (contracts.Meter, []contracts.ObservationIssue) {
	meter := contracts.Meter{
		ID:        limit.meterID("credits"),
		Label:     limit.meterLabel("Credits"),
		Kind:      contracts.MeterCreditBalance,
		Unit:      "credits",
		Unlimited: source.Unlimited.Value,
	}
	var issues []contracts.ObservationIssue
	if source.Unlimited.Unreadable {
		issues = append(issues, unreadable(meter.ID, "unlimited"))
	}
	if source.Balance.Value == nil && !source.Balance.Unreadable {
		return meter, issues
	}
	balance, err := strconv.ParseFloat(source.Balance.OrZero(), 64)
	if err != nil || balance < 0 || math.IsInf(balance, 0) || math.IsNaN(balance) {
		return meter, append(issues, issue("unreadable_field", meter.ID, "remaining", "The provider credit balance was not a non-negative number."))
	}
	meter.Remaining = &balance
	return meter, issues
}

// present reports whether the provider says a credit balance exists. A flag it
// sent but could not be read still shows the meter, so the issue has a home.
func (source credits) present() bool {
	return source.HasCredits.OrZero() || source.Unlimited.OrZero() || source.HasCredits.Unreadable || source.Unlimited.Unreadable
}

func windowName(slot string, durationMins *int64) (id, label string) {
	if durationMins == nil || *durationMins <= 0 {
		return slot, strings.ToUpper(slot[:1]) + slot[1:] + " window"
	}
	switch *durationMins {
	case 300:
		return "five_hour", "Five-hour window"
	case 10080:
		return "seven_day", "Seven-day window"
	default:
		return fmt.Sprintf("window_%dm", *durationMins), fmt.Sprintf("%d-minute window", *durationMins)
	}
}

func (limit limitSnapshot) isDefault() bool {
	id := limit.LimitID.OrZero()
	return id == "" || id == defaultLimitID
}

func (limit limitSnapshot) meterID(id string) string {
	if limit.isDefault() {
		return id
	}
	return limit.LimitID.OrZero() + "_" + id
}

func (limit limitSnapshot) meterLabel(label string) string {
	if limit.isDefault() {
		return label
	}
	name := limit.LimitName.OrZero()
	if name == "" {
		name = limit.LimitID.OrZero()
	}
	return name + " " + strings.ToLower(label[:1]) + label[1:]
}

func issue(code, meterID, field, message string) contracts.ObservationIssue {
	return contracts.ObservationIssue{Code: code, MeterID: meterID, Field: field, Message: message}
}

func unreadable(meterID, field string) contracts.ObservationIssue {
	return issue("unreadable_field", meterID, field, "The provider returned a value of the wrong type.")
}
