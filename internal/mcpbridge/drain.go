package mcpbridge

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Draining wraps a transport so that when input ends, every request already
// received is answered before the session sees the end. The SDK otherwise
// cancels in-flight handlers and drops their replies as soon as input ends,
// which loses every answer when a client writes its requests and closes stdin.
func Draining(inner mcp.Transport) mcp.Transport {
	return drainingTransport{inner}
}

type drainingTransport struct{ inner mcp.Transport }

func (t drainingTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &drainingConn{
		Connection: conn,
		answered:   make(chan struct{}, 1),
		closed:     make(chan struct{}),
	}, nil
}

type drainingConn struct {
	mcp.Connection
	unanswered atomic.Int64
	answered   chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
}

func (c *drainingConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if err != nil {
		c.awaitAnswers(ctx)
		return nil, err
	}
	if request, ok := msg.(*jsonrpc.Request); ok && request.IsCall() {
		c.unanswered.Add(1)
	}
	return msg, nil
}

func (c *drainingConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := c.Connection.Write(ctx, msg)
	if _, ok := msg.(*jsonrpc.Response); ok {
		c.unanswered.Add(-1)
		select {
		case c.answered <- struct{}{}:
		default:
		}
	}
	return err
}

func (c *drainingConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Connection.Close()
}

func (c *drainingConn) awaitAnswers(ctx context.Context) {
	for c.unanswered.Load() > 0 {
		select {
		case <-c.answered:
		case <-c.closed:
			return
		case <-ctx.Done():
			return
		}
	}
}
