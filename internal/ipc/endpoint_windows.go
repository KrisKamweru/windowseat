//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os/user"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// DefaultAddress is a named pipe that is unique to the current user.
func DefaultAddress() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", err
	}
	name := strings.ReplaceAll(current.Username, `\`, "-")
	return `\\.\pipe\windowseat-` + name, nil
}

// Listen opens a named pipe whose DACL grants access to the current user only.
func Listen(addr string) (net.Listener, error) {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := tokenUser.User.Sid.String()
	return winio.ListenPipe(addr, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")",
	})
}

// dial connects only to a pipe served by the current user. The pipe name is
// predictable, so another local user could create it while the service is down.
func dial(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := winio.DialPipeContext(ctx, addr)
	if err != nil {
		return nil, err
	}
	if err := requireOwnServer(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func requireOwnServer(conn net.Conn) error {
	pipe, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return fmt.Errorf("%w: cannot inspect the pipe", ErrUntrustedEndpoint)
	}
	var serverPID uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(pipe.Fd()), &serverPID); err != nil {
		return fmt.Errorf("%w: %w", ErrUntrustedEndpoint, err)
	}
	server, err := processUser(serverPID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUntrustedEndpoint, err)
	}
	current, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if !server.User.Sid.Equals(current.User.Sid) {
		return ErrUntrustedEndpoint
	}
	return nil
}

func processUser(pid uint32) (*windows.Tokenuser, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return nil, err
	}
	defer token.Close()
	return token.GetTokenUser()
}
