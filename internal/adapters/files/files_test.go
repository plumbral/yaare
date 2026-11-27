package files

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func sandbox(t *testing.T) (Local, string) {
	t.Helper()
	p := t.TempDir()
	root, e := os.OpenRoot(p)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { root.Close() })
	return Local{root}, p
}
func TestCopyConflictsAndCancellationPreserveExistingFiles(t *testing.T) {
	source, src := sandbox(t)
	destination, dst := sandbox(t)
	os.WriteFile(filepath.Join(src, "sample.txt"), []byte("new bytes"), 0600)
	os.WriteFile(filepath.Join(dst, "sample.txt"), []byte("original"), 0600)
	items, total, e := Plan(context.Background(), source, "/", []string{"sample.txt"})
	if e != nil {
		t.Fatal(e)
	}
	for _, choice := range []string{"skip", "keepBoth"} {
		e = Copy(context.Background(), source, destination, items, "/", "fixture", total, func(string, bool) (string, error) { return choice, nil }, func(Progress) {})
		if e != nil {
			t.Fatal(e)
		}
		b, _ := os.ReadFile(filepath.Join(dst, "sample.txt"))
		if string(b) != "original" {
			t.Fatal("original overwritten without permission")
		}
	}
	b, e := os.ReadFile(filepath.Join(dst, "sample (2).txt"))
	if e != nil || string(b) != "new bytes" {
		t.Fatal("keepBoth did not preserve bytes", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e = Copy(ctx, source, destination, items, "/", "cancel", total, func(string, bool) (string, error) { return "overwrite", nil }, func(Progress) { cancel() })
	if !errors.Is(e, context.Canceled) {
		t.Fatal("cancel was not returned", e)
	}
	b, _ = os.ReadFile(filepath.Join(dst, "sample.txt"))
	if string(b) != "original" {
		t.Fatal("cancel replaced original")
	}
	matches, _ := filepath.Glob(filepath.Join(dst, "*.part"))
	if len(matches) != 0 {
		t.Fatal("temporary file leaked")
	}
}
func TestLocalConfinementAndDirectoryCopy(t *testing.T) {
	source, src := sandbox(t)
	destination, dst := sandbox(t)
	os.Mkdir(filepath.Join(src, "folder"), 0700)
	os.WriteFile(filepath.Join(src, "folder", "中文.txt"), []byte("content"), 0600)
	items, total, e := Plan(context.Background(), source, "/", []string{"folder"})
	if e != nil {
		t.Fatal(e)
	}
	if e = Copy(context.Background(), source, destination, items, "/", "tree", total, func(string, bool) (string, error) { return "overwrite", nil }, func(Progress) {}); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "folder", "中文.txt"))
	if string(b) != "content" {
		t.Fatal("directory data changed")
	}
	if _, e = source.Root.Open("../outside"); e == nil {
		t.Fatal("root traversal accepted")
	}
	for _, name := range []string{"../outside", "a/b", "a\\b", ".", "..", ""} {
		if Leaf(name) {
			t.Fatal("unsafe leaf accepted", name)
		}
	}
	if _, _, e = Plan(context.Background(), source, "/", []string{"folder", "folder"}); e == nil {
		t.Fatal("duplicate selection accepted")
	}
}

type descendingDirectoryFS struct{ FS }

func (f descendingDirectoryFS) ReadDir(path string) ([]fs.FileInfo, error) {
	files, err := f.FS.ReadDir(path)
	slices.SortFunc(files, func(left, right fs.FileInfo) int { return strings.Compare(right.Name(), left.Name()) })
	return files, err
}

func TestPlanSortsDirectoryChildrenIndependentlyOfFilesystem(t *testing.T) {
	source, folder := sandbox(t)
	if err := os.Mkdir(filepath.Join(folder, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"z.txt", "a.txt", "中.txt", "m.txt"} {
		if err := os.WriteFile(filepath.Join(folder, "folder", name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := Plan(context.Background(), descendingDirectoryFS{source}, "/", []string{"folder"})
	if err != nil {
		t.Fatal(err)
	}
	var ordered []string
	for _, item := range items {
		ordered = append(ordered, item.Relative)
	}
	if !slices.Equal(ordered, []string{"folder", "folder/a.txt", "folder/m.txt", "folder/z.txt", "folder/中.txt"}) {
		t.Fatal("unstable directory plan", ordered)
	}
}

type brokenFS struct{ FS }
type brokenWriter struct{ io.WriteCloser }

func (w brokenWriter) Write(p []byte) (int, error) { return 0, io.ErrShortWrite }
func (f brokenFS) Create(p string) (io.WriteCloser, error) {
	w, e := f.FS.Create(p)
	if e != nil {
		return nil, e
	}
	return brokenWriter{w}, nil
}
func TestWriteFailureDoesNotReportSuccessOrOverwrite(t *testing.T) {
	source, src := sandbox(t)
	dest, dst := sandbox(t)
	os.WriteFile(filepath.Join(src, "data"), []byte(strings.Repeat("x", 65536)), 0600)
	os.WriteFile(filepath.Join(dst, "data"), []byte("old"), 0600)
	items, total, e := Plan(context.Background(), source, "/", []string{"data"})
	if e != nil {
		t.Fatal(e)
	}
	e = Copy(context.Background(), source, brokenFS{dest}, items, "/", "failure", total, func(string, bool) (string, error) { return "overwrite", nil }, func(Progress) {})
	if e == nil {
		t.Fatal("write failure reported as success")
	}
	b, _ := os.ReadFile(filepath.Join(dst, "data"))
	if string(b) != "old" {
		t.Fatal("failed write destroyed old file")
	}
}

type lstatCountingFS struct {
	FS
	lstats int
}

func (f *lstatCountingFS) Lstat(p string) (fs.FileInfo, error) {
	f.lstats++
	return f.FS.Lstat(p)
}

func TestPlanWithProgressPulsesAndAvoidsChildLstat(t *testing.T) {
	source, src := sandbox(t)
	if err := os.MkdirAll(filepath.Join(src, "root", "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		name := string(rune('a'+i)) + ".txt"
		if err := os.WriteFile(filepath.Join(src, "root", name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "root", "a", "b", name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	counted := &lstatCountingFS{FS: source}
	pulses := 0
	items, total, err := PlanWithProgress(context.Background(), counted, "/", []string{"root"}, func() {
		pulses++
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 23 || total == 0 {
		t.Fatalf("unexpected plan count=%d total=%d", len(items), total)
	}
	if pulses < 20 {
		t.Fatalf("expected heartbeat pulses during PlanWithProgress, got %d", pulses)
	}
	if counted.lstats != 1 {
		t.Fatalf("expected only 1 root Lstat (reusing ReadDir FileInfo for children), got %d", counted.lstats)
	}
}
