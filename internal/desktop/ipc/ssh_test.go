package ipc

import (
	"context"
	"testing"
)

func TestSSHServiceRejectsCallsWithoutAnOwnedPage(t *testing.T) {
	service, closeAll := NewSSHService(NewKeyService())
	defer closeAll()
	ctx := context.Background()
	if err := service.Connect(ctx, SSHConnectRequest{}, nil); err == nil {
		t.Fatal("non-page caller reached SSH connect")
	}
	if service.Write(ctx, "session", 1, "YWJj") || service.Resize(ctx, "session", 1, 80, 24) || service.ConfirmHost(ctx, "session", 1, "challenge", "remember") || service.SetForward(ctx, "session", 1, "rule", true) {
		t.Fatal("unowned page accessed session")
	}
	service.Disconnect(ctx, "session", 1)
	if service.ChangeDirectory(ctx, "session", 1, "/tmp") == nil {
		t.Fatal("unowned page changed a terminal directory")
	}
	if len(service.runs) != 0 {
		t.Fatal("rejected call created a session")
	}
}
