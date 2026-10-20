package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	methodInitialize     = "initialize"
	methodReadRateLimits = "account/rateLimits/read"
	maxMessageBytes      = 1 << 20
)

var errMalformedMessage = errors.New("malformed app-server message")

// allowedRequest is a complete, pre-encoded JSON-RPC line. The app-server also
// accepts state-changing methods such as account/rateLimitResetCredit/consume,
// so the package can only send the two values declared below.
type allowedRequest struct {
	id     int
	method string
	line   string
}

var (
	initializeRequest = allowedRequest{
		id:     1,
		method: methodInitialize,
		line:   `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"windowseat","version":"0.1.0"},"capabilities":null}}` + "\n",
	}
	readRateLimitsRequest = allowedRequest{
		id:     2,
		method: methodReadRateLimits,
		line:   `{"id":2,"method":"account/rateLimits/read","params":{"excludeResetCreditDetails":true}}` + "\n",
	}
)

type rpcMessage struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type rpcError struct {
	Method  string
	Code    int
	Message string
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%s: JSON-RPC error %d: %s", e.Method, e.Code, e.Message)
}

type incoming struct {
	message rpcMessage
	err     error
}

// exchange initializes the app-server and returns the result of one
// rate-limit read. Notifications are skipped; replies are matched by id.
func exchange(ctx context.Context, stdout io.Reader, stdin io.Writer) (json.RawMessage, error) {
	replies := make(chan incoming)
	go readReplies(ctx, stdout, replies)

	if _, err := call(ctx, stdin, replies, initializeRequest); err != nil {
		return nil, err
	}
	return call(ctx, stdin, replies, readRateLimitsRequest)
}

func call(ctx context.Context, stdin io.Writer, replies <-chan incoming, request allowedRequest) (json.RawMessage, error) {
	if _, err := io.WriteString(stdin, request.line); err != nil {
		return nil, fmt.Errorf("send %s: %w", request.method, err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case reply := <-replies:
			switch {
			case reply.err != nil:
				return nil, reply.err
			case *reply.message.ID != request.id:
				continue
			case reply.message.Error != nil:
				return nil, &rpcError{Method: request.method, Code: reply.message.Error.Code, Message: reply.message.Error.Message}
			default:
				return reply.message.Result, nil
			}
		}
	}
}

// readReplies forwards every message that carries an id, then one terminal
// error when the stream ends or a line is not JSON.
func readReplies(ctx context.Context, stdout io.Reader, replies chan<- incoming) {
	send := func(reply incoming) bool {
		select {
		case replies <- reply:
			return true
		case <-ctx.Done():
			return false
		}
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			send(incoming{err: fmt.Errorf("%w: %v", errMalformedMessage, err)})
			return
		}
		if message.ID == nil {
			continue
		}
		if !send(incoming{message: message}) {
			return
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	send(incoming{err: fmt.Errorf("app-server output ended before the reply: %w", err)})
}
