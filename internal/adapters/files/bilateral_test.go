package files

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBilateralBaselineChangesDeletesAndConflicts(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	ctx := context.Background()
	put := func(root, name, content string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	scan := func() Comparison {
		t.Helper()
		c, e := CompareTrees(ctx, left, right, "/", "/", true, true, func(int, int64) {})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	for _, name := range []string{"left.txt", "right.txt", "both.txt", "delete.txt", "delete-change.txt"} {
		put(lp, name, "original")
		put(rp, name, "original")
	}
	base := SyncBaseline{}
	AcceptSyncBaseline(base, scan())
	put(lp, "left.txt", "from local")
	put(rp, "right.txt", "from remote")
	put(lp, "both.txt", "local")
	put(rp, "both.txt", "remote")
	os.Remove(filepath.Join(lp, "delete.txt"))
	os.Remove(filepath.Join(lp, "delete-change.txt"))
	put(rp, "delete-change.txt", "changed")
	put(lp, "new-local.txt", "new local")
	put(rp, "new-remote.txt", "new remote")
	c := scan()
	actions, e := PlanBilateral(c, base, true)
	if e != nil {
		t.Fatal(e)
	}
	kinds := map[string]string{}
	for _, a := range actions {
		kinds[a.Path] = a.Kind
	}
	for name, want := range map[string]string{"left.txt": "upload", "right.txt": "download", "both.txt": "conflict", "delete.txt": "delete-remote", "delete-change.txt": "conflict", "new-local.txt": "upload", "new-remote.txt": "download"} {
		if kinds[name] != want {
			t.Fatalf("%s got %s want %s", name, kinds[name], want)
		}
	}
	disabled, _ := PlanBilateral(c, base, false)
	for _, a := range disabled {
		if a.Path == "delete.txt" && a.Kind != "retained" {
			t.Fatal("deletion occurred without opt-in")
		}
	}
	for _, direction := range []string{"upload", "download"} {
		p := BilateralCopies(actions, "/", "/", direction)
		source, dest := FS(left), FS(right)
		if direction == "download" {
			source, dest = dest, source
		}
		if e = p.Check(ctx, source, dest); e != nil {
			t.Fatal(e)
		}
		source, dest = p.Wrap(ctx, source, dest)
		if e = Copy(ctx, source, dest, p.Items, p.DestinationRoot, "bilateral-"+direction, p.Total, func(string, bool) (string, error) { return "overwrite", nil }, func(Progress) {}); e != nil {
			t.Fatal(e)
		}
	}
	if e = ApplyBilateralDeletes(ctx, left, right, "/", "/", actions); e != nil {
		t.Fatal(e)
	}
	AcceptSyncBaseline(base, scan())
	next, e := PlanBilateral(scan(), base, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(next) != 2 {
		t.Fatalf("successful paths were replayed: %#v", next)
	}
	for _, a := range next {
		if a.Kind != "conflict" {
			t.Fatal(a)
		}
	}
}
func TestBilateralDeleteChecksChangedTargetAndNewDirectoryContent(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	ctx := context.Background()
	base := SyncBaseline{}
	for _, root := range []string{lp, rp} {
		os.Mkdir(filepath.Join(root, "dir"), 0700)
		os.WriteFile(filepath.Join(root, "dir", "a"), []byte("same"), 0600)
	}
	scan := func() Comparison {
		c, e := CompareTrees(ctx, left, right, "/", "/", true, true, func(int, int64) {})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	AcceptSyncBaseline(base, scan())
	os.RemoveAll(filepath.Join(lp, "dir"))
	actions, _ := PlanBilateral(scan(), base, true)
	os.WriteFile(filepath.Join(rp, "dir", "a"), []byte("race"), 0600)
	if e := ApplyBilateralDeletes(ctx, left, right, "/", "/", actions); e == nil {
		t.Fatal("changed target removed")
	}
	os.WriteFile(filepath.Join(rp, "dir", "a"), []byte("same"), 0600)
	actions, _ = PlanBilateral(scan(), base, true)
	os.WriteFile(filepath.Join(rp, "dir", "new"), []byte("new"), 0600)
	if e := ApplyBilateralDeletes(ctx, left, right, "/", "/", actions); e == nil {
		t.Fatal("directory with new content removed")
	}
	if _, e := os.Stat(filepath.Join(rp, "dir", "new")); e != nil {
		t.Fatal(e)
	}
}

type countingSyncFS struct {
	FS
	opens int
}

func (f *countingSyncFS) Open(p string) (io.ReadCloser, error) { f.opens++; return f.FS.Open(p) }
func TestAutomaticScanReusesUnchangedHashesAndCanForceRefresh(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	for _, root := range []string{lp, rp} {
		os.WriteFile(filepath.Join(root, "a"), []byte("same"), 0600)
	}
	l, r := &countingSyncFS{FS: left}, &countingSyncFS{FS: right}
	cache := SyncScanCache{}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, e := CompareTreesCached(ctx, l, r, "/", "/", true, &cache); e != nil {
			t.Fatal(e)
		}
	}
	if l.opens != 1 || r.opens != 1 {
		t.Fatal("unchanged files reread", l.opens, r.opens)
	}
	cache = SyncScanCache{}
	CompareTreesCached(ctx, l, r, "/", "/", true, &cache)
	if l.opens != 2 || r.opens != 2 {
		t.Fatal("forced verification skipped")
	}
}

type afterSyncRename struct {
	FS
	after func()
}

func (f afterSyncRename) Rename(from, to string, replace bool) error {
	e := f.FS.Rename(from, to, replace)
	if e == nil {
		f.after()
	}
	return e
}
func TestBilateralMergeIdenticalBytesAndConcurrentChange(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	ctx := context.Background()
	write := func() {
		os.WriteFile(filepath.Join(lp, "a"), append([]byte{0xef, 0xbb, 0xbf}, []byte("local\r\n")...), 0600)
		os.WriteFile(filepath.Join(rp, "a"), []byte("remote\n"), 0600)
	}
	read := func() (TextFile, TextFile) {
		l, e := ReadText(ctx, left, "/a")
		if e != nil {
			t.Fatal(e)
		}
		r, e := ReadText(ctx, right, "/a")
		if e != nil {
			t.Fatal(e)
		}
		return l, r
	}
	write()
	l, r := read()
	if e := MergeBilateralText(ctx, left, right, "/a", "/a", l.Version, r.Version, "combined\n", "merge"); e != nil {
		t.Fatal(e)
	}
	for _, root := range []string{lp, rp} {
		b, _ := os.ReadFile(filepath.Join(root, "a"))
		if string(b) != "combined\n" {
			t.Fatalf("merge formats differ: %q", b)
		}
	}
	write()
	l, r = read()
	os.WriteFile(filepath.Join(rp, "a"), []byte("external"), 0600)
	if e := MergeBilateralText(ctx, left, right, "/a", "/a", l.Version, r.Version, "merge\n", "stale"); e == nil {
		t.Fatal("stale merge saved")
	}
	b, _ := os.ReadFile(filepath.Join(lp, "a"))
	if !strings.Contains(string(b), "local") {
		t.Fatal("first side saved before stale second-side check")
	}
	write()
	l, r = read()
	hook := afterSyncRename{left, func() { os.WriteFile(filepath.Join(rp, "a"), []byte("raced"), 0600) }}
	e := MergeBilateralText(ctx, hook, right, "/a", "/a", l.Version, r.Version, "merge\n", "partial")
	if e == nil || !strings.Contains(e.Error(), "本地已保存") {
		t.Fatal("partial save misreported", e)
	}
	b, _ = os.ReadFile(filepath.Join(rp, "a"))
	if string(b) != "raced" {
		t.Fatal("raced second side overwritten")
	}
}
