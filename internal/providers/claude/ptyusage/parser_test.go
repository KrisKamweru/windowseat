package ptyusage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseClaudeCodeTwoOneTwoFortyFive(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "wsl-120x40.txt")
	observedAt := time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)

	usage, err := Parse(validParseInput(output, observedAt))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	assertMeter(t, usage.FiveHour, "five_hour", 27, "2026-08-26T20:00:00+03:00")
	assertMeter(t, usage.SevenDay, "seven_day", 17, "2026-09-01T09:00:00+03:00")
	if len(usage.ModelLimits) != 1 {
		t.Fatalf("len(ModelLimits) = %d, want 1", len(usage.ModelLimits))
	}
	assertMeter(t, &usage.ModelLimits[0], "seven_day_model_fable", 18, "2026-09-01T09:00:00+03:00")
	if usage.ModelLimits[0].Model != "Fable" {
		t.Fatalf("Model = %q, want Fable", usage.ModelLimits[0].Model)
	}
	if usage.Credits == nil || *usage.Credits != CreditsOff {
		t.Fatalf("Credits = %v, want %q", usage.Credits, CreditsOff)
	}
	if usage.ParserProfile != ParserProfileClaudeCodeTwoOne || usage.ProfilePreferredFrom != MinimumTestedVersion {
		t.Fatalf("profile = %q from %q", usage.ParserProfile, usage.ProfilePreferredFrom)
	}
	if len(usage.Issues) != 0 {
		t.Fatalf("Issues = %#v, want none", usage.Issues)
	}
}

func TestParseAcceptsMissingModelLimit(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	observedAt := time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)

	usage, err := Parse(validParseInput(output, observedAt))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(usage.ModelLimits) != 0 {
		t.Fatalf("len(ModelLimits) = %d, want 0", len(usage.ModelLimits))
	}
}

func TestParseUsesLastUsageRefresh(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "duplicate-refresh.txt")
	observedAt := time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)

	usage, err := Parse(validParseInput(output, observedAt))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if usage.FiveHour == nil || usage.FiveHour.UsedPercentage == nil || *usage.FiveHour.UsedPercentage != 27 {
		t.Fatalf("FiveHour = %#v, want final refresh value 27", usage.FiveHour)
	}
}

func TestParseStripsANSIControls(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	output = append([]byte("\x1b[2J\x1b[H\x1b]0;Claude Code\x07\x1b7\x1b8"), output...)
	observedAt := time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)

	if _, err := Parse(validParseInput(output, observedAt)); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestParseAcceptsClaudeTypographicApostrophe(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	output = []byte(strings.Replace(string(output), "What's contributing to your limits usage?", "What’s contributing to your limits usage?", 1))
	observedAt := time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)

	if _, err := Parse(validParseInput(output, observedAt)); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestParseUsesCurrentProfileForHigherVersion(t *testing.T) {
	input := validParseInput(readParserFixture(t, "2.1.245", "wsl-120x40.txt"), time.Now())
	input.Version = "2.1.246"
	usage, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if usage.ParserProfile != ParserProfileClaudeCodeTwoOne || usage.VersionBeforeProfile {
		t.Fatalf("profile = %q, before = %t", usage.ParserProfile, usage.VersionBeforeProfile)
	}
}

func TestParseFallsBackToOldestProfileForLowerVersion(t *testing.T) {
	input := validParseInput(readParserFixture(t, "2.1.245", "wsl-120x40.txt"), time.Now())
	input.Version = "2.1.200"
	usage, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if usage.ParserProfile != ParserProfileClaudeCodeTwoOne || !usage.VersionBeforeProfile {
		t.Fatalf("profile = %q, before = %t", usage.ParserProfile, usage.VersionBeforeProfile)
	}
}

func TestParseRejectsMalformedVersion(t *testing.T) {
	input := validParseInput(readParserFixture(t, "2.1.245", "wsl-120x40.txt"), time.Now())
	input.Version = "Claude 2.1.245"
	_, err := Parse(input)
	if !errors.Is(err, ErrInvalidClaudeVersion) {
		t.Fatalf("Parse() error = %v, want ErrInvalidClaudeVersion", err)
	}
}

func TestParseRejectsUnsupportedCaptureProfile(t *testing.T) {
	input := validParseInput(
		readParserFixture(t, "2.1.245", "wsl-120x40.txt"),
		time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC),
	)
	input.Width = 80

	_, err := Parse(input)
	if !errors.Is(err, ErrUnsupportedCaptureProfile) {
		t.Fatalf("Parse() error = %v, want ErrUnsupportedCaptureProfile", err)
	}
}

func TestParseRejectsNonZeroSessionUsage(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "nonzero-session.txt")
	_, err := Parse(validParseInput(output, time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)))
	if !errors.Is(err, ErrNonZeroSession) {
		t.Fatalf("Parse() error = %v, want ErrNonZeroSession", err)
	}
}

func TestParseKeepsReadableFieldsWhenOnePercentageChanges(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "malformed-layout.txt")
	usage, err := Parse(validParseInput(output, time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if usage.FiveHour == nil || usage.FiveHour.UsedPercentage != nil || usage.FiveHour.ResetsAt == nil {
		t.Fatalf("FiveHour = %#v, want reset without percentage", usage.FiveHour)
	}
	assertMeter(t, usage.SevenDay, "seven_day", 11, "2026-09-01T09:00:00+03:00")
	assertIssue(t, usage.Issues, "usage_unparseable", "five_hour", "used_percentage")
}

func TestParseRejectsMalformedNewestRedraw(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	output = append(output, []byte("\nCurrent session\n20% 20% used\n")...)

	_, err := Parse(validParseInput(output, time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)))
	if !errors.Is(err, ErrMalformedUsage) {
		t.Fatalf("Parse() error = %v, want ErrMalformedUsage", err)
	}
}

func TestParseRejectsInvalidUTF8AndUnknownControls(t *testing.T) {
	base := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	tests := map[string][]byte{
		"invalid_utf8":    append(append([]byte(nil), base...), 0xff),
		"unknown_control": append([]byte{0x01}, base...),
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(validParseInput(output, time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)))
			if !errors.Is(err, ErrMalformedUsage) {
				t.Fatalf("Parse() error = %v, want ErrMalformedUsage", err)
			}
		})
	}
}

func TestParseKeepsUsageWhenResetIsOutsideMeterHorizon(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	output = []byte(strings.Replace(string(output), "Resets 8pm", "Resets 11pm", 1))

	usage, err := Parse(validParseInput(output, time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if usage.FiveHour == nil || usage.FiveHour.UsedPercentage == nil || usage.FiveHour.ResetsAt != nil {
		t.Fatalf("FiveHour = %#v, want usage without normalized reset", usage.FiveHour)
	}
	assertIssue(t, usage.Issues, "reset_time_outside_window", "five_hour", "window.resets_at")
}

func TestParseResolvesResetAcrossMidnight(t *testing.T) {
	observedAt := time.Date(2026, time.August, 26, 20, 30, 0, 0, time.UTC)
	reset, zone, err := parseReset("Resets 1am (Africa/Nairobi)", observedAt)
	if err != nil {
		t.Fatalf("parseReset() error = %v", err)
	}
	if zone != "Africa/Nairobi" || reset.Format(time.RFC3339) != "2026-08-27T01:00:00+03:00" {
		t.Fatalf("parseReset() = %s, %q", reset.Format(time.RFC3339), zone)
	}
}

func TestParseRejectsAmbiguousReset(t *testing.T) {
	observedAt := time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC)
	_, _, err := parseReset("Resets Nov 1, 1:30am (America/New_York)", observedAt)
	if !errors.Is(err, ErrMalformedUsage) {
		t.Fatalf("parseReset() error = %v, want ErrMalformedUsage", err)
	}
}

func TestParseKeepsMetersWhenCreditsLayoutChanges(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	output = []byte(strings.Replace(
		string(output),
		"Usage credits are off · /usage-credits to turn them on",
		"Usage credits are available",
		1,
	))

	usage, err := Parse(validParseInput(output, time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if usage.Credits != nil {
		t.Fatalf("Credits = %v, want nil", usage.Credits)
	}
	assertIssue(t, usage.Issues, "credits_unparseable", "", "credits")
}

func TestParseKeepsResetWhenPercentageChanges(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "wsl-120x40.txt")
	output = []byte(strings.Replace(string(output), "18% 18% used", "usage unavailable", 1))

	usage, err := Parse(validParseInput(output, time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(usage.ModelLimits) != 1 || usage.ModelLimits[0].UsedPercentage != nil || usage.ModelLimits[0].ResetsAt == nil {
		t.Fatalf("ModelLimits = %#v, want reset without percentage", usage.ModelLimits)
	}
	assertIssue(t, usage.Issues, "usage_unparseable", "seven_day_model_fable", "used_percentage")
}

func TestParseDeduplicatesUnknownModelHeadingIssues(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	longModel := strings.Repeat("M", 65)
	insert := "Current week (" + longModel + ")\nCurrent week (" + longModel + ")\n"
	output = []byte(strings.Replace(
		string(output),
		"What's contributing to your limits usage?",
		insert+"What's contributing to your limits usage?",
		1,
	))

	usage, err := Parse(validParseInput(output, time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	count := 0
	for _, issue := range usage.Issues {
		if issue.Code == "meter_heading_unparseable" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("meter_heading_unparseable issues = %d, want 1: %#v", count, usage.Issues)
	}
}

func TestParseRejectsDiagnosticTextWithoutUsableMeterValue(t *testing.T) {
	output := readParserFixture(t, "2.1.245", "without-model-limit.txt")
	changed := strings.ReplaceAll(string(output), "% used", "% unavailable")
	changed = strings.Replace(changed, "Resets 8pm", "Resets someday", 1)
	changed = strings.Replace(changed, "Resets Sep 1, 9am", "Resets eventually", 1)

	_, err := Parse(validParseInput(
		[]byte(changed),
		time.Date(2026, time.August, 26, 13, 54, 9, 0, time.UTC),
	))
	if !errors.Is(err, ErrMalformedUsage) {
		t.Fatalf("Parse() error = %v, want ErrMalformedUsage", err)
	}
}

func validParseInput(output []byte, observedAt time.Time) ParseInput {
	return ParseInput{
		Output:       output,
		Version:      MinimumTestedVersion,
		Width:        CaptureWidth,
		Height:       CaptureHeight,
		ScreenReader: true,
		ObservedAt:   observedAt,
	}
}

func assertMeter(t *testing.T, meter *UsageMeter, id string, percentage float64, reset string) {
	t.Helper()
	if meter == nil {
		t.Fatalf("meter = nil, want %q", id)
	}
	if meter.ID != id {
		t.Errorf("ID = %q, want %q", meter.ID, id)
	}
	if meter.UsedPercentage == nil || *meter.UsedPercentage != percentage {
		t.Errorf("UsedPercentage = %v, want %v", meter.UsedPercentage, percentage)
	}
	if meter.ResetsAt == nil {
		t.Fatalf("ResetsAt = nil, want %q", reset)
	}
	if got := meter.ResetsAt.Format(time.RFC3339); got != reset {
		t.Errorf("ResetsAt = %q, want %q", got, reset)
	}
}

func assertIssue(t *testing.T, issues []ParseIssue, code, meterID, field string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.MeterID == meterID && issue.Field == field {
			return
		}
	}
	t.Fatalf("issues = %#v, want %q for %q.%q", issues, code, meterID, field)
}

func readParserFixture(t *testing.T, version, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", version, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return data
}
