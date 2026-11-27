package ipc

import (
	"context"
	"testing"
)

func TestFileConnectionsRejectsUnownedCaller(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	if _, e := s.FileConnections(context.Background()); e == nil {
		t.Fatal("unowned metadata request")
	}
}
