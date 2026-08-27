package ptyusage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const artifactSessionID = "eccffe34-e604-4b57-b04c-e586c4dda5a8"

func TestCleanupRemovesOnlyProvenProbeArtifact(t *testing.T) {
	expectation := createArtifact(t, true, validArtifactTranscript())

	plan, err := ClassifyArtifact(expectation)
	if err != nil {
		t.Fatalf("ClassifyArtifact() error = %v", err)
	}
	result, err := plan.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.ArtifactRemoved || !result.ProjectDirectoryRemoved || result.ProjectDirectoryRetained {
		t.Fatalf("Execute() result = %#v", result)
	}
	if _, err := os.Stat(expectation.CandidatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact Stat() error = %v, want not exist", err)
	}

	if _, err := plan.Execute(); err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
}

func TestCleanupKeepsPreexistingProjectDirectory(t *testing.T) {
	expectation := createArtifact(t, false, validArtifactTranscript())
	plan, err := ClassifyArtifact(expectation)
	if err != nil {
		t.Fatalf("ClassifyArtifact() error = %v", err)
	}
	result, err := plan.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.ArtifactRemoved || result.ProjectDirectoryRemoved {
		t.Fatalf("Execute() result = %#v", result)
	}
	if info, err := os.Stat(expectation.ProjectDirectory); err != nil || !info.IsDir() {
		t.Fatalf("project directory Stat() = %#v, %v", info, err)
	}
}

func TestCleanupKeepsDirectoryWithDecoy(t *testing.T) {
	expectation := createArtifact(t, true, validArtifactTranscript())
	decoy := filepath.Join(expectation.ProjectDirectory, "do-not-remove.jsonl")
	if err := os.WriteFile(decoy, []byte("user data"), 0o600); err != nil {
		t.Fatalf("WriteFile(decoy) error = %v", err)
	}

	plan, err := ClassifyArtifact(expectation)
	if err != nil {
		t.Fatalf("ClassifyArtifact() error = %v", err)
	}
	result, err := plan.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.ArtifactRemoved || !result.ProjectDirectoryRetained || result.ProjectDirectoryRemoved {
		t.Fatalf("Execute() result = %#v", result)
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Fatalf("decoy Stat() error = %v", err)
	}
}

func TestAuditReportsAndRetainsProvenArtifact(t *testing.T) {
	expectation := createArtifact(t, true, validArtifactTranscript())

	cleanup, audit, err := handleProbeArtifact(expectation, true)
	if err != nil {
		t.Fatalf("handleProbeArtifact() error = %v", err)
	}
	if cleanup != nil {
		t.Fatalf("handleProbeArtifact() cleanup = %#v, want nil", cleanup)
	}
	if audit == nil || !audit.Present || !audit.EmptySessionVerified {
		t.Fatalf("handleProbeArtifact() audit = %#v", audit)
	}
	content, err := os.ReadFile(expectation.CandidatePath)
	if err != nil {
		t.Fatalf("ReadFile(retained artifact) error = %v", err)
	}
	digest := sha256.Sum256(content)
	if audit.CandidatePath != expectation.CandidatePath ||
		audit.ProjectDirectory != expectation.ProjectDirectory ||
		!audit.ProjectDirectoryWasCreated ||
		audit.ByteSize != int64(len(content)) ||
		audit.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("handleProbeArtifact() audit = %#v", audit)
	}
	if _, err := os.Stat(expectation.CandidatePath); err != nil {
		t.Fatalf("retained artifact Stat() error = %v", err)
	}
	if _, err := os.Stat(expectation.ProjectDirectory); err != nil {
		t.Fatalf("retained project directory Stat() error = %v", err)
	}
}

func TestClassifyArtifactRejectsSessionWithModelActivity(t *testing.T) {
	transcript := validArtifactTranscript() + `{"type":"assistant","sessionId":"` + artifactSessionID + `","message":{"role":"assistant","content":"answer"}}` + "\n"
	expectation := createArtifact(t, true, transcript)

	_, err := ClassifyArtifact(expectation)
	if !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("ClassifyArtifact() error = %v, want ErrUnsafeArtifact", err)
	}
	if _, statErr := os.Stat(expectation.CandidatePath); statErr != nil {
		t.Fatalf("unsafe artifact was changed: %v", statErr)
	}
}

func TestClassifyArtifactRejectsUserPrompt(t *testing.T) {
	transcript := strings.Replace(validArtifactTranscript(),
		`<command-name>/usage</command-name>`, "tell me a joke", 1)
	expectation := createArtifact(t, true, transcript)

	_, err := ClassifyArtifact(expectation)
	if !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("ClassifyArtifact() error = %v, want ErrUnsafeArtifact", err)
	}
}

func TestCleanupRejectsReplacementAfterClassification(t *testing.T) {
	expectation := createArtifact(t, true, validArtifactTranscript())
	plan, err := ClassifyArtifact(expectation)
	if err != nil {
		t.Fatalf("ClassifyArtifact() error = %v", err)
	}
	replacement := filepath.Join(filepath.Dir(expectation.CandidatePath), "replacement.jsonl")
	if err := os.WriteFile(replacement, []byte(strings.ReplaceAll(validArtifactTranscript(), "local command", "other command")), 0o600); err != nil {
		t.Fatalf("WriteFile(replacement) error = %v", err)
	}
	if err := os.Rename(replacement, expectation.CandidatePath); err != nil {
		t.Fatalf("Rename(replacement) error = %v", err)
	}
	if err := os.Chtimes(expectation.CandidatePath, expectation.StartedAt, expectation.StartedAt); err != nil {
		t.Fatalf("Chtimes(replacement) error = %v", err)
	}

	_, err = plan.Execute()
	if !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("Execute() error = %v, want ErrUnsafeArtifact", err)
	}
	if _, statErr := os.Stat(expectation.CandidatePath); statErr != nil {
		t.Fatalf("replacement artifact was removed: %v", statErr)
	}
}

func createArtifact(t *testing.T, directoryWasCreated bool, transcript string) ArtifactExpectation {
	t.Helper()
	root := t.TempDir()
	projectsRoot := filepath.Join(root, "projects")
	projectDirectory := filepath.Join(projectsRoot, "-tmp-windowseat-probe")
	if err := os.MkdirAll(projectDirectory, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	candidate := filepath.Join(projectDirectory, artifactSessionID+".jsonl")
	workingDirectory := filepath.Join(root, "work")
	startedAt := time.Now().Add(-time.Second).UTC()
	endedAt := time.Now().Add(time.Second).UTC()
	encodedWorkingDirectory, err := json.Marshal(workingDirectory)
	if err != nil {
		t.Fatalf("Marshal(workingDirectory) error = %v", err)
	}
	encodedTimestamp, err := json.Marshal(startedAt.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("Marshal(timestamp) error = %v", err)
	}
	transcript = strings.ReplaceAll(transcript, `"WORKDIR"`, string(encodedWorkingDirectory))
	transcript = strings.ReplaceAll(transcript, `"TIMESTAMP"`, string(encodedTimestamp))
	if err := os.WriteFile(candidate, []byte(transcript), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chtimes(candidate, startedAt, startedAt); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	return ArtifactExpectation{
		ProjectsRoot:               projectsRoot,
		ProjectDirectory:           projectDirectory,
		ProjectDirectoryWasCreated: directoryWasCreated,
		CandidatePath:              candidate,
		WorkingDirectory:           workingDirectory,
		SessionID:                  artifactSessionID,
		StartedAt:                  startedAt,
		EndedAt:                    endedAt,
	}
}

func validArtifactTranscript() string {
	return strings.Join([]string{
		`{"type":"mode","sessionId":"` + artifactSessionID + `"}`,
		`{"type":"permission-mode","sessionId":"` + artifactSessionID + `"}`,
		`{"type":"bridge-session","sessionId":"` + artifactSessionID + `"}`,
		`{"type":"system","subtype":"local_command","sessionId":"` + artifactSessionID + `","cwd":"WORKDIR","timestamp":"TIMESTAMP","content":"<command-name>/usage</command-name>"}`,
		`{"type":"system","subtype":"local_command","sessionId":"` + artifactSessionID + `","cwd":"WORKDIR","timestamp":"TIMESTAMP","content":"<local-command-stdout>Session Usage</local-command-stdout>"}`,
		`{"type":"file-history-snapshot","sessionId":"` + artifactSessionID + `"}`,
		`{"type":"last-prompt","sessionId":"` + artifactSessionID + `"}`,
		`{"type":"user","sessionId":"` + artifactSessionID + `","message":{"role":"user","content":"<local-command-caveat>local command</local-command-caveat>"}}`,
		`{"type":"user","sessionId":"` + artifactSessionID + `","message":{"role":"user","content":"<command-name>/exit</command-name>"}}`,
	}, "\n") + "\n"
}
