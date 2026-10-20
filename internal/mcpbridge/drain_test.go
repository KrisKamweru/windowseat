package mcpbridge

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"windowseat/internal/contracts"
)

// slowMonitor answers after a delay, so a reply is still pending when stdin closes.
type slowMonitor struct{ fakeMonitor }

func (m *slowMonitor) Snapshot(ctx context.Context) (contracts.Snapshot, error) {
	time.Sleep(100 * time.Millisecond)
	return m.fakeMonitor.Snapshot(ctx)
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Close() error { return nil }

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAnswersRequestsReceivedBeforeInputCloses(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"pipe","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_usage","arguments":{}}}`,
	}, "\n") + "\n"
	output := &syncBuffer{}
	transport := Draining(&mcp.IOTransport{Reader: io.NopCloser(strings.NewReader(input)), Writer: output})

	_ = NewServer(&slowMonitor{}, "test").Run(context.Background(), transport)

	if !strings.Contains(output.String(), `"id":2,"result"`) {
		t.Fatalf("no reply to the tools/call before exit; output:\n%s", output)
	}
}
