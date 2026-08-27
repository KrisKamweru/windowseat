package main

import (
	"errors"
	"os"
	"testing"

	"windowseat/internal/providers/claude/ptyusage"
)

func TestPrepareWorkingDirectoryCreatesUniqueDirectories(t *testing.T) {
	first, firstCreated, err := prepareWorkingDirectory("")
	if err != nil {
		t.Fatalf("prepareWorkingDirectory(first) error = %v", err)
	}
	t.Cleanup(func() { os.Remove(first) })
	second, secondCreated, err := prepareWorkingDirectory("")
	if err != nil {
		t.Fatalf("prepareWorkingDirectory(second) error = %v", err)
	}
	t.Cleanup(func() { os.Remove(second) })

	if !firstCreated || !secondCreated {
		t.Fatalf("created = %t, %t, want true, true", firstCreated, secondCreated)
	}
	if first == second {
		t.Fatalf("directories match: %q", first)
	}
}

func TestFormatAuditFailurePreservesInspectionPaths(t *testing.T) {
	artifact := &ptyusage.ArtifactAudit{
		CandidatePath:        `C:\Users\person\.claude\projects\probe\session.jsonl`,
		ProjectDirectory:     `C:\Users\person\.claude\projects\probe`,
		Present:              true,
		EmptySessionVerified: true,
	}
	result := ptyusage.ProbeResult{
		ArtifactAudit: artifact,
		ArtifactError: errors.New("test error"),
	}

	failure := formatAuditFailure(
		"2.1.245",
		ptyusage.ParserProfile{
			ID:            ptyusage.ParserProfileClaudeCodeTwoOne,
			PreferredFrom: ptyusage.MinimumTestedVersion,
			Width:         ptyusage.CaptureWidth,
			Height:        ptyusage.CaptureHeight,
		},
		`C:\Program Files\Claude\claude.exe`,
		`C:\Users\person\AppData\Local\Temp\windowseat-probe`,
		result,
	)

	if failure.ProbeErrorCode != "probe_failed" || failure.ArtifactErrorCode != "artifact_handling_failed" {
		t.Fatalf("formatAuditFailure() error codes = %q, %q", failure.ProbeErrorCode, failure.ArtifactErrorCode)
	}
	if failure.ParserProfile != ptyusage.ParserProfileClaudeCodeTwoOne ||
		failure.ProfilePreferredFrom != ptyusage.MinimumTestedVersion ||
		failure.CaptureProfile != "screen-reader-120x40" {
		t.Fatalf("formatAuditFailure() profile = %#v", failure)
	}
	if failure.Audit == nil || failure.Audit.Artifact != artifact ||
		failure.Audit.WorkingDirectory != `C:\Users\person\AppData\Local\Temp\windowseat-probe` {
		t.Fatalf("formatAuditFailure() audit = %#v", failure.Audit)
	}
}
