package files

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestDroppedSelectionConfinementAndCopy(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "folder")
	os.Mkdir(dir, 0700)
	os.Mkdir(filepath.Join(dir, "empty"), 0700)
	os.WriteFile(filepath.Join(dir, "中文.txt"), []byte("拖入🦂"), 0600)
	os.WriteFile(filepath.Join(base, "secret.txt"), []byte("not selected"), 0600)
	d, e := OpenDropped([]string{dir, filepath.Join(dir, "中文.txt"), dir})
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if len(d.Names()) != 1 || d.Names()[0] != "folder" {
		t.Fatal("duplicate descendants", d.Names())
	}
	for _, p := range []string{"/secret.txt", "/folder/../secret.txt", "/folder\\..\\secret.txt"} {
		if _, e = d.Open(p); e == nil {
			t.Fatal("escaped selection")
		}
	}
	if _, e = d.Create("/folder/new"); !os.IsPermission(e) {
		t.Fatal("writable grant")
	}
	items, total, e := Plan(context.Background(), d, "/", d.Names())
	if e != nil || total != 10 || len(items) != 3 {
		t.Fatal("plan", total, len(items), e)
	}
	dest, _ := sandbox(t)
	e = CopyResumable(context.Background(), d, dest, items, "/", "drop", total, &CopyCheckpoint{}, func(string, bool) (string, error) { return "skip", nil }, func(Progress) {}, func(string) {})
	if e != nil {
		t.Fatal(e)
	}
	r, e := dest.Open("/folder/中文.txt")
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	if string(b) != "拖入🦂" {
		t.Fatal("bytes")
	}
}
func TestDroppedFilesRejectDuplicatesLinksAndPrivateSiblings(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	pa, pb := filepath.Join(a, "same.txt"), filepath.Join(b, "same.txt")
	os.WriteFile(pa, []byte("a"), 0600)
	os.WriteFile(pb, []byte("b"), 0600)
	if d, e := OpenDropped([]string{pa, pb}); e == nil {
		d.Close()
		t.Fatal("ambiguous destination")
	}
	d, e := OpenDropped([]string{pa})
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if _, e = d.Lstat("/other.txt"); !os.IsPermission(e) {
		t.Fatal("sibling access")
	}
	link := filepath.Join(a, "link")
	if os.Symlink(pb, link) == nil {
		if d, e := OpenDropped([]string{link}); e == nil {
			d.Close()
			t.Fatal("link accepted")
		}
	}
	if _, e = OpenDropped([]string{"relative.txt"}); e == nil {
		t.Fatal("relative native path")
	}
	if _, e = d.Open("/same.txt/nope"); e != fs.ErrPermission {
		t.Fatal("file descendant accepted", e)
	}
}
