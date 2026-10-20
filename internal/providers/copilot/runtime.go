package copilot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"os/exec"
	"strconv"
	"time"

	"windowseat/internal/providers/command"
)

// method is a JSON-RPC method this adapter may send to the Copilot runtime.
// Every other method, including the credential-bearing account.getCurrentAuth
// and account.getAllUsers, is refused before it reaches the wire.
type method string

const (
	methodConnect    method = "connect"
	methodAuthStatus method = "auth.getStatus"
	methodQuota      method = "account.getQuota"
	methodShutdown   method = "runtime.shutdown"
)

var allowedMethods = map[method]bool{
	methodConnect:    true,
	methodAuthStatus: true,
	methodQuota:      true,
	methodShutdown:   true,
}

// runtimeArgs match the official SDK's stdio launch and pin the runtime the
// npm loader bundles instead of a newer cached or downloaded one.
var runtimeArgs = []string{"--headless", "--no-auto-update", "--stdio"}

const (
	stopGracePeriod = 5 * time.Second
	maxFrameBytes   = 4 << 20
)

type connectResult struct {
	ProtocolVersion int64  `json:"protocolVersion"`
	Version         string `json:"version"`
}

type authStatus struct {
	IsAuthenticated bool    `json:"isAuthenticated"`
	AuthType        *string `json:"authType"`
	Host            *string `json:"host"`
	Login           *string `json:"login"`
}

// runtime is one Copilot CLI process speaking JSON-RPC over stdio. It lives
// for a single read because the runtime caches quota per process.
type runtime struct {
	process *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	exited  chan struct{}
	nextID  int64
}

// startRuntime launches the selected CLI with the inherited environment,
// passed through untouched.
func startRuntime(ctx context.Context, path string) (*runtime, error) {
	process := exec.CommandContext(ctx, path, runtimeArgs...)
	process.WaitDelay = stopGracePeriod
	stdin, err := process.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.StartTree(process); err != nil {
		return nil, err
	}

	exited := make(chan struct{})
	go func() {
		_ = process.Wait()
		close(exited)
	}()
	return &runtime{process: process, stdin: stdin, stdout: bufio.NewReader(stdout), exited: exited}, nil
}

func (r *runtime) connect() (connectResult, error) {
	var result connectResult
	params := map[string]any{"supportedTaskKinds": []string{}}
	return result, r.call(methodConnect, params, &result)
}

func (r *runtime) authStatus() (authStatus, error) {
	var result authStatus
	return result, r.call(methodAuthStatus, struct{}{}, &result)
}

func (r *runtime) quota() (json.RawMessage, error) {
	var result json.RawMessage
	return result, r.call(methodQuota, struct{}{}, &result)
}

// stop asks the runtime to shut down, closes its input, and kills it if it
// has not exited within the grace period.
func (r *runtime) stop() error {
	shutdownErr := r.call(methodShutdown, struct{}{}, nil)
	_ = r.stdin.Close()
	select {
	case <-r.exited:
		return shutdownErr
	case <-time.After(stopGracePeriod):
		_ = r.process.Cancel()
		<-r.exited
		return errors.Join(shutdownErr, errors.New("copilot runtime did not exit after shutdown"))
	}
}

type rpcError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("json-rpc error %d", e.Code)
}

type incoming struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// call sends one request and waits for its response. Server-initiated
// requests are refused and notifications are ignored, because this adapter
// offers the runtime no client capabilities.
func (r *runtime) call(name method, params any, result any) error {
	if !allowedMethods[name] {
		return fmt.Errorf("copilot method %q is not allowed", name)
	}
	r.nextID++
	id := strconv.FormatInt(r.nextID, 10)
	if err := r.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": name, "params": params}); err != nil {
		return err
	}

	for {
		message, err := r.receive()
		if err != nil {
			return err
		}
		switch {
		case message.Method != "" && len(message.ID) > 0:
			refusal := map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": rpcError{Code: -32601, Message: "method not supported"}}
			if err := r.send(refusal); err != nil {
				return err
			}
		case message.Method != "":
			continue
		case string(message.ID) != id:
			continue
		case message.Error != nil:
			return message.Error
		case result == nil:
			return nil
		default:
			return json.Unmarshal(message.Result, result)
		}
	}
}

func (r *runtime) send(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	_, err = io.WriteString(r.stdin, frame)
	return err
}

func (r *runtime) receive() (incoming, error) {
	headers, err := textproto.NewReader(r.stdout).ReadMIMEHeader()
	if err != nil {
		return incoming{}, err
	}
	length, err := strconv.Atoi(headers.Get("Content-Length"))
	if err != nil || length <= 0 || length > maxFrameBytes {
		return incoming{}, errors.New("copilot runtime sent a frame without a valid Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r.stdout, body); err != nil {
		return incoming{}, err
	}
	var message incoming
	return message, json.Unmarshal(body, &message)
}
