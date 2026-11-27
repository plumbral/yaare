package ipc

import (
	"context"

	"os"
	"path/filepath"
	"testing"
	"time"
	filefs "yaare/internal/adapters/files"
)

func TestAutomaticSyncCyclesUseQueueAndBaseline(t *testing.T) {
	dir := t.TempDir()
	for _, side := range []string{"left", "right"} {
		os.Mkdir(filepath.Join(dir, side), 0700)
	}
	put := func(side, name, content string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(dir, side, name), []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	put("left", "a", "same")
	put("right", "a", "same")
	put("left", "new-local", "from left")
	put("right", "new-remote", "from right")
	s, shutdown := NewFileService(nil)
	defer shutdown()
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	page := new(callerScope)
	s.roots[page] = &localFolder{root: root, token: "grant"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run := &directorySyncRun{page: page, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), base: filefs.SyncBaseline{}, request: ComparisonRequest{Left: FileLocation{Side: "local", Token: "grant", Path: "/left"}, Right: FileLocation{Side: "local", Token: "grant", Path: "/right"}, Recursive: true}, view: DirectorySyncView{ID: "test-sync", DeleteEnabled: true}}
	emit := func(view DirectorySyncView) error { return nil }
	cycle := func() {
		t.Helper()
		if e := s.directorySyncCycle(run, emit); e != nil {
			t.Fatal(e)
		}
	}
	cycle()
	if len(run.view.Actions) != 0 || len(s.jobs) != 2 {
		t.Fatal("initial queue copies", run.view, len(s.jobs))
	}
	firstRevision := run.view.Revision
	cycle()
	if firstRevision != run.view.Revision {
		t.Fatal("unchanged scans invalidated merge revisions")
	}
	put("left", "a", "modified local")
	cycle()
	content, _ := os.ReadFile(filepath.Join(dir, "right", "a"))
	if string(content) != "modified local" {
		t.Fatal("change not propagated")
	}
	os.Remove(filepath.Join(dir, "right", "a"))
	cycle()
	if _, e = os.Stat(filepath.Join(dir, "left", "a")); !os.IsNotExist(e) {
		t.Fatal("delete not propagated", e)
	}
	put("left", "new-local", "both-left")
	put("right", "new-local", "both-right")
	cycle()
	if len(run.view.Actions) != 1 || run.view.Actions[0].Kind != "conflict" {
		t.Fatal("both-side changes overwritten", run.view)
	}
	content, _ = os.ReadFile(filepath.Join(dir, "right", "new-local"))
	if string(content) != "both-right" {
		t.Fatal("conflict overwritten")
	}
}
func TestAutomaticSyncEntryPointsRequireCaller(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	ctx := context.Background()
	if _, e := s.PreviewDirectorySync(ctx, "id", true); e == nil {
		t.Fatal("preview without caller")
	}
	if e := s.DirectorySync(ctx, DirectorySyncRequest{Confirmed: true}, nil); e == nil {
		t.Fatal("unowned loop started")
	}
	if _, e := s.SyncConflictText(ctx, "id", "path", 1); e == nil {
		t.Fatal("unowned conflict read")
	}
	if e := s.ResolveDirectorySync(ctx, DirectorySyncResolution{ID: "id"}); e == nil {
		t.Fatal("unowned resolution")
	}
	if e := s.StopDirectorySync(ctx, "session"); e == nil {
		t.Fatal("unowned stop")
	}
}
