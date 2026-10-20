//go:build !windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// DefaultAddress is $XDG_RUNTIME_DIR/windowseat/windowseat.sock, falling
// back to the user cache directory.
func DefaultAddress() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		base = cache
	}
	return filepath.Join(base, "windowseat", "windowseat.sock"), nil
}

// Listen opens a socket that only the current user can reach. A socket
// left behind by a dead service is replaced; a live one is an error.
func Listen(addr string) (net.Listener, error) {
	dir := filepath.Dir(addr)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	if err := removeStaleSocket(addr); err != nil {
		return nil, err
	}

	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(addr, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func removeStaleSocket(addr string) error {
	info, err := os.Lstat(addr)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket", addr)
	}
	if conn, err := net.Dial("unix", addr); err == nil {
		conn.Close()
		return fmt.Errorf("another windowseat service is listening on %s", addr)
	}
	return os.Remove(addr)
}

func dial(ctx context.Context, addr string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", addr)
}
