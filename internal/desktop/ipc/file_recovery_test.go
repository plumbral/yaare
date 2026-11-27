//go:build windows

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo"
	"os"
	"path/filepath"
	"testing"
	"time"
	filefs "yaare/internal/adapters/files"
)

func TestRelayRecoveryDisplaysSavedLabelsOrPublicEndpoints(t *testing.T) {
	service, shutdown := NewFileService(nil)
	defer shutdown()
	unlabeled := transferRecord{
		Request:  TransferRequest{Source: FileLocation{Side: "remote"}, Destination: FileLocation{Side: "remote"}},
		Bindings: [2]string{"[2001:db8::1]:22\x00qa\x00private-fingerprint", "b.example:2202\x00release\x00private-fingerprint"},
		View:     FileTask{ID: "unlabeled", Created: 1},
	}
	named := unlabeled
	named.View = FileTask{ID: "named", Created: 2, SourceLabel: "Build A", DestinationLabel: "Build B"}
	service.recoverable = map[string]transferRecord{"unlabeled": unlabeled, "named": named}
	items := service.recoveryView(nil).Items
	if len(items) != 2 || items[0].Source != "qa@[2001:db8::1]:22" || items[0].Destination != "release@b.example:2202" ||
		items[1].Source != "Build A" || items[1].Destination != "Build B" {
		t.Fatal("recovered endpoints must use their reviewed labels or the original public connection")
	}
	if recoveryLabel(FileLocation{Side: "remote"}, "", "") != "远程服务器" {
		t.Fatal("a remote endpoint without a public address needs a display label")
	}
}

func TestRecoveryChecksManifestVersionAgainstProtocol(t *testing.T) {
	dir := t.TempDir()
	service, shutdown := NewFileService(nil)
	defer shutdown()
	ConfigureTransferRecovery(service, func() (string, error) { return dir, nil })
	InitializeTransferRecovery(service)
	cases := []struct {
		id, protocol string
		version      int
		want         bool
	}{
		{"default", "", 1, true},
		{"scp", "scp", 2, true},
		{"scp-mismatch", "scp", 1, false},
		{"default-mismatch", "", 2, false},
		{"unknown-version", "", 3, false},
		{"missing-version", "", 0, false},
		{"unknown-protocol", "unknown", 1, false},
	}
	for _, tc := range cases {
		manifest := transferManifest{ID: tc.id, Nonce: tc.id, Version: tc.version, Request: TransferRequest{Protocol: tc.protocol}}
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err = service.recovery.SaveManifest(tc.id, raw); err != nil {
			t.Fatal(err)
		}
		state, err := json.Marshal(transferState{View: FileTask{ID: tc.id, State: "queued"}, Checkpoint: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		if err = service.recovery.SaveState(tc.id, state); err != nil {
			t.Fatal(err)
		}
	}
	InitializeTransferRecovery(service)
	for _, tc := range cases {
		if _, found := service.recoverable[tc.id]; found != tc.want {
			t.Errorf("%s: recoverable = %t, want %t", tc.id, found, tc.want)
		}
	}
	if service.recoveryProblem == "" {
		t.Error("expected a warning for unsupported records")
	}
}

func TestTransferRecoveryRequiresOriginalGrantAndExplicitResume(t *testing.T) {
	for _, mode := range []string{"directory", "reviewed", "dropped", "queued", "scp"} {
		t.Run(mode, func(t *testing.T) { testTransferRecovery(t, mode) })
	}
}
func TestQueuedTransferIsDurableBeforeSchedulerStarts(t *testing.T) {
	service, shutdown := NewFileService(nil)
	defer shutdown()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	service.roots[nil] = &localFolder{root: root, token: "queue-grant"}
	appDir := t.TempDir()
	ConfigureTransferRecovery(service, func() (string, error) { return appDir, nil })
	InitializeTransferRecovery(service)
	for i := 0; i < 8; i++ {
		service.jobs[fmt.Sprint(i)] = &fileJob{executing: true, cancel: func() {}, view: FileTask{State: "running"}}
	}
	id, err := service.enqueueTransfer(context.Background(), TransferRequest{Source: FileLocation{Side: "local", Token: "queue-grant", Path: "/source"}, Destination: FileLocation{Side: "local", Token: "queue-grant", Path: "/target"}, Names: []string{"queued-file"}, Policy: "ask"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, closeNext := NewFileService(nil)
	defer closeNext()
	ConfigureTransferRecovery(next, func() (string, error) { return appDir, nil })
	InitializeTransferRecovery(next)
	if record, ok := next.recoverable[id]; !ok || len(record.Items) != 0 || record.View.State != "queued" {
		t.Fatal("queued request was not durable before a worker was available")
	}
}
func TestSchedulerSkipsCheckpointPreparation(t *testing.T) {
	service, shutdown := NewFileService(nil)
	defer shutdown()
	job := &fileJob{preparing: true, view: FileTask{State: "queued"}, cancel: func() {}}
	service.jobs["preparing"] = job
	if len(service.claimLocked()) != 0 {
		t.Fatal("worker raced checkpoint preparation")
	}
	job.preparing = false
	if len(service.claimLocked()) != 1 {
		t.Fatal("prepared job was not claimable")
	}
}
func testTransferRecovery(t *testing.T, mode string) {
	if mode != "directory" && os.Getenv("YAARE_RECOVERY_FIXTURE_DIR") != "" {
		t.Skip("native fixture uses directory mode")
	}
	appDir := t.TempDir()
	filesDir := t.TempDir()
	if fixture := os.Getenv("YAARE_RECOVERY_FIXTURE_DIR"); fixture != "" {
		if !filepath.IsAbs(fixture) {
			t.Fatal("absolute fixture root required")
		}
		appDir = filepath.Join(fixture, "app")
		filesDir = filepath.Join(fixture, "local")
		if err := os.MkdirAll(appDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filesDir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	os.Mkdir(filepath.Join(filesDir, "source"), 0700)
	os.Mkdir(filepath.Join(filesDir, "target"), 0700)
	content := bytes.Repeat([]byte("restart-checkpoint-"), 20000)
	os.WriteFile(filepath.Join(filesDir, "source", "large"), content, 0600)
	root, err := os.OpenRoot(filesDir)
	if err != nil {
		t.Fatal(err)
	}
	local := filefs.Local{Root: root}
	var source filefs.FS = local
	sourcePath := "/source"
	paths := []string{filepath.Join(filesDir, "source", "large")}
	if mode == "dropped" {
		drop, e := filefs.OpenDropped(paths)
		if e != nil {
			t.Fatal(e)
		}
		defer drop.Close()
		source = drop
		sourcePath = "/"
	}
	items, total, err := filefs.Plan(context.Background(), source, sourcePath, []string{"large"})
	if err != nil {
		t.Fatal(err)
	}
	cp := filefs.CopyCheckpoint{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if mode != "queued" {
		err = filefs.CopyResumable(ctx, source, local, items, "/target", "restore-task", total, &cp, func(string, bool) (string, error) { return "overwrite", nil }, func(p filefs.Progress) {
			if p.Done >= 65536 {
				cancel()
			}
		}, func(string) {})
	}
	if mode != "queued" && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	page := new(callerScope)
	first, closeFirst := NewFileService(nil)
	ConfigureTransferRecovery(first, func() (string, error) { return appDir, nil })
	InitializeTransferRecovery(first)
	first.roots[page] = &localFolder{root: root, token: "old-grant"}
	job := &fileJob{page: page, items: items, total: total, nonce: "restore-task", checkpoint: cp, view: FileTask{ID: "restore-task", Name: "large", State: "paused", Direction: "local-local", CanResume: true, Done: cp.Progress.Done, Total: total}, request: TransferRequest{Source: FileLocation{Side: "local", Token: "old-grant", Path: "/source"}, Destination: FileLocation{Side: "local", Token: "old-grant", Path: "/target"}, Names: []string{"large"}, Policy: "ask"}}
	if mode == "dropped" {
		job.dropPaths = paths
		job.request.Source = FileLocation{Side: "drop", Path: "/"}
	}
	if mode == "scp" {
		job.request.Protocol = "scp"
		job.view.Protocol = "scp"
	}
	if mode == "reviewed" {
		comparison, e := filefs.CompareTrees(context.Background(), local, local, "/source", "/target", true, true, func(int, int64) {})
		if e != nil {
			t.Fatal(e)
		}
		job.review, e = filefs.BuildReviewedSync(comparison, "/source", "/target", "upload", []string{"large"})
		if e != nil {
			t.Fatal(e)
		}
		job.view.Reviewed = true
	}
	if mode == "queued" {
		job.items = nil
		job.total = 0
		job.view.Total = 0
		job.view.Done = 0
		job.view.State = "queued"
		job.view.CanResume = false
	}
	first.persistJob(job, true)
	closeFirst()
	if os.Getenv("YAARE_RECOVERY_FIXTURE_DIR") != "" {
		t.Log("isolated native recovery fixture saved")
		return
	}
	second, closeSecond := NewFileService(nil)
	defer closeSecond()
	ConfigureTransferRecovery(second, func() (string, error) { return appDir, nil })
	InitializeTransferRecovery(second)
	view := second.recoveryView(page)
	if len(view.Items) != 1 || len(second.jobs) != 0 {
		t.Fatal("restart autoqueued or lost record")
	}
	encoded, _ := json.Marshal(view)
	if bytes.Contains(encoded, []byte(filesDir)) {
		t.Fatal("absolute local path exposed")
	}
	request := RestoreTransferRequest{ID: "restore-task"}
	if _, err = second.restoreTransfer(context.Background(), page, request); err == nil {
		t.Fatal("restored without local grant")
	}
	wrong, _ := os.OpenRoot(t.TempDir())
	second.roots[page] = &localFolder{root: wrong, token: "wrong"}
	if _, err = second.restoreTransfer(context.Background(), page, request); err == nil {
		t.Fatal("restored wrong local directory")
	}
	wrong.Close()
	chosen, _ := os.OpenRoot(filesDir)
	second.roots[page] = &localFolder{root: chosen, token: "new-grant"}
	if mode == "dropped" {
		event := &mygo.FileDropEvent{Paths: append([]string(nil), paths...)}
		second.captureDrop(page, event)
		if e := second.authorizeRecoveredDrop(page, request.ID, event.Paths[0]); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = second.restoreTransfer(context.Background(), page, request); err != nil {
		t.Fatal(err)
	}
	if _, err = second.restoreTransfer(context.Background(), page, request); err == nil {
		t.Fatal("duplicate restore accepted")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		second.mu.Lock()
		state := second.jobs[request.ID].view.State
		executing := second.jobs[request.ID].executing
		second.mu.Unlock()
		if state == "completed" && !executing {
			break
		}
		if state == "failed" {
			t.Fatal("resumed task failed")
		}
		if time.Now().After(deadline) {
			t.Fatal("resume did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	actual, _ := os.ReadFile(filepath.Join(filesDir, "target", "large"))
	if !bytes.Equal(actual, content) {
		t.Fatal("restored transfer bytes differ")
	}
	third, closeThird := NewFileService(nil)
	defer closeThird()
	ConfigureTransferRecovery(third, func() (string, error) { return appDir, nil })
	InitializeTransferRecovery(third)
	if len(third.recoverable) != 0 {
		t.Fatal("completed transfer remained recoverable")
	}
	if _, err = second.RecoverableTransfers(context.Background()); err == nil {
		t.Fatal("unowned recovery list")
	}
}

func TestRecoverySplitsManifestAndAsyncState(t *testing.T) {
	appDir := t.TempDir()
	filesDir := t.TempDir()
	root, err := os.OpenRoot(filesDir)
	if err != nil {
		t.Fatal(err)
	}
	page := new(callerScope)
	first, closeFirst := NewFileService(nil)
	defer closeFirst()
	ConfigureTransferRecovery(first, func() (string, error) { return appDir, nil })
	InitializeTransferRecovery(first)
	first.roots[page] = &localFolder{root: root, token: "split-grant"}
	job := &fileJob{
		page:  page,
		items: []filefs.Item{{Source: "/src/a", Relative: "a", Size: 100}},
		total: 100,
		nonce: "split-task",
		view:  FileTask{ID: "split-task", Name: "a", State: "running", Direction: "local-local", Done: 10, Total: 100},
		request: TransferRequest{
			Source:      FileLocation{Side: "local", Token: "split-grant", Path: "/src"},
			Destination: FileLocation{Side: "local", Token: "split-grant", Path: "/dst"},
			Names:       []string{"a"},
			Policy:      "ask",
		},
	}
	first.persistJob(job, true)
	manifestPath := filepath.Join(appDir, "transfer-recovery", "split-task.manifest")
	statePath := filepath.Join(appDir, "transfer-recovery", "split-task.state")
	manifestBefore, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("manifest missing after initial persist: %v", err)
	}
	time.Sleep(25 * time.Millisecond)
	for _, done := range []int64{25, 50, 75} {
		job.view.Done = done
		job.checkpoint.Progress.Done = done
		job.lastCheckpoint = time.Now().Add(-3 * time.Second)
		first.persistJob(job, false)
	}
	closeFirst()
	manifestAfter, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("manifest missing after state updates: %v", err)
	}
	if !manifestAfter.ModTime().Equal(manifestBefore.ModTime()) || manifestAfter.Size() != manifestBefore.Size() {
		t.Fatalf("manifest was rewritten during async state-only updates: before=%v after=%v", manifestBefore.ModTime(), manifestAfter.ModTime())
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing after flush: %v", err)
	}
	second, closeSecond := NewFileService(nil)
	defer closeSecond()
	ConfigureTransferRecovery(second, func() (string, error) { return appDir, nil })
	InitializeTransferRecovery(second)
	rec, ok := second.recoverable["split-task"]
	if !ok || rec.View.Done != 75 || len(rec.Items) != 1 {
		t.Fatalf("unexpected restored split recovery record: ok=%v rec=%+v", ok, rec)
	}
}
