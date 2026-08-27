package ptyusage

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/xpty"
)

const (
	maximumCaptureBytes = 2 * 1024 * 1024
	quietPeriod         = 500 * time.Millisecond
	usageConfirmPeriod  = 500 * time.Millisecond
	exitDelay           = 200 * time.Millisecond
)

var (
	ErrManagedPolicyPresent = errors.New("Claude managed policy is present or cannot be ruled out")
	ErrProbeTimeout         = errors.New("Claude usage probe timed out")
	ErrProbeUnavailable     = errors.New("Claude usage probe is unavailable")
)

type ProbeConfig struct {
	CommandPath      string
	Version          string
	WorkingDirectory string
	ProjectsRoot     string
	SessionID        string
	RetainArtifact   bool
	CheckPolicy      func() error
}

type ProbeResult struct {
	Usage         Usage
	ParserProfile ParserProfile
	StartedAt     time.Time
	ObservedAt    time.Time
	EndedAt       time.Time
	Cleanup       *CleanupResult
	ArtifactAudit *ArtifactAudit
	ArtifactError error
}

func Run(ctx context.Context, config ProbeConfig) (ProbeResult, error) {
	if err := validateProbeConfig(config); err != nil {
		return ProbeResult{}, err
	}
	parser, err := selectParser(config.Version)
	if err != nil {
		return ProbeResult{}, err
	}
	if err := config.CheckPolicy(); err != nil {
		return ProbeResult{}, fmt.Errorf("%w: %v", ErrManagedPolicyPresent, err)
	}

	projectDirectory := ProjectDirectory(config.ProjectsRoot, config.WorkingDirectory)
	projectDirectoryWasCreated, err := pathWasAbsent(projectDirectory)
	if err != nil {
		return ProbeResult{}, err
	}
	candidatePath := filepath.Join(projectDirectory, config.SessionID+".jsonl")
	if _, err := os.Lstat(candidatePath); err == nil {
		return ProbeResult{}, fmt.Errorf("%w: session artifact already exists", ErrProbeUnavailable)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProbeResult{}, fmt.Errorf("%w: session artifact cannot be checked", ErrProbeUnavailable)
	}

	terminal, err := xpty.NewPty(parser.Width, parser.Height)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("%w: PTY creation failed", ErrProbeUnavailable)
	}
	width, height, err := terminal.Size()
	if err != nil || width != parser.Width || height != parser.Height {
		terminal.Close()
		return ProbeResult{}, fmt.Errorf("%w: PTY size could not be fixed", ErrProbeUnavailable)
	}

	command := exec.Command(config.CommandPath, claudeArguments(config.SessionID)...)
	command.Dir = config.WorkingDirectory
	configureProcess(command)
	startedAt := time.Now()
	if err := terminal.Start(command); err != nil {
		terminal.Close()
		return ProbeResult{}, fmt.Errorf("%w: Claude could not start", ErrProbeUnavailable)
	}

	capture := newBoundedCapture(terminal)
	waitChannel := make(chan error, 1)
	go func() { waitChannel <- waitProcess(command) }()

	usage, observedAt, waitErr, processWaited, driveErr := driveProbe(
		ctx,
		terminal,
		capture,
		waitChannel,
		config.Version,
		parser.ParserProfile,
	)
	if driveErr != nil {
		terminateProcess(command)
	}
	if !processWaited {
		waitErr = <-waitChannel
	}
	if closeErr := terminal.Close(); driveErr == nil && closeErr != nil {
		driveErr = fmt.Errorf("%w: PTY close failed", ErrProbeUnavailable)
	}
	capture.wait()
	endedAt := time.Now()

	result := ProbeResult{
		Usage:         usage,
		ParserProfile: parser.ParserProfile,
		StartedAt:     startedAt,
		ObservedAt:    observedAt,
		EndedAt:       endedAt,
	}
	result.Cleanup, result.ArtifactAudit, result.ArtifactError = handleProbeArtifact(ArtifactExpectation{
		ProjectsRoot:               config.ProjectsRoot,
		ProjectDirectory:           projectDirectory,
		ProjectDirectoryWasCreated: projectDirectoryWasCreated,
		CandidatePath:              candidatePath,
		WorkingDirectory:           config.WorkingDirectory,
		SessionID:                  config.SessionID,
		StartedAt:                  startedAt,
		EndedAt:                    endedAt,
	}, config.RetainArtifact)
	if driveErr != nil {
		return result, driveErr
	}
	if waitErr != nil {
		return result, fmt.Errorf("%w: Claude exited unsuccessfully", ErrProbeUnavailable)
	}
	return result, nil
}

func validateProbeConfig(config ProbeConfig) error {
	if config.CommandPath == "" || config.WorkingDirectory == "" || config.ProjectsRoot == "" || config.SessionID == "" {
		return fmt.Errorf("%w: probe configuration is incomplete", ErrProbeUnavailable)
	}
	if _, err := selectParser(config.Version); err != nil {
		return err
	}
	if !uuidPattern.MatchString(config.SessionID) {
		return fmt.Errorf("%w: session ID is not a UUID", ErrProbeUnavailable)
	}
	if config.CheckPolicy == nil {
		return fmt.Errorf("%w: managed policy check is required", ErrManagedPolicyPresent)
	}
	workingDirectory, err := os.Stat(config.WorkingDirectory)
	if err != nil || !workingDirectory.IsDir() {
		return fmt.Errorf("%w: working directory is unavailable", ErrProbeUnavailable)
	}
	return nil
}

func claudeArguments(sessionID string) []string {
	return []string{
		"--safe-mode",
		"--ax-screen-reader",
		"--no-chrome",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--tools", "",
		"--permission-mode", "dontAsk",
		"--model", "haiku",
		"--session-id", sessionID,
		"--settings", `{"disableAllHooks":true}`,
	}
}

type probePhase int

const (
	phaseWaitingForReady probePhase = iota
	phaseWaitingForUsage
	phaseConfirmingUsage
	phaseClosingUsage
	phaseWaitingForExit
)

func driveProbe(
	ctx context.Context,
	terminal xpty.Pty,
	capture *boundedCapture,
	waitChannel <-chan error,
	version string,
	parser ParserProfile,
) (Usage, time.Time, error, bool, error) {
	phase := phaseWaitingForReady
	timer := time.NewTimer(quietPeriod)
	defer timer.Stop()
	lastOutputAt := time.Now()
	var candidate Usage
	var observedAt time.Time
	var lastParseError error

	for {
		select {
		case <-ctx.Done():
			return Usage{}, time.Time{}, nil, false, timeoutError(phase, capture.snapshot(), lastParseError)
		case waitErr := <-waitChannel:
			if phase != phaseWaitingForExit {
				return Usage{}, time.Time{}, waitErr, true, fmt.Errorf("%w: Claude exited before the probe completed", ErrProbeUnavailable)
			}
			return candidate, observedAt, waitErr, true, nil
		case <-capture.activity:
			lastOutputAt = time.Now()
			if phase == phaseWaitingForUsage || phase == phaseConfirmingUsage {
				lastParseError = nil
			}
			if capture.overflowed() {
				return Usage{}, time.Time{}, nil, false, fmt.Errorf("%w: terminal output exceeded the capture limit", ErrProbeUnavailable)
			}
			if hasTrustPrompt(capture.snapshot()) {
				return Usage{}, time.Time{}, nil, false, fmt.Errorf("%w: Claude requested workspace trust", ErrProbeUnavailable)
			}
			resetTimer(timer, quietPeriod)
		case <-timer.C:
			transcript := capture.snapshot()
			switch phase {
			case phaseWaitingForReady:
				if !hasReadyPrompt(transcript) {
					continue
				}
				if err := writeAll(terminal, []byte("/usage\r")); err != nil {
					return Usage{}, time.Time{}, nil, false, fmt.Errorf("%w: /usage could not be sent", ErrProbeUnavailable)
				}
				phase = phaseWaitingForUsage
				resetTimer(timer, quietPeriod)
			case phaseWaitingForUsage, phaseConfirmingUsage:
				parsed, err := Parse(ParseInput{
					Output:       transcript,
					Version:      version,
					Width:        parser.Width,
					Height:       parser.Height,
					ScreenReader: parser.ScreenReader,
					ObservedAt:   lastOutputAt,
				})
				if err != nil {
					completeFooter := strings.Contains(strings.ToLower(string(transcript)), "usage credits") &&
						strings.Contains(strings.ToLower(string(transcript)), "esc to cancel")
					if completeFooter && lastParseError != nil && lastParseError.Error() == err.Error() {
						return Usage{}, time.Time{}, nil, false, err
					}
					lastParseError = err
					phase = phaseWaitingForUsage
					if completeFooter {
						resetTimer(timer, usageConfirmPeriod)
					}
					continue
				}
				if phase == phaseConfirmingUsage && reflect.DeepEqual(candidate, parsed) {
					candidate = parsed
					observedAt = lastOutputAt
					if err := writeAll(terminal, []byte{0x1b}); err != nil {
						return Usage{}, time.Time{}, nil, false, fmt.Errorf("%w: usage view could not be closed", ErrProbeUnavailable)
					}
					phase = phaseClosingUsage
					resetTimer(timer, exitDelay)
					continue
				}
				candidate = parsed
				phase = phaseConfirmingUsage
				resetTimer(timer, usageConfirmPeriod)
			case phaseClosingUsage:
				if err := writeAll(terminal, []byte("/exit\r")); err != nil {
					return Usage{}, time.Time{}, nil, false, fmt.Errorf("%w: /exit could not be sent", ErrProbeUnavailable)
				}
				phase = phaseWaitingForExit
			case phaseWaitingForExit:
			}
		}
	}
}

type boundedCapture struct {
	reader   io.Reader
	activity chan struct{}
	done     chan struct{}

	mu       sync.Mutex
	data     []byte
	overflow bool
}

func newBoundedCapture(reader io.Reader) *boundedCapture {
	capture := &boundedCapture{
		reader:   reader,
		activity: make(chan struct{}, 1),
		done:     make(chan struct{}),
		data:     make([]byte, 0, 64*1024),
	}
	go capture.read()
	return capture
}

func (capture *boundedCapture) read() {
	defer close(capture.done)
	buffer := make([]byte, 64*1024)
	for {
		read, err := capture.reader.Read(buffer)
		if read > 0 {
			capture.mu.Lock()
			remaining := maximumCaptureBytes - len(capture.data)
			if read <= remaining {
				capture.data = append(capture.data, buffer[:read]...)
			} else {
				if remaining > 0 {
					capture.data = append(capture.data, buffer[:remaining]...)
				}
				capture.overflow = true
			}
			capture.mu.Unlock()
			select {
			case capture.activity <- struct{}{}:
			default:
			}
		}
		if err != nil {
			return
		}
	}
}

func (capture *boundedCapture) snapshot() []byte {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]byte(nil), capture.data...)
}

func (capture *boundedCapture) overflowed() bool {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.overflow
}

func timeoutError(phase probePhase, output []byte, parseError error) error {
	plain := strings.ToLower(ansiControlPattern.ReplaceAllString(string(output), ""))
	parseStatus := "none"
	if parseError != nil {
		parseStatus = parseError.Error()
	}
	return fmt.Errorf(
		"%w: phase=%s bytes=%d safe_mode=%t claude_code=%t help_prompt=%t trust=%t usage_navigation=%t current_session=%t completed_footer=%t parser=%q",
		ErrProbeTimeout,
		phase,
		len(output),
		strings.Contains(plain, "safe mode"),
		strings.Contains(plain, "claude code"),
		strings.Contains(plain, "what can i help"),
		strings.Contains(plain, "trust the files") || strings.Contains(plain, "yes, i trust"),
		strings.Contains(plain, "settings") && strings.Contains(plain, "status") && strings.Contains(plain, "usage"),
		strings.Contains(plain, "current session"),
		strings.Contains(plain, "usage credits") && strings.Contains(plain, "esc to cancel"),
		parseStatus,
	)
}

func (phase probePhase) String() string {
	switch phase {
	case phaseWaitingForReady:
		return "waiting_for_ready"
	case phaseWaitingForUsage:
		return "waiting_for_usage"
	case phaseConfirmingUsage:
		return "confirming_usage"
	case phaseClosingUsage:
		return "closing_usage"
	case phaseWaitingForExit:
		return "waiting_for_exit"
	default:
		return "unknown"
	}
}

func (capture *boundedCapture) wait() {
	<-capture.done
}

func hasReadyPrompt(output []byte) bool {
	plain := strings.ToLower(ansiControlPattern.ReplaceAllString(string(output), ""))
	return strings.Contains(plain, "safe mode: all customizations are disabled") &&
		(strings.Contains(plain, "claude code") || strings.Contains(plain, "what can i help"))
}

func hasTrustPrompt(output []byte) bool {
	plain := strings.ToLower(ansiControlPattern.ReplaceAllString(string(output), ""))
	return strings.Contains(plain, "trust the files") || strings.Contains(plain, "yes, i trust")
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		data = data[written:]
	}
	return nil
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func handleProbeArtifact(expectation ArtifactExpectation, retain bool) (*CleanupResult, *ArtifactAudit, error) {
	audit := NewArtifactAudit(expectation)
	plan, err := ClassifyArtifact(expectation)
	if errors.Is(err, os.ErrNotExist) {
		if retain {
			return nil, &audit, nil
		}
		cleanup := CleanupResult{}
		return &cleanup, nil, nil
	}
	if err != nil {
		return nil, &audit, err
	}
	if retain {
		audit = plan.Audit()
		return nil, &audit, nil
	}
	cleanup, err := plan.Execute()
	return &cleanup, nil, err
}

func ProjectDirectory(projectsRoot, workingDirectory string) string {
	encoded := strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(filepath.Clean(workingDirectory))
	return filepath.Join(projectsRoot, encoded)
}

func pathWasAbsent(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, fmt.Errorf("%w: project directory cannot be checked", ErrProbeUnavailable)
}

func NewSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}
