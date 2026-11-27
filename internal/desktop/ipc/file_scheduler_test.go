package ipc

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	filefs "yaare/internal/adapters/files"
)

func TestTaskResumesCheckpointAndFreesPlanMemory(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	page := &callerScope{}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "source"), 0700)
	os.Mkdir(filepath.Join(dir, "target"), 0700)
	data := bytes.Repeat([]byte("checkpoint"), 50000)
	os.WriteFile(filepath.Join(dir, "source", "large"), data, 0600)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.roots[page] = &localFolder{root: root, token: "owned"}
	local := filefs.Local{Root: root}
	items, total, err := filefs.Plan(context.Background(), local, "/source", []string{"large"})
	if err != nil {
		t.Fatal(err)
	}
	cp := filefs.CopyCheckpoint{}
	paused, stop := context.WithCancel(context.Background())
	err = filefs.CopyResumable(paused, local, local, items, "/target", "task-resume", total, &cp, func(string, bool) (string, error) { return "overwrite", nil }, func(p filefs.Progress) {
		if p.Done > 65536 {
			stop()
		}
	}, func(string) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &fileJob{page: page, caller: context.Background(), ctx: ctx, cancel: cancel, items: items, total: total, checkpoint: cp, plannedCount: len(items), nonce: "task-resume", view: FileTask{ID: "resume", State: "paused", CanResume: true}, request: TransferRequest{Source: FileLocation{Side: "local", Token: "owned", Path: "/source"}, Destination: FileLocation{Side: "local", Token: "owned", Path: "/target"}, Names: []string{"large"}, Policy: "ask"}}
	s.jobs["resume"] = job
	s.plannedItems = len(items)
	s.mu.Lock()
	err = s.controlLocked(job, "resume")
	claimed := s.claimLocked()
	s.mu.Unlock()
	if err != nil || len(claimed) != 1 {
		t.Fatal(err)
	}
	s.runTask(job)
	actual, err := os.ReadFile(filepath.Join(dir, "target", "large"))
	if err != nil || !bytes.Equal(actual, data) || job.view.State != "completed" || job.view.Done != total {
		t.Fatal(job.view, err)
	}
	if len(job.items) != 0 || s.plannedItems != 0 {
		t.Fatal("completed task retained plan memory")
	}
}

func TestTransferSchedulerBoundsAndPausedSlots(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	page, other := &callerScope{}, &callerScope{}
	for i := 0; i < 12; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		id := string(rune('a' + i))
		s.jobs[id] = &fileJob{page: page, ctx: ctx, cancel: cancel, sequence: uint64(i), view: FileTask{ID: id, State: "queued"}}
	}
	first := s.claimLocked()
	if len(first) != 2 {
		t.Fatal("default concurrency", len(first))
	}
	if err := s.controlLocked(first[0], "pause"); err != nil {
		t.Fatal(err)
	}
	if first[0].ctx.Err() == nil || first[0].view.State != "pausing" {
		t.Fatal("pause did not cancel independent IO")
	}
	if len(s.claimLocked()) != 0 {
		t.Fatal("released slot before IO closed")
	}
	first[0].executing = false
	first[0].view.State = "paused"
	first[0].intent = ""
	if len(s.claimLocked()) != 1 {
		t.Fatal("pause did not release slot")
	}
	s.limits[page] = 1
	if len(s.claimLocked()) != 0 {
		t.Fatal("lowered limit aborted existing or started extra work")
	}
	s.limits[page] = 8
	if len(s.claimLocked()) != 6 {
		t.Fatal("raising limit did not fill slots")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.jobs["other"] = &fileJob{page: other, ctx: ctx, cancel: cancel, view: FileTask{ID: "other", State: "queued"}}
	if len(s.claimLocked()) != 0 {
		t.Fatal("global eight-stream cap exceeded")
	}
	for _, job := range s.jobs {
		if job.executing {
			job.executing = false
			job.view.State = "conflict"
		}
	}
	if len(s.claimLocked()) != 4 {
		t.Fatal("conflicts held slots or page limits failed")
	}
}
func TestTransferControlsRequireOwnedPage(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	ctx := context.Background()
	if s.ConfigureTransfers(ctx, 2) == nil || s.ControlMany(ctx, []string{}, "pause") == nil || s.ClearCompleted(ctx) == nil || s.Control(ctx, "x", "resume") == nil {
		t.Fatal("unowned caller accepted")
	}
	if _, err := s.Tasks(ctx); err == nil {
		t.Fatal("unowned task snapshot accepted")
	}
}

type funcEventSink[T any] func(T) error

func (f funcEventSink[T]) Send(value T) error { return f(value) }

func TestFileServiceReleasePageAndWatchThrottle(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	page := &callerScope{}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.roots[page] = &localFolder{root: root, token: "grant"}
	s.limits[page] = 4
	s.drops["d"] = nativeDrop{page: page, paths: []string{dir}}
	s.comparisons["c"] = ownedComparison{page: page}
	s.syncIntents["i"] = ownedSync{page: page}
	s.mu.Unlock()

	watchCtx, cancelWatch := context.WithCancel(context.WithValue(context.Background(), callerScopeKey{}, page))
	var snapshots atomic.Int32
	var lastMu sync.Mutex
	var lastSnapshot FileEvent
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- s.watchStream(watchCtx, funcEventSink[FileEvent](func(list FileEvent) error {
			snapshots.Add(1)
			lastMu.Lock()
			lastSnapshot = list
			lastMu.Unlock()
			return nil
		}))
	}()
	deadline := time.Now().Add(time.Second)
	for snapshots.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if snapshots.Load() == 0 {
		cancelWatch()
		t.Fatal("watchStream did not send initial snapshot")
	}
	for i := 1; i <= 20; i++ {
		s.mu.Lock()
		s.jobs["burst"] = &fileJob{page: page, view: FileTask{ID: "burst", Done: int64(i), State: "running"}}
		s.notifyLocked(page)
		s.mu.Unlock()
	}
	time.Sleep(180 * time.Millisecond)
	cancelWatch()
	if err := <-watchDone; err != nil {
		t.Fatal(err)
	}
	if count := snapshots.Load(); count > 5 {
		t.Fatalf("expected watchStream burst to coalesce (<=5 snapshots), got %d", count)
	}
	lastMu.Lock()
	gotTasks := lastSnapshot.Tasks
	lastMu.Unlock()
	if len(gotTasks) != 1 || gotTasks[0].Done != 20 {
		t.Fatalf("expected final coalesced snapshot with Done=20, got %+v", gotTasks)
	}

	s.releasePage(page)
	s.mu.Lock()
	leaked := len(s.roots) + len(s.limits) + len(s.drops) + len(s.comparisons) + len(s.syncIntents) + len(s.listeners)
	s.mu.Unlock()
	if leaked != 0 {
		t.Fatalf("releasePage left %d page-scoped entries behind", leaked)
	}
}

func TestStreamVNCConnCoalescesFragmentsAndFlushesBulk(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	var packets []DesktopPacket
	var assembled []byte
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var expected []byte
	go func() {
		frag := bytes.Repeat([]byte("A"), 32)
		for i := 0; i < 16; i++ {
			_, _ = server.Write(frag)
		}
		bulk := bytes.Repeat([]byte("BCDE"), 24<<10)
		_, _ = server.Write(bulk)
		_ = server.Close()
	}()
	expected = append(expected, bytes.Repeat([]byte("A"), 16*32)...)
	expected = append(expected, bytes.Repeat([]byte("BCDE"), 24<<10)...)

	err := streamVNCConn(ctx, "vnc-1", client, funcEventSink[DesktopPacket](func(packet DesktopPacket) error {
		packets = append(packets, packet)
		raw, decErr := base64.StdEncoding.DecodeString(packet.Data)
		if decErr != nil {
			return decErr
		}
		assembled = append(assembled, raw...)
		return nil
	}))
	if err != nil {
		t.Fatalf("streamVNCConn returned error on clean EOF: %v", err)
	}
	if !bytes.Equal(assembled, expected) {
		t.Fatalf("VNC coalesced stream byte mismatch: got %d want %d", len(assembled), len(expected))
	}
	if len(packets) >= 16 {
		t.Fatalf("expected fragmented VNC writes to coalesce into far fewer packets, got %d", len(packets))
	}
}
