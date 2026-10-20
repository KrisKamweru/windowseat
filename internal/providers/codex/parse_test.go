package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"windowseat/internal/contracts"
)

func fixtureResult(t *testing.T, version string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", version, "rate_limits_read.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &reply); err != nil {
		t.Fatal(err)
	}
	return reply.Result
}

func meterByID(t *testing.T, meters []contracts.Meter, id string) contracts.Meter {
	t.Helper()
	for _, meter := range meters {
		if meter.ID == id {
			return meter
		}
	}
	t.Fatalf("meter %q missing from %+v", id, meters)
	return contracts.Meter{}
}

func assertWindow(t *testing.T, meter contracts.Meter, label string, used float64, durationSeconds int64, resetsAt time.Time) {
	t.Helper()
	if meter.Label != label || meter.Kind != contracts.MeterRollingWindow {
		t.Errorf("%s: label %q kind %q", meter.ID, meter.Label, meter.Kind)
	}
	if meter.UsedPercentage == nil || *meter.UsedPercentage != used {
		t.Errorf("%s: used_percentage = %v, want %v", meter.ID, meter.UsedPercentage, used)
	}
	if meter.Window == nil || meter.Window.DurationSeconds == nil || *meter.Window.DurationSeconds != durationSeconds {
		t.Errorf("%s: window = %+v, want duration %d", meter.ID, meter.Window, durationSeconds)
	}
	if meter.Window == nil || meter.Window.ResetsAt == nil || !meter.Window.ResetsAt.Equal(resetsAt) {
		t.Errorf("%s: window = %+v, want reset %s", meter.ID, meter.Window, resetsAt)
	}
}

var probeTime = time.Date(2026, 8, 24, 19, 59, 10, 0, time.UTC)

func TestParseCurrentShapeIdentifiesWindowsByDuration(t *testing.T) {
	reading, err := parseRateLimits(fixtureResult(t, "0.163.0"), probeTime)

	if err != nil {
		t.Fatal(err)
	}
	if reading.plan != "plus" || len(reading.issues) != 0 || len(reading.meters) != 2 {
		t.Fatalf("reading = %+v", reading)
	}
	assertWindow(t, meterByID(t, reading.meters, "five_hour"), "Five-hour window", 89, 5*3600, time.Unix(1792505356, 0))
	assertWindow(t, meterByID(t, reading.meters, "seven_day"), "Seven-day window", 14, 7*24*3600, time.Unix(1793092156, 0))
}

func TestParseOlderShapeWithSevenDayPrimaryAndNoSecondary(t *testing.T) {
	reading, err := parseRateLimits(fixtureResult(t, "0.149.1"), probeTime)

	if err != nil {
		t.Fatal(err)
	}
	if reading.plan != "plus" || len(reading.issues) != 0 || len(reading.meters) != 1 {
		t.Fatalf("reading = %+v", reading)
	}
	assertWindow(t, reading.meters[0], "Seven-day window", 14, 7*24*3600, time.Unix(1788167165, 0))
}

func TestParseNamesUnknownDurationsByMinutes(t *testing.T) {
	result := json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":3,"windowDurationMins":1440}}}`)

	reading, err := parseRateLimits(result, probeTime)

	if err != nil {
		t.Fatal(err)
	}
	meter := meterByID(t, reading.meters, "window_1440m")
	if meter.Label != "1440-minute window" || meter.Window.ResetsAt != nil {
		t.Errorf("meter = %+v", meter)
	}
}

func TestParsePrefixesWindowsFromOtherLimits(t *testing.T) {
	result := json.RawMessage(`{
		"rateLimits":{"limitId":"codex","primary":{"usedPercent":1,"windowDurationMins":300}},
		"rateLimitsByLimitId":{
			"spark":{"limitId":"spark","limitName":"Spark","normalModelSlug":"gpt-spark","primary":{"usedPercent":40,"windowDurationMins":300}},
			"codex":{"limitId":"codex","primary":{"usedPercent":1,"windowDurationMins":300}}
		}}`)

	reading, err := parseRateLimits(result, probeTime)

	if err != nil {
		t.Fatal(err)
	}
	if len(reading.meters) != 2 || reading.meters[0].ID != "five_hour" {
		t.Fatalf("meters = %+v", reading.meters)
	}
	spark := meterByID(t, reading.meters, "spark_five_hour")
	if spark.Label != "Spark five-hour window" || spark.Model != "gpt-spark" || *spark.UsedPercentage != 40 {
		t.Errorf("spark = %+v", spark)
	}
}

func TestParseAddsCreditMeterOnlyWhenCreditsExist(t *testing.T) {
	result := json.RawMessage(`{"rateLimits":{
		"primary":{"usedPercent":1,"windowDurationMins":300},
		"credits":{"hasCredits":true,"unlimited":false,"balance":"12.5"}}}`)

	reading, err := parseRateLimits(result, probeTime)

	if err != nil {
		t.Fatal(err)
	}
	credits := meterByID(t, reading.meters, "credits")
	if credits.Kind != contracts.MeterCreditBalance || credits.Remaining == nil || *credits.Remaining != 12.5 {
		t.Errorf("credits = %+v", credits)
	}
	if credits.Unlimited == nil || *credits.Unlimited {
		t.Errorf("credits.unlimited = %v", credits.Unlimited)
	}
}

func TestParseReportsPastResetAsIssue(t *testing.T) {
	result := json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":5,"windowDurationMins":300,"resetsAt":1000}}}`)

	reading, err := parseRateLimits(result, probeTime)

	if err != nil {
		t.Fatal(err)
	}
	meter := meterByID(t, reading.meters, "five_hour")
	if meter.Window.ResetsAt != nil || meter.Window.RawResetAt != "1970-01-01T00:16:40Z" {
		t.Errorf("window = %+v", meter.Window)
	}
	if len(reading.issues) != 1 || reading.issues[0].Code != "reset_time_not_future" || reading.issues[0].MeterID != "five_hour" {
		t.Errorf("issues = %+v", reading.issues)
	}
}

func TestParseReportsOutOfRangePercentageAsIssue(t *testing.T) {
	result := json.RawMessage(`{"rateLimits":{
		"primary":{"usedPercent":140,"windowDurationMins":300},
		"secondary":{"usedPercent":10,"windowDurationMins":10080}}}`)

	reading, err := parseRateLimits(result, probeTime)

	if err != nil {
		t.Fatal(err)
	}
	if meterByID(t, reading.meters, "five_hour").UsedPercentage != nil {
		t.Error("out-of-range percentage was kept")
	}
	if len(reading.issues) != 1 || reading.issues[0].Field != "used_percentage" {
		t.Errorf("issues = %+v", reading.issues)
	}
}

func TestParseKeepsOtherFieldsWhenOneIsWronglyTyped(t *testing.T) {
	result := json.RawMessage(`{"ordinaryUsageAllowed":"no","rateLimits":{
		"planType":7,
		"primary":{"usedPercent":"89","windowDurationMins":300,"resetsAt":1792505356},
		"secondary":{"usedPercent":14,"windowDurationMins":10080,"resetsAt":"soon"},
		"credits":{"hasCredits":true,"unlimited":false,"balance":12}}}`)

	reading, err := parseRateLimits(result, probeTime)

	if err != nil {
		t.Fatal(err)
	}
	fiveHour := meterByID(t, reading.meters, "five_hour")
	if fiveHour.UsedPercentage != nil || fiveHour.Window == nil || fiveHour.Window.ResetsAt == nil {
		t.Errorf("five_hour = %+v, want reset kept and percentage dropped", fiveHour)
	}
	sevenDay := meterByID(t, reading.meters, "seven_day")
	if sevenDay.UsedPercentage == nil || *sevenDay.UsedPercentage != 14 || sevenDay.Window.ResetsAt != nil {
		t.Errorf("seven_day = %+v, want percentage kept and reset dropped", sevenDay)
	}
	if credits := meterByID(t, reading.meters, "credits"); credits.Remaining != nil {
		t.Errorf("credits remaining = %v, want dropped", *credits.Remaining)
	}
	if reading.plan != "" || reading.usageAllowed != nil {
		t.Errorf("plan = %q, usage allowed = %v, want both dropped", reading.plan, reading.usageAllowed)
	}
	got := map[string]bool{}
	for _, issue := range reading.issues {
		got[issue.MeterID+"/"+issue.Field] = true
	}
	for _, want := range []string{"/plan", "/usage_allowed", "five_hour/used_percentage", "seven_day/window.resets_at", "credits/remaining"} {
		if !got[want] {
			t.Errorf("missing issue %s in %+v", want, reading.issues)
		}
	}
}

func TestParseRejectsResponsesWithoutWindows(t *testing.T) {
	for name, result := range map[string]string{
		"not json":     `[`,
		"null limits":  `{"rateLimits":null}`,
		"null windows": `{"rateLimits":{"primary":null,"secondary":null}}`,
		"no readings":  `{"rateLimits":{"primary":{"windowDurationMins":300},"secondary":{"usedPercent":140,"windowDurationMins":10080,"resetsAt":1000}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseRateLimits(json.RawMessage(result), probeTime)
			if !errors.Is(err, errNoUsableMeters) && name != "not json" {
				t.Errorf("err = %v, want errNoUsableMeters", err)
			}
			if err == nil {
				t.Error("expected an error")
			}
		})
	}
}
