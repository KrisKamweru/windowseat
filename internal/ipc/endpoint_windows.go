//go:build windows

package ipc

import (
	"context"
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

func dial(ctx context.Context, addr string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, addr)
}
