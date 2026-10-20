package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"windowseat/internal/contracts"
)

var (
	// ErrNotRunning means nothing is listening on the service endpoint.
	ErrNotRunning = errors.New("windowseat service is not running")
	// ErrUntrustedEndpoint means another user's process holds the service endpoint.
	ErrUntrustedEndpoint = errors.New("windowseat endpoint is held by another user")
)

// Client talks to a running service. It satisfies Monitor.
type Client struct {
	http *http.Client
}

func NewClient(addr string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			conn, err := dial(ctx, addr)
			if errors.Is(err, ErrUntrustedEndpoint) {
				return nil, err
			}
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrNotRunning, err)
			}
			return conn, nil
		},
	}
	return &Client{http: &http.Client{Transport: transport}}
}

func (c *Client) Snapshot(ctx context.Context) (contracts.Snapshot, error) {
	var snapshot contracts.Snapshot
	err := c.get(ctx, "/v1/snapshot", &snapshot)
	return snapshot, err
}

func (c *Client) Health(ctx context.Context) (contracts.HealthReport, error) {
	var report contracts.HealthReport
	err := c.get(ctx, "/v1/health", &report)
	return report, err
}

func (c *Client) Refresh(ctx context.Context, provider contracts.Provider) error {
	path := "/v1/refresh"
	if provider != "" {
		path += "?provider=" + url.QueryEscape(string(provider))
	}
	return c.do(ctx, http.MethodPost, path, nil)
}

func (c *Client) get(ctx context.Context, path string, into any) error {
	return c.do(ctx, http.MethodGet, path, into)
}

func (c *Client) do(ctx context.Context, method, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://windowseat"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body errorBody
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return fmt.Errorf("windowseat service: %s: %s", resp.Status, body.Error)
	}
	if into == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
