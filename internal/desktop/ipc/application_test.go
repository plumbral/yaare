package ipc

import (
	"context"
	"testing"
)

func TestWindowFullscreenRequiresNativeCaller(t *testing.T) {
	service := NewApplicationService(nil)
	for _, ctx := range []context.Context{context.Background(), testOwner(context.Background())} {
		for _, enabled := range []bool{false, true} {
			if _, err := service.SetWindowFullscreen(ctx, enabled); err == nil {
				t.Fatal("a non-native caller must not control a desktop window")
			}
		}
	}
}
