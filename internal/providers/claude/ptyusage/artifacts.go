package ptyusage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const maximumArtifactBytes = 1024 * 1024

var (
	ErrUnsafeArtifact = errors.New("Claude probe artifact is not safe to remove")
	uuidPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type ArtifactExpectation struct {
	ProjectsRoot               string
	ProjectDirectory           string
	ProjectDirectoryWasCreated bool
	CandidatePath              string
	WorkingDirectory           string
	SessionID                  string
	StartedAt                  time.Time
	EndedAt                    time.Time
}

type CleanupPlan struct {
	expectation  ArtifactExpectation
	artifactInfo os.FileInfo
	projectInfo  os.FileInfo
	digest       [sha256.Size]byte
}

type CleanupResult struct {
	ArtifactRemoved          bool `json:"artifact_removed"`
	ProjectDirectoryRemoved  bool `json:"project_directory_removed"`
	ProjectDirectoryRetained bool `json:"project_directory_retained"`
}

type ArtifactAudit struct {
	CandidatePath              string `json:"candidate_path"`
	ProjectDirectory           string `json:"project_directory"`
	ProjectDirectoryWasCreated bool   `json:"project_directory_was_created"`
	Present                    bool   `json:"present"`
	EmptySessionVerified       bool   `json:"empty_session_verified"`
	ByteSize                   int64  `json:"byte_size,omitempty"`
	SHA256                     string `json:"sha256,omitempty"`
}

func ClassifyArtifact(expectation ArtifactExpectation) (CleanupPlan, error) {
	if err := validateExpectation(expectation); err != nil {
		return CleanupPlan{}, err
	}

	projectInfo, err := os.Lstat(expectation.ProjectDirectory)
	if err != nil {
		return CleanupPlan{}, unsafeArtifact("project directory is unavailable", err)
	}
	if !projectInfo.IsDir() || projectInfo.Mode()&os.ModeSymlink != 0 {
		return CleanupPlan{}, unsafeArtifact("project path is not a regular directory", nil)
	}

	file, info, err := openRegularFile(expectation.CandidatePath)
	if err != nil {
		return CleanupPlan{}, err
	}
	defer file.Close()
	if info.Size() > maximumArtifactBytes {
		return CleanupPlan{}, unsafeArtifact("artifact exceeds the size limit", nil)
	}
	if !withinProbeWindow(info.ModTime(), expectation.StartedAt, expectation.EndedAt) {
		return CleanupPlan{}, unsafeArtifact("artifact timestamp is outside the probe window", nil)
	}

	digest, err := inspectTranscript(file, info.Size(), expectation)
	if err != nil {
		return CleanupPlan{}, err
	}
	finalInfo, err := file.Stat()
	if err != nil {
		return CleanupPlan{}, unsafeArtifact("artifact metadata changed during inspection", err)
	}
	if !sameMetadata(info, finalInfo) {
		return CleanupPlan{}, unsafeArtifact("artifact metadata changed during inspection", nil)
	}

	return CleanupPlan{
		expectation:  expectation,
		artifactInfo: info,
		projectInfo:  projectInfo,
		digest:       digest,
	}, nil
}

func NewArtifactAudit(expectation ArtifactExpectation) ArtifactAudit {
	return ArtifactAudit{
		CandidatePath:              expectation.CandidatePath,
		ProjectDirectory:           expectation.ProjectDirectory,
		ProjectDirectoryWasCreated: expectation.ProjectDirectoryWasCreated,
	}
}

func (plan CleanupPlan) Audit() ArtifactAudit {
	report := NewArtifactAudit(plan.expectation)
	report.Present = true
	report.EmptySessionVerified = true
	report.ByteSize = plan.artifactInfo.Size()
	report.SHA256 = hex.EncodeToString(plan.digest[:])
	return report
}

func (plan CleanupPlan) Execute() (CleanupResult, error) {
	file, info, err := openRegularFile(plan.expectation.CandidatePath)
	if errors.Is(err, os.ErrNotExist) {
		return plan.removeEmptyProjectDirectory(CleanupResult{})
	}
	if err != nil {
		return CleanupResult{}, err
	}
	defer file.Close()

	if !os.SameFile(plan.artifactInfo, info) || !sameMetadata(plan.artifactInfo, info) {
		return CleanupResult{}, unsafeArtifact("artifact identity or metadata changed before cleanup", nil)
	}
	digest, err := hashFile(file, info.Size())
	if err != nil {
		return CleanupResult{}, err
	}
	if !bytes.Equal(digest[:], plan.digest[:]) {
		return CleanupResult{}, unsafeArtifact("artifact content changed before cleanup", nil)
	}
	if err := file.Close(); err != nil {
		return CleanupResult{}, unsafeArtifact("artifact could not be closed before cleanup", err)
	}
	if err := os.Remove(plan.expectation.CandidatePath); err != nil {
		return CleanupResult{}, unsafeArtifact("artifact could not be removed", err)
	}

	return plan.removeEmptyProjectDirectory(CleanupResult{ArtifactRemoved: true})
}

func (plan CleanupPlan) removeEmptyProjectDirectory(result CleanupResult) (CleanupResult, error) {
	if !plan.expectation.ProjectDirectoryWasCreated {
		return result, nil
	}
	info, err := os.Lstat(plan.expectation.ProjectDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, unsafeArtifact("project directory could not be checked after cleanup", err)
	}
	if !os.SameFile(plan.projectInfo, info) {
		return result, unsafeArtifact("project directory identity changed before cleanup", nil)
	}
	entries, err := os.ReadDir(plan.expectation.ProjectDirectory)
	if err != nil {
		return result, unsafeArtifact("project directory could not be read after cleanup", err)
	}
	if len(entries) != 0 {
		result.ProjectDirectoryRetained = true
		return result, nil
	}
	if err := os.Remove(plan.expectation.ProjectDirectory); err != nil {
		return result, unsafeArtifact("empty project directory could not be removed", err)
	}
	result.ProjectDirectoryRemoved = true
	return result, nil
}

func validateExpectation(expectation ArtifactExpectation) error {
	if expectation.ProjectsRoot == "" || expectation.ProjectDirectory == "" || expectation.CandidatePath == "" ||
		expectation.WorkingDirectory == "" || expectation.SessionID == "" {
		return unsafeArtifact("artifact expectation is incomplete", nil)
	}
	if !uuidPattern.MatchString(expectation.SessionID) {
		return unsafeArtifact("session ID is not a UUID", nil)
	}
	if expectation.StartedAt.IsZero() || expectation.EndedAt.IsZero() || expectation.EndedAt.Before(expectation.StartedAt) {
		return unsafeArtifact("probe time window is invalid", nil)
	}
	if !pathWithin(expectation.ProjectsRoot, expectation.ProjectDirectory) {
		return unsafeArtifact("project directory is outside the Claude projects root", nil)
	}
	expectedCandidate := filepath.Join(expectation.ProjectDirectory, expectation.SessionID+".jsonl")
	if !pathsEqual(expectedCandidate, expectation.CandidatePath) {
		return unsafeArtifact("candidate path does not match the probe session", nil)
	}
	return nil
}

func inspectTranscript(file *os.File, size int64, expectation ArtifactExpectation) ([sha256.Size]byte, error) {
	content, err := io.ReadAll(io.LimitReader(file, maximumArtifactBytes+1))
	if err != nil {
		return [sha256.Size]byte{}, unsafeArtifact("artifact could not be read", err)
	}
	if int64(len(content)) != size || len(content) > maximumArtifactBytes {
		return [sha256.Size]byte{}, unsafeArtifact("artifact size changed during inspection", nil)
	}
	if err := validateTranscript(content, expectation); err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(content), nil
}

type transcriptRecord struct {
	Type      string             `json:"type"`
	Subtype   string             `json:"subtype"`
	SessionID string             `json:"sessionId"`
	CWD       string             `json:"cwd"`
	Timestamp string             `json:"timestamp"`
	Content   json.RawMessage    `json:"content"`
	Message   *transcriptMessage `json:"message"`
	Usage     json.RawMessage    `json:"usage"`
}

type transcriptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage   json.RawMessage `json:"usage"`
}

func validateTranscript(content []byte, expectation ArtifactExpectation) error {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), maximumArtifactBytes)
	records := 0
	sessionSeen := false
	workingDirectorySeen := false
	timestampSeen := false
	usageCommandSeen := false
	exitCommandSeen := false

	for scanner.Scan() {
		records++
		var record transcriptRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return unsafeArtifact("artifact contains invalid JSON", nil)
		}
		if err := validateTranscriptRecord(record, expectation); err != nil {
			return err
		}
		if record.SessionID != "" {
			sessionSeen = true
		}
		if record.CWD != "" {
			workingDirectorySeen = true
		}
		if record.Timestamp != "" {
			timestampSeen = true
		}
		for _, text := range transcriptStrings(record) {
			usageCommandSeen = usageCommandSeen || strings.Contains(text, "<command-name>/usage</command-name>")
			exitCommandSeen = exitCommandSeen || strings.Contains(text, "<command-name>/exit</command-name>")
		}
	}
	if err := scanner.Err(); err != nil {
		return unsafeArtifact("artifact could not be scanned", err)
	}
	if records == 0 || !sessionSeen || !workingDirectorySeen || !timestampSeen || !usageCommandSeen || !exitCommandSeen {
		return unsafeArtifact("artifact does not prove an empty Windowseat probe session", nil)
	}
	return nil
}

func validateTranscriptRecord(record transcriptRecord, expectation ArtifactExpectation) error {
	allowedTypes := map[string]bool{
		"mode":                  true,
		"permission-mode":       true,
		"bridge-session":        true,
		"system":                true,
		"file-history-snapshot": true,
		"last-prompt":           true,
		"user":                  true,
	}
	if !allowedTypes[record.Type] {
		return unsafeArtifact("artifact contains a disallowed record type", nil)
	}
	if rawPresent(record.Usage) || (record.Message != nil && rawPresent(record.Message.Usage)) {
		return unsafeArtifact("artifact contains model usage", nil)
	}
	if record.SessionID != "" && record.SessionID != expectation.SessionID {
		return unsafeArtifact("artifact contains a different session ID", nil)
	}
	if record.CWD != "" && !pathsEqual(record.CWD, expectation.WorkingDirectory) {
		return unsafeArtifact("artifact contains a different working directory", nil)
	}
	if record.Timestamp != "" {
		timestamp, err := time.Parse(time.RFC3339Nano, record.Timestamp)
		if err != nil || !withinProbeWindow(timestamp, expectation.StartedAt, expectation.EndedAt) {
			return unsafeArtifact("artifact contains a timestamp outside the probe window", nil)
		}
	}

	if record.Message != nil {
		if record.Message.Role != "user" || record.Type != "user" {
			return unsafeArtifact("artifact contains a non-user message", nil)
		}
		text, ok := rawString(record.Message.Content)
		if !ok || !allowedLocalCommandText(text) {
			return unsafeArtifact("artifact contains a user prompt", nil)
		}
	}
	if record.Type == "user" && record.Message == nil {
		return unsafeArtifact("artifact contains an unrecognized user record", nil)
	}
	if record.Type == "system" {
		text, ok := rawString(record.Content)
		if record.Subtype != "local_command" || !ok || !allowedLocalCommandText(text) {
			return unsafeArtifact("artifact contains an unrecognized system record", nil)
		}
	}
	return nil
}

func allowedLocalCommandText(text string) bool {
	return strings.HasPrefix(text, "<command-name>/usage</command-name>") ||
		strings.HasPrefix(text, "<command-name>/exit</command-name>") ||
		strings.HasPrefix(text, "<local-command-caveat>") ||
		strings.HasPrefix(text, "<local-command-stdout>")
}

func transcriptStrings(record transcriptRecord) []string {
	values := make([]string, 0, 2)
	if text, ok := rawString(record.Content); ok {
		values = append(values, text)
	}
	if record.Message != nil {
		if text, ok := rawString(record.Message.Content); ok {
			values = append(values, text)
		}
	}
	return values
}

func rawString(value json.RawMessage) (string, bool) {
	if !rawPresent(value) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	return text, true
}

func rawPresent(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null"))
}

func openRegularFile(path string) (*os.File, os.FileInfo, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, unsafeArtifact("artifact is unavailable", err)
	}
	if !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, unsafeArtifact("artifact is not a regular file", nil)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, unsafeArtifact("artifact could not be opened", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, unsafeArtifact("artifact metadata could not be read", err)
	}
	if !os.SameFile(linkInfo, info) {
		file.Close()
		return nil, nil, unsafeArtifact("artifact identity changed while opening", nil)
	}
	return file, info, nil
}

func hashFile(file *os.File, size int64) ([sha256.Size]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return [sha256.Size]byte{}, unsafeArtifact("artifact could not be reread", err)
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumArtifactBytes+1))
	if err != nil || int64(len(content)) != size || len(content) > maximumArtifactBytes {
		return [sha256.Size]byte{}, unsafeArtifact("artifact changed while verifying cleanup", err)
	}
	return sha256.Sum256(content), nil
}

func sameMetadata(left, right os.FileInfo) bool {
	return left.Size() == right.Size() && left.Mode() == right.Mode() && left.ModTime().Equal(right.ModTime())
}

func withinProbeWindow(value, start, end time.Time) bool {
	const tolerance = 2 * time.Second
	return !value.Before(start.Add(-tolerance)) && !value.After(end.Add(tolerance))
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != "." && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func pathsEqual(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func unsafeArtifact(message string, cause error) error {
	return artifactSafetyError{message: message, cause: cause}
}

type artifactSafetyError struct {
	message string
	cause   error
}

func (err artifactSafetyError) Error() string {
	return ErrUnsafeArtifact.Error() + ": " + err.message
}

func (err artifactSafetyError) Unwrap() []error {
	if err.cause == nil {
		return []error{ErrUnsafeArtifact}
	}
	return []error{ErrUnsafeArtifact, err.cause}
}
