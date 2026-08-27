package ptyusage

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

var (
	ErrInvalidClaudeVersion      = errors.New("invalid Claude version")
	ErrUnsupportedCaptureProfile = errors.New("unsupported Claude capture profile")
	ErrMalformedUsage            = errors.New("malformed Claude usage view")
	ErrNonZeroSession            = errors.New("Claude usage probe consumed model usage")

	ansiControlPattern = regexp.MustCompile("\\x1b(?:\\[[0-?]*[ -/]*[@-~]|\\][^\\x07]*(?:\\x07|\\x1b\\\\)|[78]|[@-_])")
	percentagePattern  = regexp.MustCompile(`^([0-9]+)% ([0-9]+)% used$`)
	resetPattern       = regexp.MustCompile(`^Resets (.+) \(([^()]+)\)$`)
	modelHeading       = regexp.MustCompile(`^Current week \(([^()]+)\)$`)
	timeOfDayPattern   = regexp.MustCompile(`^([0-9]{1,2})(?::([0-9]{2}))?(am|pm)$`)
	monthDayPattern    = regexp.MustCompile(`^([A-Z][a-z]{2}) ([0-9]{1,2}), ([0-9]{1,2})(?::([0-9]{2}))?(am|pm)$`)
	wallDuration       = regexp.MustCompile(`^Total duration \(wall\): [0-9]+(?:\.[0-9]+)?(?:ms|s|m|h)$`)
)

type ParseInput struct {
	Output       []byte
	Version      string
	Width        int
	Height       int
	ScreenReader bool
	ObservedAt   time.Time
}

type Usage struct {
	ParserProfile        string
	ProfilePreferredFrom string
	VersionBeforeProfile bool
	FiveHour             *UsageMeter
	SevenDay             *UsageMeter
	ModelLimits          []UsageMeter
	Credits              *CreditsState
	Issues               []ParseIssue
}

type CreditsState string

const (
	CreditsOff CreditsState = "off"
	CreditsOn  CreditsState = "on"
)

type UsageMeter struct {
	ID             string
	Label          string
	Model          string
	UsedPercentage *float64
	ResetsAt       *time.Time
	RawReset       string
	TimeZone       string
}

type ParseIssue struct {
	Code    string
	MeterID string
	Field   string
	Message string
}

func Parse(input ParseInput) (Usage, error) {
	profile, err := selectParser(input.Version)
	if err != nil {
		return Usage{}, err
	}
	if input.Width != profile.Width || input.Height != profile.Height || input.ScreenReader != profile.ScreenReader {
		return Usage{}, ErrUnsupportedCaptureProfile
	}
	if input.ObservedAt.IsZero() {
		return Usage{}, fmt.Errorf("%w: observation time is required", ErrMalformedUsage)
	}

	lines, err := normalizeLines(input.Output)
	if err != nil {
		return Usage{}, err
	}
	usage, err := profile.parse(input, lines)
	if err != nil {
		return Usage{}, err
	}
	usage.ParserProfile = profile.ID
	usage.ProfilePreferredFrom = profile.PreferredFrom
	usage.VersionBeforeProfile = profile.VersionBeforeProfile
	return usage, nil
}

func parseClaudeCodeTwoOneTwoFortyFive(input ParseInput, lines []string) (Usage, error) {
	currentSession := lastLine(lines, "Current session")
	if currentSession < 0 {
		return Usage{}, fmt.Errorf("%w: current session meter is missing", ErrMalformedUsage)
	}
	if err := validateEmptySessions(lines[:currentSession]); err != nil {
		return Usage{}, err
	}

	usage, err := parseMeters(lines[currentSession:], input.ObservedAt)
	if err != nil {
		return Usage{}, err
	}
	return usage, nil
}

func normalizeLines(output []byte) ([]string, error) {
	if !utf8.Valid(output) {
		return nil, fmt.Errorf("%w: terminal output is not valid UTF-8", ErrMalformedUsage)
	}
	text := ansiControlPattern.ReplaceAllString(string(output), "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.ContainsRune(text, '\r') {
		return nil, fmt.Errorf("%w: terminal output contains a lone carriage return", ErrMalformedUsage)
	}
	characters := []rune(text)
	for index, character := range characters {
		if character < ' ' && character != '\n' && character != '\t' {
			if character == '\x1b' && index+1 < len(characters) {
				return nil, fmt.Errorf(
					"%w: terminal output contains unknown control U+%04X followed by U+%04X",
					ErrMalformedUsage,
					character,
					characters[index+1],
				)
			}
			return nil, fmt.Errorf("%w: terminal output contains unknown control U+%04X", ErrMalformedUsage, character)
		}
	}

	rawLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, strings.Join(strings.Fields(line), " "))
	}
	return lines, nil
}

func validateEmptySessions(lines []string) error {
	sessionStarts := matchingLines(lines, "Session")
	if len(sessionStarts) == 0 {
		return fmt.Errorf("%w: session summary is missing", ErrMalformedUsage)
	}
	if lastLine(lines[:sessionStarts[len(sessionStarts)-1]], "Settings Status Config Usage Stats") < 0 {
		return fmt.Errorf("%w: usage navigation header is missing", ErrMalformedUsage)
	}

	for _, start := range sessionStarts {
		end := len(lines)
		for index := start + 1; index < len(lines); index++ {
			if lines[index] == "Current session" || lines[index] == "Session" {
				end = index
				break
			}
		}
		if err := validateEmptySession(lines[start+1 : end]); err != nil {
			return err
		}
	}
	return nil
}

func validateEmptySession(lines []string) error {
	found := map[string]bool{
		"cost":         false,
		"api_duration": false,
		"wall":         false,
		"changes":      false,
		"usage":        false,
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "Total cost:"):
			found["cost"] = true
			if line != "Total cost: $0.0000" {
				return fmt.Errorf("%w: total cost is nonzero", ErrNonZeroSession)
			}
		case strings.HasPrefix(line, "Total duration (API):"):
			found["api_duration"] = true
			if line != "Total duration (API): 0s" {
				return fmt.Errorf("%w: API duration is nonzero", ErrNonZeroSession)
			}
		case strings.HasPrefix(line, "Total duration (wall):"):
			found["wall"] = true
			if !wallDuration.MatchString(line) {
				return fmt.Errorf("%w: wall duration changed", ErrMalformedUsage)
			}
		case strings.HasPrefix(line, "Total code changes:"):
			found["changes"] = true
			if line != "Total code changes: 0 lines added, 0 lines removed" {
				return fmt.Errorf("%w: code changes are nonzero", ErrNonZeroSession)
			}
		case strings.HasPrefix(line, "Usage:"):
			found["usage"] = true
			if line != "Usage: 0 input, 0 output, 0 cache read, 0 cache write" {
				return fmt.Errorf("%w: token usage is nonzero", ErrNonZeroSession)
			}
		}
	}
	for name, present := range found {
		if !present {
			return fmt.Errorf("%w: session field %s is missing", ErrMalformedUsage, name)
		}
	}
	return nil
}

func parseMeters(lines []string, observedAt time.Time) (Usage, error) {
	boundary := -1
	for index, line := range lines {
		if isContributingHeading(line) {
			boundary = index
			break
		}
	}
	if boundary < 0 {
		return Usage{}, fmt.Errorf("%w: usage meter boundary is missing", ErrMalformedUsage)
	}

	meterLines := lines[:boundary]
	issues := make([]ParseIssue, 0, 2)
	fiveHour, meterIssues, err := parseNamedMeter(
		meterLines,
		"Current session",
		"five_hour",
		"",
		observedAt,
	)
	if err != nil {
		return Usage{}, err
	}
	issues = append(issues, meterIssues...)
	sevenDay, meterIssues, err := parseNamedMeter(
		meterLines,
		"Current week (all models)",
		"seven_day",
		"",
		observedAt,
	)
	if err != nil {
		return Usage{}, err
	}
	issues = append(issues, meterIssues...)

	modelLimits := make([]UsageMeter, 0, 1)
	modelIDs := make(map[string]struct{})
	modelLabels := make(map[string]struct{})
	for position, line := range meterLines {
		if line == "Current week (all models)" || !strings.HasPrefix(line, "Current week (") {
			continue
		}
		match := modelHeading.FindStringSubmatch(line)
		if match == nil || match[1] == "all models" {
			issues = append(issues, ParseIssue{
				Code:    "meter_heading_unparseable",
				Field:   "meters",
				Message: "Claude displayed a model meter heading that Windowseat could not identify.",
			})
			continue
		}
		model := match[1]
		if len([]rune(model)) > 64 {
			issues = append(issues, ParseIssue{
				Code:    "meter_heading_unparseable",
				Field:   "meters",
				Message: "Claude displayed a model meter label that was too long to use safely.",
			})
			continue
		}
		id := "seven_day_model_" + slug(model)
		if id == "seven_day_model_" {
			issues = append(issues, ParseIssue{
				Code:    "meter_heading_unparseable",
				Field:   "meters",
				Message: "Claude displayed a model meter label that could not form a stable ID.",
			})
			continue
		}
		if _, exists := modelLabels[model]; exists {
			return Usage{}, fmt.Errorf("%w: duplicate model label", ErrMalformedUsage)
		}
		if _, exists := modelIDs[id]; exists {
			return Usage{}, fmt.Errorf("%w: duplicate model ID", ErrMalformedUsage)
		}
		modelLabels[model] = struct{}{}
		modelIDs[id] = struct{}{}
		meter, modelIssues := parseMeterAt(
			meterLines,
			position,
			line,
			id,
			model,
			observedAt,
		)
		modelLimits = append(modelLimits, meter)
		issues = append(issues, modelIssues...)
	}

	credits, footerIssues, err := parseFooter(lines[boundary+1:])
	if err != nil {
		return Usage{}, err
	}
	issues = append(issues, footerIssues...)
	issues = uniqueIssues(issues)

	if !hasObservedMeter(fiveHour) && !hasObservedMeter(sevenDay) {
		observedModel := false
		for index := range modelLimits {
			observedModel = observedModel || hasObservedMeter(&modelLimits[index])
		}
		if !observedModel {
			return Usage{}, fmt.Errorf("%w: no usage meter contained a readable value", ErrMalformedUsage)
		}
	}

	return Usage{
		FiveHour:    fiveHour,
		SevenDay:    sevenDay,
		ModelLimits: modelLimits,
		Credits:     credits,
		Issues:      issues,
	}, nil
}

func uniqueIssues(issues []ParseIssue) []ParseIssue {
	unique := make([]ParseIssue, 0, len(issues))
	seen := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		key := issue.MeterID + "\x00" + issue.Field + "\x00" + issue.Code
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, issue)
	}
	return unique
}

func isContributingHeading(line string) bool {
	return line == "What's contributing to your limits usage?" ||
		line == "What’s contributing to your limits usage?"
}

func parseFooter(lines []string) (*CreditsState, []ParseIssue, error) {
	if lastLine(lines, "Esc to cancel") < 0 {
		return nil, nil, fmt.Errorf("%w: completed usage marker is missing", ErrMalformedUsage)
	}
	credits := -1
	for index, line := range lines {
		if line == "Usage credits" {
			credits = index
			break
		}
	}
	if credits < 0 {
		return nil, []ParseIssue{{
			Code:    "credits_missing",
			Field:   "credits",
			Message: "Claude did not display a usage credits value.",
		}}, nil
	}
	position := skipBlank(lines, credits+1)
	if position < len(lines) {
		switch lines[position] {
		case "Usage credits are off · /usage-credits to turn them on":
			value := CreditsOff
			return &value, nil, nil
		case "Usage credits are on":
			value := CreditsOn
			return &value, nil, nil
		}
	}
	return nil, []ParseIssue{{
		Code:    "credits_unparseable",
		Field:   "credits",
		Message: "Claude displayed a usage credits value that Windowseat could not read.",
	}}, nil
}

func parseNamedMeter(
	lines []string,
	heading string,
	id string,
	model string,
	observedAt time.Time,
) (*UsageMeter, []ParseIssue, error) {
	positions := matchingLines(lines, heading)
	if len(positions) == 0 {
		return nil, []ParseIssue{{
			Code:    "meter_missing",
			MeterID: id,
			Field:   "meter",
			Message: "Claude did not display the expected " + id + " meter.",
		}}, nil
	}
	if len(positions) != 1 {
		return nil, nil, fmt.Errorf("%w: duplicate %s meter", ErrMalformedUsage, id)
	}
	meter, issues := parseMeterAt(lines, positions[0], heading, id, model, observedAt)
	return &meter, issues, nil
}

func parseMeterAt(
	lines []string,
	start int,
	heading string,
	id string,
	model string,
	observedAt time.Time,
) (UsageMeter, []ParseIssue) {
	meter := UsageMeter{ID: id, Label: heading, Model: model}
	issues := make([]ParseIssue, 0, 1)
	position := skipBlank(lines, start+1)

	if position >= len(lines) {
		issues = append(issues, usageIssue(id))
		return meter, append(issues, resetMissingIssue(id))
	}
	if resetPattern.MatchString(lines[position]) {
		issues = append(issues, usageIssue(id))
	} else {
		percentage, percentageErr := parsePercentage(lines[position])
		if percentageErr != nil {
			issues = append(issues, usageIssue(id))
		} else {
			meter.UsedPercentage = &percentage
		}
		position = skipBlank(lines, position+1)
	}
	if position >= len(lines) || !resetPattern.MatchString(lines[position]) {
		return meter, append(issues, resetMissingIssue(id))
	}
	meter.RawReset = lines[position]

	reset, zone, resetErr := parseReset(meter.RawReset, observedAt)
	if resetErr != nil {
		issues = append(issues, resetIssue(id, "reset_time_unparseable", "Claude displayed a reset time that Windowseat could not normalize."))
		return meter, issues
	}
	maximumHorizon := 7*24*time.Hour + time.Minute
	if id == "five_hour" {
		maximumHorizon = 5*time.Hour + time.Minute
	}
	if reset.Sub(observedAt) > maximumHorizon {
		issues = append(issues, resetIssue(id, "reset_time_outside_window", "Claude displayed a reset time outside the meter window."))
		return meter, issues
	}
	meter.ResetsAt = &reset
	meter.TimeZone = zone
	return meter, issues
}

func usageIssue(id string) ParseIssue {
	return ParseIssue{
		Code:    "usage_unparseable",
		MeterID: id,
		Field:   "used_percentage",
		Message: "Claude displayed a meter whose usage percentage Windowseat could not read.",
	}
}

func resetMissingIssue(id string) ParseIssue {
	return resetIssue(id, "reset_time_missing", "Claude did not display a reset time for the meter.")
}

func resetIssue(id, code, message string) ParseIssue {
	return ParseIssue{Code: code, MeterID: id, Field: "window.resets_at", Message: message}
}

func hasObservedMeter(meter *UsageMeter) bool {
	return meter != nil && (meter.UsedPercentage != nil || meter.ResetsAt != nil)
}

func parsePercentage(line string) (float64, error) {
	match := percentagePattern.FindStringSubmatch(line)
	if match == nil || match[1] != match[2] {
		return 0, fmt.Errorf("%w: percentage line changed", ErrMalformedUsage)
	}
	percentage, err := strconv.ParseFloat(match[1], 64)
	if err != nil || percentage < 0 || percentage > 100 {
		return 0, fmt.Errorf("%w: percentage is outside the provider range", ErrMalformedUsage)
	}
	return percentage, nil
}

func parseReset(line string, observedAt time.Time) (time.Time, string, error) {
	match := resetPattern.FindStringSubmatch(line)
	if match == nil {
		return time.Time{}, "", fmt.Errorf("%w: reset line changed", ErrMalformedUsage)
	}
	location, err := time.LoadLocation(match[2])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: reset timezone is unsupported", ErrMalformedUsage)
	}
	observed := observedAt.In(location)

	if clock := timeOfDayPattern.FindStringSubmatch(match[1]); clock != nil {
		hour, minute, valid := clockTime(clock[1], clock[2], clock[3])
		if !valid {
			return time.Time{}, "", fmt.Errorf("%w: reset clock is invalid", ErrMalformedUsage)
		}
		reset := time.Date(observed.Year(), observed.Month(), observed.Day(), hour, minute, 0, 0, location)
		if !validWallTime(reset, observed.Year(), observed.Month(), observed.Day(), hour, minute) || ambiguousWallTime(reset) {
			return time.Time{}, "", fmt.Errorf("%w: reset clock is invalid or ambiguous", ErrMalformedUsage)
		}
		if !reset.After(observedAt) {
			reset = reset.AddDate(0, 0, 1)
		}
		return reset, match[2], nil
	}

	calendar := monthDayPattern.FindStringSubmatch(match[1])
	if calendar == nil {
		return time.Time{}, "", fmt.Errorf("%w: reset time format changed", ErrMalformedUsage)
	}
	month, ok := monthNumber(calendar[1])
	if !ok {
		return time.Time{}, "", fmt.Errorf("%w: reset month is unsupported", ErrMalformedUsage)
	}
	day, _ := strconv.Atoi(calendar[2])
	hour, minute, valid := clockTime(calendar[3], calendar[4], calendar[5])
	if !valid {
		return time.Time{}, "", fmt.Errorf("%w: reset clock is invalid", ErrMalformedUsage)
	}
	reset := time.Date(observed.Year(), month, day, hour, minute, 0, 0, location)
	if !validWallTime(reset, observed.Year(), month, day, hour, minute) || ambiguousWallTime(reset) {
		return time.Time{}, "", fmt.Errorf("%w: reset date is invalid or ambiguous", ErrMalformedUsage)
	}
	if !reset.After(observedAt) {
		reset = time.Date(observed.Year()+1, month, day, hour, minute, 0, 0, location)
		if !validWallTime(reset, observed.Year()+1, month, day, hour, minute) || ambiguousWallTime(reset) {
			return time.Time{}, "", fmt.Errorf("%w: reset date is invalid or ambiguous", ErrMalformedUsage)
		}
	}
	return reset, match[2], nil
}

func clockTime(rawHour, rawMinute, meridiem string) (int, int, bool) {
	hour, _ := strconv.Atoi(rawHour)
	minute, _ := strconv.Atoi(rawMinute)
	if hour < 1 || hour > 12 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	if hour == 12 {
		hour = 0
	}
	if meridiem == "pm" {
		hour += 12
	}
	return hour, minute, true
}

func validWallTime(value time.Time, year int, month time.Month, day, hour, minute int) bool {
	local := value.In(value.Location())
	return local.Year() == year && local.Month() == month && local.Day() == day &&
		local.Hour() == hour && local.Minute() == minute
}

func ambiguousWallTime(value time.Time) bool {
	local := value.In(value.Location())
	for offset := -180; offset <= 180; offset += 15 {
		if offset == 0 {
			continue
		}
		other := value.Add(time.Duration(offset) * time.Minute).In(value.Location())
		if other.Year() == local.Year() && other.Month() == local.Month() && other.Day() == local.Day() &&
			other.Hour() == local.Hour() && other.Minute() == local.Minute() {
			return true
		}
	}
	return false
}

func monthNumber(name string) (time.Month, bool) {
	months := map[string]time.Month{
		"Jan": time.January,
		"Feb": time.February,
		"Mar": time.March,
		"Apr": time.April,
		"May": time.May,
		"Jun": time.June,
		"Jul": time.July,
		"Aug": time.August,
		"Sep": time.September,
		"Oct": time.October,
		"Nov": time.November,
		"Dec": time.December,
	}
	month, ok := months[name]
	return month, ok
}

func lastLine(lines []string, target string) int {
	for index := len(lines) - 1; index >= 0; index-- {
		if lines[index] == target {
			return index
		}
	}
	return -1
}

func matchingLines(lines []string, target string) []int {
	matches := make([]int, 0, 1)
	for index, line := range lines {
		if line == target {
			matches = append(matches, index)
		}
	}
	return matches
}

func skipBlank(lines []string, start int) int {
	for start < len(lines) && lines[start] == "" {
		start++
	}
	return start
}

func slug(value string) string {
	var builder strings.Builder
	underscore := false
	for _, character := range strings.ToLower(value) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			builder.WriteRune(character)
			underscore = false
		case builder.Len() > 0 && !underscore:
			builder.WriteByte('_')
			underscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}
