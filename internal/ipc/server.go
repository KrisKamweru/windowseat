// Package ipc serves the Windowseat API over a user-private local endpoint:
// a Unix socket on Linux and macOS, a named pipe on Windows.
package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"windowseat/internal/contracts"
	"windowseat/internal/service"
)

// Monitor is what the API exposes: read-only data plus on-demand refresh.
type Monitor interface {
	Snapshot(ctx context.Context) (contracts.Snapshot, error)
	Health(ctx context.Context) (contracts.HealthReport, error)
	Refresh(ctx context.Context, provider contracts.Provider) error
}

// Serve answers API requests on ln until ctx is cancelled.
func Serve(ctx context.Context, ln net.Listener, monitor Monitor) error {
	server := &http.Server{Handler: Handler(monitor), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func Handler(monitor Monitor) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		respond(w, snapshotFor(r.Context(), monitor, ""))
	})
	mux.HandleFunc("GET /v1/snapshot/{provider}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, snapshotFor(r.Context(), monitor, contracts.Provider(r.PathValue("provider"))))
	})
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, func() (any, error) { return monitor.Health(r.Context()) })
	})
	mux.HandleFunc("POST /v1/refresh", func(w http.ResponseWriter, r *http.Request) {
		provider := contracts.Provider(r.URL.Query().Get("provider"))
		if err := monitor.Refresh(r.Context(), provider); err != nil {
			writeError(w, err)
			return
		}
		respond(w, snapshotFor(r.Context(), monitor, provider))
	})
	return mux
}

func snapshotFor(ctx context.Context, monitor Monitor, provider contracts.Provider) func() (any, error) {
	return func() (any, error) {
		snapshot, err := monitor.Snapshot(ctx)
		if err != nil || provider == "" {
			return snapshot, err
		}
		return service.Select(snapshot, provider)
	}
}

func respond(w http.ResponseWriter, produce func() (any, error)) {
	body, err := produce()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, statusFor(err), errorBody{Error: err.Error()})
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, service.ErrUnknownProvider):
		return http.StatusNotFound
	case errors.Is(err, service.ErrNoProviders):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
