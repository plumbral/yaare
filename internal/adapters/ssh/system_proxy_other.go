//go:build !windows

package sshclient

import (
	"context"

	"yaare/internal/domain/workspace"
)

func systemProxy(_ context.Context, target string) (workspace.ProxyConfig, string, error) {
	return envSystemProxy(target)
}
