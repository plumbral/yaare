//go:build !windows

package sshagent

import (
	"context"
	"net"
)

func dial(ctx context.Context, path string) (net.Conn, error) {
	if path == "" {
		return nil, ErrUnavailable
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", path)
}
