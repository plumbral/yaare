package ipc

import (
	"context"
	"errors"

	"github.com/pkg/sftp"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileServiceRequiresOwnedCallerAndClassifiesSafeErrors(t *testing.T) {
	s, closeService := NewFileService(nil)
	defer closeService()
	if _, err := s.List(context.Background(), FileLocation{Side: "local"}); err == nil {
		t.Fatal("unowned list accepted")
	}
	for _, ctx := range []context.Context{context.Background(), testOwner(context.Background())} {
		if _, err := s.OpenLocalHome(ctx); err == nil {
			t.Fatal("non-native home directory access accepted")
		}
	}
	if err := s.Search(context.Background(), FileSearchRequest{}, nil); err == nil {
		t.Fatal("unowned search accepted")
	}
	if _, err := s.Transfer(context.Background(), TransferRequest{}); err == nil {
		t.Fatal("unowned transfer accepted")
	}
	if err := s.Control(context.Background(), "other", "cancel"); err == nil {
		t.Fatal("unowned control accepted")
	}
	if _, err := s.ReadText(context.Background(), FileLocation{Side: "remote", Path: "/etc/passwd"}); err == nil {
		t.Fatal("unowned text read accepted")
	}
	if _, err := s.SaveText(context.Background(), TextSaveRequest{}); err == nil {
		t.Fatal("unowned text save accepted")
	}
	if err := s.SetTextProtection(context.Background(), 1, true); err == nil {
		t.Fatal("unowned native protection accepted")
	}
	for _, item := range []struct {
		err  error
		code string
	}{{fs.ErrPermission, "FILE_PERMISSION"}, {fs.ErrNotExist, "FILE_NOT_FOUND"}, {io.EOF, "SFTP_DISCONNECTED"}, {context.DeadlineExceeded, "FILE_TIMEOUT"}, {&sftp.StatusError{Code: 3}, "FILE_PERMISSION"}, {errors.New("PRIVATE MATERIAL must never appear"), "FILE_FAILED"}} {
		code, message := fileError(item.err)
		if code != item.code || message == item.err.Error() {
			t.Fatal("unsafe classification", code)
		}
		nestedCode, _ := fileError(fileProblem(item.err))
		if nestedCode != code {
			t.Fatal("wrapped error lost safe code")
		}
	}
}
func TestFileTasksIsolateFailureAndSnapshotsByPage(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	page := &callerScope{}
	other := &callerScope{}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "source"), 0700)
	os.Mkdir(filepath.Join(dir, "target"), 0700)
	os.WriteFile(filepath.Join(dir, "source", "good.txt"), []byte("complete bytes"), 0600)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.roots[page] = &localFolder{root: root, token: "owned", name: "test"}
	makeJob := func(id, name string) *fileJob {
		ctx, cancel := context.WithCancel(context.Background())
		j := &fileJob{page: page, ctx: ctx, cancel: cancel, caller: context.Background(), view: FileTask{ID: id, Created: 1, State: "queued"}, request: TransferRequest{Source: FileLocation{Side: "local", Token: "owned", Path: "/source"}, Destination: FileLocation{Side: "local", Token: "owned", Path: "/target"}, Names: []string{name}, Policy: "ask"}}
		s.jobs[id] = j
		return j
	}
	failed := makeJob("failed", "missing.txt")
	s.runTask(failed)
	if failed.view.State != "failed" || failed.view.Code != "FILE_NOT_FOUND" {
		t.Fatal(failed.view)
	}
	good := makeJob("good", "good.txt")
	s.runTask(good)
	if good.view.State != "completed" || good.view.Done != 14 {
		t.Fatal(good.view)
	}
	data, err := os.ReadFile(filepath.Join(dir, "target", "good.txt"))
	if err != nil || string(data) != "complete bytes" {
		t.Fatal("bad committed data", err)
	}
	if len(s.taskSnapshot(other).Tasks) != 0 || len(s.taskSnapshot(page).Tasks) != 2 {
		t.Fatal("task ownership leaked")
	}
	conflict := makeJob("conflict", "good.txt")
	done := make(chan struct{})
	go func() { s.runTask(conflict); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		state, total := conflict.view.State, conflict.view.Total
		s.mu.Unlock()
		if state == "conflict" {
			if total != 14 {
				t.Fatal("conflict lost planned size", total)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("conflict was not offered")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("conflict did not resolve")
	}
	s.mu.Lock()
	if err := s.controlLocked(conflict, "keepBoth"); err != nil {
		t.Fatal(err)
	}
	claimed := s.claimLocked()
	s.mu.Unlock()
	if len(claimed) != 1 {
		t.Fatal("conflict should release and reacquire a slot")
	}
	s.runTask(conflict)
	if conflict.view.State != "completed" {
		t.Fatal(conflict.view)
	}
	if _, err = os.Stat(filepath.Join(dir, "target", "good (2).txt")); err != nil {
		t.Fatal("keep both did not preserve files", err)
	}
	cancelled := makeJob("cancelled", "good.txt")
	cancelled.intent = "cancel"
	cancelled.cancel()
	s.runTask(cancelled)
	if cancelled.view.State != "cancelled" {
		t.Fatal(cancelled.view)
	}
}

func TestLocalFolderDisplayName(t *testing.T) {
	if got := localFolderDisplayName(filepath.Join("C:"+string(filepath.Separator), "Users", "aaa")); got != "aaa" {
		t.Fatalf("unexpected folder name %q", got)
	}
	root := filepath.Clean(string(filepath.Separator))
	if got := localFolderDisplayName(root); got != root {
		t.Fatalf("expected root %q, got %q", root, got)
	}
	if vol := filepath.VolumeName(t.TempDir()); vol != "" {
		driveRoot := vol + string(filepath.Separator)
		if got := localFolderDisplayName(driveRoot); got != driveRoot {
			t.Fatalf("expected drive root %q, got %q", driveRoot, got)
		}
	}
}
