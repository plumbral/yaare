package ipc

import (
	"context"
	"github.com/egoist/mygo"
	"strings"
	"testing"
	"time"
)

func TestNativeDropRedactsSharedPathsAndScopesOneUseGrant(t *testing.T) {
	s, close := NewFileService(nil)
	defer close()
	page := &callerScope{}
	paths := []string{`C:\private\one.txt`, `C:\private\two.txt`}
	event := &mygo.FileDropEvent{Paths: paths, X: 10, Y: 20}
	s.captureDrop(page, event)
	if !strings.HasPrefix(paths[0], "yaare-drop:") || paths[1] != "" {
		t.Fatal("native shared slice not redacted")
	}
	grant := s.drops[paths[0]]
	if grant.page != page || len(grant.paths) != 2 || grant.paths[0] != `C:\private\one.txt` {
		t.Fatal("backend grant lost")
	}
	invalid := make([]string, 257)
	for i := range invalid {
		invalid[i] = "private"
	}
	s.captureDrop(page, &mygo.FileDropEvent{Paths: invalid})
	for _, p := range invalid {
		if p != "" {
			t.Fatal("invalid drop leaked")
		}
	}
	for id, d := range s.drops {
		d.at = time.Now().Add(-3 * time.Minute)
		s.drops[id] = d
	}
	s.captureDrop(page, &mygo.FileDropEvent{Paths: []string{"new"}})
	if len(s.drops) != 1 {
		t.Fatal("unclaimed drops unbounded")
	}
	if _, err := s.UploadDropped(context.Background(), paths[0], FileLocation{}); err == nil {
		t.Fatal("unowned upload")
	}
	if _, err := s.Transfer(context.Background(), TransferRequest{Source: FileLocation{Side: "drop"}}); err == nil {
		t.Fatal("public arbitrary drop source")
	}
}
