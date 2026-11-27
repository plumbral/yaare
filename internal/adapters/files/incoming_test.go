package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIncomingFileSequentialCommitCancelAndConflicts(t *testing.T) {
	f, dir := sandbox(t)
	for _, name := range []string{"../escape", "/absolute", "nested/file", "nul\x00"} {
		if _, e := NewIncomingFile(f, "/", name, 4, "test"); e == nil {
			t.Fatal("peer path accepted", name)
		}
	}
	incoming, e := NewIncomingFile(f, "/", "new.bin", 4, "new")
	if e != nil {
		t.Fatal(e)
	}
	if e = incoming.Begin("ask"); e != nil {
		t.Fatal(e)
	}
	if e = incoming.Write(1, []byte("bad")); e == nil {
		t.Fatal("out-of-order data accepted")
	}
	if e = incoming.Write(0, []byte("excess")); e == nil {
		t.Fatal("oversized stream accepted")
	}
	if e = incoming.Write(0, []byte{0, 128, 254, 255}); e != nil {
		t.Fatal(e)
	}
	if e = incoming.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = incoming.Cancel(); e != nil {
		t.Fatal(e)
	}
	bytes, _ := os.ReadFile(filepath.Join(dir, "new.bin"))
	if len(bytes) != 4 || bytes[3] != 255 {
		t.Fatal("bytes changed")
	}
	if incoming.Write(4, nil) == nil {
		t.Fatal("write after commit")
	}
	os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("original"), 0600)
	pending, e := NewIncomingFile(f, "/", "existing.txt", 4, "conflict")
	if e != nil {
		t.Fatal(e)
	}
	if e = pending.Begin("ask"); !errors.Is(e, ErrConflict) {
		t.Fatal("overwrite not confirmed", e)
	}
	if e = pending.Begin("overwrite"); e != nil {
		t.Fatal(e)
	}
	pending.Write(0, []byte("next"))
	os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("external modification"), 0600)
	if e = pending.Commit(); e == nil {
		t.Fatal("concurrent target overwritten")
	}
	pending.Cancel()
	bytes, _ = os.ReadFile(filepath.Join(dir, "existing.txt"))
	if string(bytes) != "external modification" {
		t.Fatal("external data lost")
	}
	keep, e := NewIncomingFile(f, "/", "existing.txt", 0, "keep")
	if e != nil {
		t.Fatal(e)
	}
	if e = keep.Begin("keepBoth"); e != nil {
		t.Fatal(e)
	}
	if keep.Info().Name != "existing (2).txt" {
		t.Fatal(keep.Info())
	}
	if e = keep.Commit(); e != nil {
		t.Fatal(e)
	}
	if info, e := os.Stat(filepath.Join(dir, "existing (2).txt")); e != nil || info.Size() != 0 {
		t.Fatal("empty file", e)
	}
	cancel, e := NewIncomingFile(f, "/", "cancelled", 20, "cancel")
	if e != nil {
		t.Fatal(e)
	}
	cancel.Begin("ask")
	cancel.Write(0, []byte("partial"))
	if cancel.Commit() == nil {
		t.Fatal("incomplete file committed")
	}
	if e = cancel.Cancel(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(dir, "cancelled")); !os.IsNotExist(e) {
		t.Fatal("cancel created a target")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".yaare-zmodem-*.part"))
	if len(matches) != 0 {
		t.Fatal("stages not cleaned", matches)
	}
}
