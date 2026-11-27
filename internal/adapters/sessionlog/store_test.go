package sessionlog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTranscriptSplitUTF8AndControlStrings(t *testing.T) {
	input := []byte("\x1b[31m中文🦂\x1b[0m\n\x1b]52;c;hidden-clipboard\x07old\rnew\nabc\bX\n\x1b]7;hidden-directory\x1b\\done")
	for split := 0; split <= len(input); split++ {
		var plain plainText
		var text strings.Builder
		plain.feed(input[:split], func(s string) { text.WriteString(s) })
		plain.feed(input[split:], func(s string) { text.WriteString(s) })
		plain.flush(func(s string) { text.WriteString(s) })
		if text.String() != "中文🦂\nnew\nabX\ndone" {
			t.Fatalf("split %d: %q", split, text.String())
		}
	}
}
func fresh(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s := New(func() (string, error) { return dir, nil })
	if _, err := os.Stat(filepath.Join(dir, "session-logs")); !os.IsNotExist(err) {
		t.Fatal("constructor accessed disk")
	}
	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, dir
}
func stop(t *testing.T, r *Recording) Info {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := r.Stop(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	return info
}
func TestRecordingIsolationPaginationSearchExportAndRecovery(t *testing.T) {
	s, dir := fresh(t)
	a, err := s.Start("pane-a", 1, "主机 A", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Start("pane-b", 1, "主机 B", nil)
	if err != nil {
		t.Fatal(err)
	}
	var input strings.Builder
	for i := 0; i < 220; i++ {
		input.WriteString("remote 中文\n")
	}
	a.Capture([]byte(input.String()))
	b.Capture([]byte("other\n"))
	info := stop(t, a)
	stop(t, b)
	page, err := s.Read(context.Background(), Query{ID: info.ID})
	if err != nil || len(page.Lines) != 200 || !page.More {
		t.Fatal("page", len(page.Lines), err)
	}
	last, err := s.Read(context.Background(), Query{ID: info.ID, Cursor: page.Next, Text: "REMOTE"})
	if err != nil || len(last.Lines) != 20 || last.More {
		t.Fatal("next", len(last.Lines), err)
	}
	none, err := s.Read(context.Background(), Query{ID: info.ID, After: time.Now().Add(time.Hour).UnixMilli()})
	if err != nil || len(none.Lines) != 0 {
		t.Fatal("time filter", err)
	}
	var out bytes.Buffer
	if err = s.Export(context.Background(), info.ID, &out); err != nil || strings.Contains(out.String(), "other") || strings.Count(out.String(), "remote 中文") != 220 {
		t.Fatal("export", err)
	}
	restarted := New(func() (string, error) { return dir, nil })
	if err = restarted.Initialize(); err != nil {
		t.Fatal(err)
	}
	list, err := restarted.List()
	if err != nil || len(list) != 2 || list[0].State == "recording" {
		t.Fatal("recovery", err)
	}
	if _, err = s.Read(context.Background(), Query{ID: "../escape"}); err == nil {
		t.Fatal("accepted arbitrary path")
	}
	if err = s.Delete(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(context.Background(), Query{ID: info.ID}); err == nil {
		t.Fatal("deleted log readable")
	}
}
func TestRecordingBoundedFailureDoesNotBlockCapture(t *testing.T) {
	s, _ := fresh(t)
	r, err := s.Start("pane", 1, "bounded", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.max = 64
	r.Capture([]byte(strings.Repeat("x", 200) + "\n"))
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("size limit did not stop")
	}
	if r.Info().State != "limited" || r.Info().Bytes > 64 {
		t.Fatal(r.Info())
	}
	r, err = s.Start("pane", 2, "disk-error", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.file.Close(); err != nil {
		t.Fatal(err)
	}
	r.Capture([]byte("remote\n"))
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("disk failure blocked")
	}
	if r.Info().State != "limited" {
		t.Fatal(r.Info())
	}
	r.Capture([]byte("still safe"))
	stalled := &Recording{info: Info{State: "recording"}, queue: make(chan packet, 1), stop: make(chan struct{})}
	start := time.Now()
	for i := 0; i < 100; i++ {
		stalled.Capture(make([]byte, 32768))
	}
	if time.Since(start) > time.Second {
		t.Fatal("capture blocked")
	}
	select {
	case <-stalled.stop:
	default:
		t.Fatal("queue overflow did not stop")
	}
}
func TestActiveRecordingCannotBeDeletedOrExported(t *testing.T) {
	s, _ := fresh(t)
	r, err := s.Start("pane", 1, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Delete(r.Info().ID) == nil {
		t.Fatal("deleted active log")
	}
	if s.Export(context.Background(), r.Info().ID, &bytes.Buffer{}) == nil {
		t.Fatal("exported active log")
	}
	stop(t, r)
}

func TestLazyLogIndexRetriesAndStopsAtClose(t *testing.T) {
	calls := 0
	root := t.TempDir()
	s := New(func() (string, error) {
		calls++
		if calls == 1 {
			return "", os.ErrNotExist
		}
		return root, nil
	})
	if calls != 0 {
		t.Fatal("constructor touched the filesystem")
	}
	if _, err := s.List(); err == nil {
		t.Fatal("first access hid initialization failure")
	}
	if logs, err := s.List(); err != nil || len(logs) != 0 || calls != 2 {
		t.Fatal("retry did not load index", err)
	}
	if _, err := s.List(); err != nil || calls != 2 {
		t.Fatal("ready index was reloaded", err)
	}
	s.Close()
	if _, err := s.Start("session", 1, "test", nil); err == nil {
		t.Fatal("recording started after shutdown")
	}
}
