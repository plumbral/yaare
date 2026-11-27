package ipc

import (
	"context"

	"os"
	"path/filepath"
	"testing"
	filefs "yaare/internal/adapters/files"
)

func TestComparisonIPCRejectsUnownedCalls(t *testing.T) {
	s, close := NewFileService(nil)
	defer close()
	ctx := context.Background()
	if s.Compare(ctx, ComparisonRequest{}, nil) == nil {
		t.Fatal("unowned compare")
	}
	if _, e := s.ComparisonText(ctx, "id", "x", "auto", "auto"); e == nil {
		t.Fatal("unowned text")
	}
	if _, e := s.PreviewSync(ctx, "id", "upload", []string{"x"}); e == nil {
		t.Fatal("unowned preview")
	}
	if _, e := s.ExecuteSync(ctx, "id", true); e == nil {
		t.Fatal("unowned execute")
	}
	if s.ForgetComparison(ctx, "id") == nil || s.DiscardSync(ctx, "id") == nil {
		t.Fatal("unowned discard")
	}
}
func TestReviewedComparisonUsesExistingTaskQueue(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "left"), 0700)
	os.Mkdir(filepath.Join(dir, "right"), 0700)
	os.WriteFile(filepath.Join(dir, "left", "new.txt"), []byte("confirmed"), 0600)
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	f := filefs.Local{Root: root}
	c, e := filefs.CompareTrees(context.Background(), f, f, "/left", "/right", false, false, func(int, int64) {})
	if e != nil {
		t.Fatal(e)
	}
	plan, e := filefs.BuildReviewedSync(c, "/left", "/right", "upload", []string{"new.txt"})
	if e != nil {
		t.Fatal(e)
	}
	s, close := NewFileService(nil)
	defer close()
	page := &callerScope{}
	s.roots[page] = &localFolder{root: root, token: "root", name: "fixture"}
	ctx, cancel := context.WithCancel(context.Background())
	job := &fileJob{page: page, caller: context.Background(), ctx: ctx, cancel: cancel, review: plan, request: TransferRequest{Source: FileLocation{Side: "local", Path: "/left", Token: "root"}, Destination: FileLocation{Side: "local", Path: "/right", Token: "root"}, Names: []string{"comparison"}, Policy: "overwrite"}, view: FileTask{ID: "reviewed-task", State: "queued", Reviewed: true}}
	s.jobs[job.view.ID] = job
	s.runTask(job)
	if job.view.State != "completed" || job.view.Done != 9 || job.review != nil {
		t.Fatal("task", job.view)
	}
	got, e := os.ReadFile(filepath.Join(dir, "right", "new.txt"))
	if e != nil || string(got) != "confirmed" {
		t.Fatal(e)
	}
	if s.controlLocked(job, "retry") == nil {
		t.Fatal("reviewed task retried without new preview")
	}
}
