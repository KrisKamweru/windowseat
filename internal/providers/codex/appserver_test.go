package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeServer struct {
	reply    func(request rpcMessage) []string
	received []rpcMessage
}

// serve plays the app-server side of the pipe pair until the client stops writing.
func (server *fakeServer) serve(t *testing.T, fromClient io.Reader, toClient io.WriteCloser) {
	t.Helper()
	defer toClient.Close()
	scanner := bufio.NewScanner(fromClient)
	for scanner.Scan() {
		var request rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			t.Errorf("client sent invalid JSON: %q", scanner.Text())
			return
		}
		server.received = append(server.received, request)
		for _, line := range server.reply(request) {
			if _, err := io.WriteString(toClient, line+"\n"); err != nil {
				return
			}
		}
	}
}

func runExchange(t *testing.T, server *fakeServer, timeout time.Duration) (json.RawMessage, error) {
	t.Helper()
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	done := make(chan struct{})
	go func() {
		server.serve(t, serverReads, serverWrites)
		close(done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := exchange(ctx, clientReads, clientWrites)
	clientWrites.Close()
	clientReads.Close()
	<-done
	return result, err
}

func codexLikeServer(rateLimitsReply string) *fakeServer {
	return &fakeServer{reply: func(request rpcMessage) []string {
		switch request.Method {
		case "initialize":
			return []string{
				`{"method":"remoteControl/status/changed","params":{}}`,
				`{"id":1,"result":{"userAgent":"codex/0.163.0"}}`,
			}
		case "account/rateLimits/read":
			return []string{`{"method":"account/updated","params":{}}`, rateLimitsReply}
		}
		return nil
	}}
}

func TestExchangeSendsOnlyAllowlistedRequestsAndReturnsTheReadResult(t *testing.T) {
	server := codexLikeServer(`{"id":2,"result":{"rateLimits":null}}`)

	result, err := runExchange(t, server, time.Second)

	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if string(result) != `{"rateLimits":null}` {
		t.Errorf("result = %s", result)
	}
	var methods []string
	for _, request := range server.received {
		methods = append(methods, request.Method)
	}
	if strings.Join(methods, ",") != "initialize,account/rateLimits/read" {
		t.Errorf("methods = %v", methods)
	}
	if string(server.received[1].Params) != `{"excludeResetCreditDetails":true}` {
		t.Errorf("read params = %s", server.received[1].Params)
	}
}

func TestExchangeReportsJSONRPCErrorFromRateLimitRead(t *testing.T) {
	server := codexLikeServer(`{"id":2,"error":{"code":-32600,"message":"not logged in"}}`)

	_, err := runExchange(t, server, time.Second)

	var rpcErr *rpcError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err = %v, want *rpcError", err)
	}
	if rpcErr.Method != "account/rateLimits/read" || rpcErr.Message != "not logged in" {
		t.Errorf("rpcErr = %+v", rpcErr)
	}
}

func TestExchangeReportsJSONRPCErrorFromInitialize(t *testing.T) {
	server := &fakeServer{reply: func(rpcMessage) []string {
		return []string{`{"id":1,"error":{"code":-1,"message":"boom"}}`}
	}}

	_, err := runExchange(t, server, time.Second)

	var rpcErr *rpcError
	if !errors.As(err, &rpcErr) || rpcErr.Method != "initialize" {
		t.Fatalf("err = %v, want initialize *rpcError", err)
	}
}

func TestExchangeRejectsMalformedLines(t *testing.T) {
	server := codexLikeServer(`not json`)

	_, err := runExchange(t, server, time.Second)

	if !errors.Is(err, errMalformedMessage) {
		t.Fatalf("err = %v, want errMalformedMessage", err)
	}
}

func TestExchangeFailsWhenServerClosesBeforeReply(t *testing.T) {
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	go func() {
		scanner := bufio.NewScanner(serverReads)
		scanner.Scan()
		io.WriteString(serverWrites, `{"id":1,"result":{}}`+"\n")
		serverWrites.Close()
		io.Copy(io.Discard, serverReads)
	}()

	_, err := exchange(context.Background(), clientReads, clientWrites)
	clientWrites.Close()

	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestExchangeStopsWaitingWhenContextExpires(t *testing.T) {
	server := &fakeServer{reply: func(rpcMessage) []string { return nil }}

	_, err := runExchange(t, server, 20*time.Millisecond)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}
