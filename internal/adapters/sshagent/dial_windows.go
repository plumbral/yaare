//go:build windows

package sshagent

import (
	"context"
	winio "github.com/Microsoft/go-winio"
	"net"
	"strings"
)

func dial(ctx context.Context, path string) (net.Conn, error) {
	if path == "" {
		path = `\\.\pipe\openssh-ssh-agent`
	}
	if strings.HasPrefix(strings.ToLower(path), `\\.\pipe\`) {
		return winio.DialPipeContext(ctx, path)
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", path)
}
