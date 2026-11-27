package ipc

import (
	"context"
	"testing"
)

func TestClipboardRequiresCallingPageAndWindowWithoutReadingSystem(t *testing.T) {
	service := NewApplicationService(nil)
	if err := service.WriteClipboardText(context.Background(), "test"); err == nil {
		t.Fatal("unowned clipboard write accepted")
	}
	if text, err := service.ReadClipboardText(context.Background()); err == nil || text != "" {
		t.Fatal("unowned clipboard read accepted")
	}
}
