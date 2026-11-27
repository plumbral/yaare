package ipc

import (
	"bytes"
	"context"
	"errors"

	"os"
	"path/filepath"
	"testing"
	filefs "yaare/internal/adapters/files"
)

func TestSCPRestartsOnlyCurrentFileAfterInterruptedPayload(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "source"), 0700)
	os.Mkdir(filepath.Join(dir, "target"), 0700)
	first := []byte("completed file")
	second := bytes.Repeat([]byte("second payload"), 20000)
	os.WriteFile(filepath.Join(dir, "source", "first"), first, 0600)
	os.WriteFile(filepath.Join(dir, "source", "second"), second, 0600)
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	f := filefs.Local{Root: root}
	items, total, e := filefs.Plan(context.Background(), f, "/source", []string{"first", "second"})
	if e != nil {
		t.Fatal(e)
	}
	cp := filefs.CopyCheckpoint{}
	pause, cancelPause := context.WithCancel(context.Background())
	e = filefs.CopyResumable(pause, f, f, items, "/target", "scp-partial", total, &cp, func(string, bool) (string, error) { return "overwrite", nil }, func(p filefs.Progress) {
		if p.Done > int64(len(first))+65536 {
			cancelPause()
		}
	}, func(string) {})
	if !errors.Is(e, context.Canceled) || cp.Index != 1 || cp.File == nil {
		t.Fatal("fixture interruption", e, cp.Index)
	}
	oldStage := cp.File.Temporary
	os.WriteFile(filepath.Join(dir, "source", "first"), []byte("must not replay completed file"), 0600)
	s, shutdown := NewFileService(nil)
	defer shutdown()
	page := new(callerScope)
	s.roots[page] = &localFolder{root: root, token: "scp-root"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := &fileJob{page: page, caller: context.Background(), ctx: ctx, cancel: cancel, items: items, total: total, checkpoint: cp, nonce: "scp-partial", request: TransferRequest{Protocol: "scp", Source: FileLocation{Side: "local", Path: "/source", Token: "scp-root"}, Destination: FileLocation{Side: "local", Path: "/target", Token: "scp-root"}, Names: []string{"first", "second"}, Policy: "overwrite"}, view: FileTask{ID: "scp-job", Protocol: "scp", State: "queued"}}
	s.jobs[job.view.ID] = job
	s.runTask(job)
	if job.view.State != "completed" || job.view.Done != total {
		t.Fatal("restart", job.view)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "target", "first"))
	if !bytes.Equal(b, first) {
		t.Fatal("completed file replayed")
	}
	b, _ = os.ReadFile(filepath.Join(dir, "target", "second"))
	if !bytes.Equal(b, second) {
		t.Fatal("current file incomplete")
	}
	if _, e = f.Lstat(oldStage); !os.IsNotExist(e) {
		t.Fatal("old uncommitted stage not removed", e)
	}
}
func TestSCPActiveTaskCannotPauseAndProtocolIsExplicit(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	job := &fileJob{executing: true, request: TransferRequest{Protocol: "scp"}, view: FileTask{State: "running"}, cancel: func() { t.Fatal("active SCP unexpectedly paused") }}
	if s.controlLocked(job, "pause") == nil {
		t.Fatal("SCP pause accepted")
	}
}
