package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"windowseat/internal/providers/claude/ptyusage"
)

var versionPattern = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+)\b`)

type output struct {
	CommandVersion       string                  `json:"command_version"`
	ParserProfile        string                  `json:"parser_profile"`
	ProfilePreferredFrom string                  `json:"profile_preferred_from"`
	VersionBeforeProfile bool                    `json:"version_before_profile,omitempty"`
	CaptureProfile       string                  `json:"capture_profile"`
	ObservedAt           time.Time               `json:"observed_at"`
	DurationMS           float64                 `json:"duration_ms"`
	FiveHour             *meterOutput            `json:"five_hour,omitempty"`
	SevenDay             *meterOutput            `json:"seven_day,omitempty"`
	ModelLimits          []meterOutput           `json:"model_limits"`
	Credits              *ptyusage.CreditsState  `json:"credits,omitempty"`
	Issues               []issueOutput           `json:"issues,omitempty"`
	Cleanup              *ptyusage.CleanupResult `json:"cleanup,omitempty"`
	Audit                *auditOutput            `json:"audit,omitempty"`
	ArtifactErrorCode    string                  `json:"artifact_error_code,omitempty"`
}

type auditOutput struct {
	CommandPath      string                  `json:"command_path"`
	WorkingDirectory string                  `json:"working_directory"`
	Artifact         *ptyusage.ArtifactAudit `json:"artifact"`
}

type auditErrorOutput struct {
	CommandVersion       string       `json:"command_version"`
	ParserProfile        string       `json:"parser_profile"`
	ProfilePreferredFrom string       `json:"profile_preferred_from"`
	VersionBeforeProfile bool         `json:"version_before_profile,omitempty"`
	CaptureProfile       string       `json:"capture_profile"`
	ProbeErrorCode       string       `json:"probe_error_code"`
	ArtifactErrorCode    string       `json:"artifact_error_code,omitempty"`
	Audit                *auditOutput `json:"audit"`
}

type meterOutput struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	Model          string     `json:"model,omitempty"`
	UsedPercentage *float64   `json:"used_percentage,omitempty"`
	ResetsAt       *time.Time `json:"resets_at,omitempty"`
	RawReset       string     `json:"raw_reset,omitempty"`
	TimeZone       string     `json:"time_zone,omitempty"`
}

type issueOutput struct {
	Code    string `json:"code"`
	MeterID string `json:"meter_id,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func main() {
	commandFlag := flag.String("claude", "", "path to the selected native Claude command")
	workingDirectoryFlag := flag.String("working-directory", "", "dedicated probe working directory")
	projectsRootFlag := flag.String("projects-root", "", "Claude projects state directory")
	auditFlag := flag.Bool("audit", false, "retain the probe working directory and session artifact, and report their local paths")
	flag.Parse()

	commandPath, err := resolveCommand(*commandFlag)
	exitOnError(err)
	version, err := detectVersion(commandPath)
	exitOnError(err)
	parserProfile, err := ptyusage.ParserProfileForVersion(version)
	exitOnError(err)
	workingDirectory, created, err := prepareWorkingDirectory(*workingDirectoryFlag)
	exitOnError(err)
	if created && !*auditFlag {
		defer os.Remove(workingDirectory)
	}
	projectsRoot, err := resolveProjectsRoot(*projectsRootFlag)
	exitOnError(err)
	sessionID, err := ptyusage.NewSessionID()
	exitOnError(err)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result, err := ptyusage.Run(ctx, ptyusage.ProbeConfig{
		CommandPath:      commandPath,
		Version:          version,
		WorkingDirectory: workingDirectory,
		ProjectsRoot:     projectsRoot,
		SessionID:        sessionID,
		RetainArtifact:   *auditFlag,
		CheckPolicy: func() error {
			if err := ptyusage.CheckServerManagedPolicyUnavailable(commandPath); err != nil {
				return err
			}
			return ptyusage.CheckManagedPolicyFilesAbsent()
		},
	})
	if err != nil && *auditFlag {
		writeJSON(formatAuditFailure(version, parserProfile, commandPath, workingDirectory, result))
	}
	exitOnError(err)

	modelLimits := make([]meterOutput, len(result.Usage.ModelLimits))
	for index, meter := range result.Usage.ModelLimits {
		modelLimits[index] = *formatMeter(&meter)
	}
	issues := make([]issueOutput, len(result.Usage.Issues))
	for index, issue := range result.Usage.Issues {
		issues[index] = issueOutput{
			Code:    issue.Code,
			MeterID: issue.MeterID,
			Field:   issue.Field,
			Message: issue.Message,
		}
	}
	formatted := output{
		CommandVersion:       version,
		ParserProfile:        result.Usage.ParserProfile,
		ProfilePreferredFrom: result.Usage.ProfilePreferredFrom,
		VersionBeforeProfile: result.Usage.VersionBeforeProfile,
		CaptureProfile:       captureProfileName(result.ParserProfile),
		ObservedAt:           result.ObservedAt,
		DurationMS:           float64(result.EndedAt.Sub(result.StartedAt).Microseconds()) / 1000,
		FiveHour:             formatMeter(result.Usage.FiveHour),
		SevenDay:             formatMeter(result.Usage.SevenDay),
		ModelLimits:          modelLimits,
		Credits:              result.Usage.Credits,
		Issues:               issues,
		Cleanup:              result.Cleanup,
	}
	if *auditFlag {
		formatted.Audit = &auditOutput{
			CommandPath:      commandPath,
			WorkingDirectory: workingDirectory,
			Artifact:         result.ArtifactAudit,
		}
	}
	if result.ArtifactError != nil {
		formatted.ArtifactErrorCode = "artifact_handling_failed"
	}

	writeJSON(formatted)
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	exitOnError(encoder.Encode(value))
}

func formatAuditFailure(
	version string,
	parserProfile ptyusage.ParserProfile,
	commandPath string,
	workingDirectory string,
	result ptyusage.ProbeResult,
) auditErrorOutput {
	failure := auditErrorOutput{
		CommandVersion:       version,
		ParserProfile:        parserProfile.ID,
		ProfilePreferredFrom: parserProfile.PreferredFrom,
		VersionBeforeProfile: parserProfile.VersionBeforeProfile,
		CaptureProfile:       captureProfileName(parserProfile),
		ProbeErrorCode:       "probe_failed",
		Audit: &auditOutput{
			CommandPath:      commandPath,
			WorkingDirectory: workingDirectory,
			Artifact:         result.ArtifactAudit,
		},
	}
	if result.ArtifactError != nil {
		failure.ArtifactErrorCode = "artifact_handling_failed"
	}
	return failure
}

func formatMeter(meter *ptyusage.UsageMeter) *meterOutput {
	if meter == nil {
		return nil
	}
	return &meterOutput{
		ID:             meter.ID,
		Label:          meter.Label,
		Model:          meter.Model,
		UsedPercentage: meter.UsedPercentage,
		ResetsAt:       meter.ResetsAt,
		RawReset:       meter.RawReset,
		TimeZone:       meter.TimeZone,
	}
}

func captureProfileName(profile ptyusage.ParserProfile) string {
	return fmt.Sprintf("screen-reader-%dx%d", profile.Width, profile.Height)
}

func resolveCommand(configured string) (string, error) {
	var path string
	var err error
	if configured != "" {
		path, err = filepath.Abs(configured)
	} else {
		path, err = exec.LookPath("claude")
	}
	if err != nil {
		return "", fmt.Errorf("Claude command was not found")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("Claude command path could not be resolved")
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe") {
		return "", fmt.Errorf("Claude command must be a native Windows executable")
	}
	return path, nil
}

func detectVersion(commandPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	value, err := exec.CommandContext(ctx, commandPath, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("Claude version could not be read")
	}
	match := versionPattern.FindSubmatch(value)
	if match == nil {
		return "", fmt.Errorf("Claude version output was not recognized")
	}
	return string(match[1]), nil
}

func prepareWorkingDirectory(configured string) (string, bool, error) {
	if configured != "" {
		path, err := filepath.Abs(configured)
		return path, false, err
	}
	path, err := os.MkdirTemp("", "windowseat-claude-pty-probe-")
	if err != nil {
		return "", false, fmt.Errorf("probe working directory could not be created")
	}
	return path, true, nil
}

func resolveProjectsRoot(configured string) (string, error) {
	if configured != "" {
		return filepath.Abs(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home directory could not be resolved")
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func exitOnError(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "claude-pty-probe: %v\n", err)
	os.Exit(1)
}
