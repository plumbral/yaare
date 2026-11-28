package ipc

import (
	"context"
	"strings"
	"testing"
)

func TestBashIntegrationRequiresOwnedAttempt(t *testing.T) {
	service, closeAll := NewSSHService(NewKeyService())
	defer closeAll()
	ctx := context.Background()
	if _, err := service.PreviewBashIntegration(ctx, "session", 1); err == nil {
		t.Fatal("unowned preview accepted")
	}
	if err := service.ApplyBashIntegration(ctx, "session", 1, "install", strings.Repeat("a", 64)); err == nil {
		t.Fatal("unowned modification accepted")
	}
}
