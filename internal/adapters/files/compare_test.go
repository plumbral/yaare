package files

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func TestCompareContentHashesShallowRecursiveAndNames(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	os.Mkdir(filepath.Join(lp, "sub"), 0700)
	os.Mkdir(filepath.Join(rp, "sub"), 0700)
	for _, root := range []string{lp, rp} {
		os.WriteFile(filepath.Join(root, "same.txt"), []byte("same"), 0600)
		os.WriteFile(filepath.Join(root, "sub", "nested.txt"), []byte("nested"), 0600)
	}
	os.WriteFile(filepath.Join(lp, "changed.txt"), []byte("aaaa"), 0600)
	os.WriteFile(filepath.Join(rp, "changed.txt"), []byte("bbbb"), 0600)
	sameTime := time.Unix(1700000000, 0)
	os.Chtimes(filepath.Join(lp, "changed.txt"), sameTime, sameTime)
	os.Chtimes(filepath.Join(rp, "changed.txt"), sameTime, sameTime)
	os.WriteFile(filepath.Join(lp, "only-left.txt"), nil, 0600)
	os.WriteFile(filepath.Join(rp, "only-right.txt"), nil, 0600)
	c, e := CompareTrees(context.Background(), left, right, "/", "/", false, true, func(int, int64) {})
	if e != nil || !c.Complete {
		t.Fatal(e)
	}
	status := map[string]string{}
	for _, row := range c.Entries {
		status[row.Relative] = row.Status
	}
	if status["changed.txt"] != "different" || status["same.txt"] != "same" || status["only-left.txt"] != "left-only" || status["only-right.txt"] != "right-only" || status["sub/nested.txt"] != "" {
		t.Fatal(status)
	}
	c, e = CompareTrees(context.Background(), left, right, "/", "/", true, true, func(int, int64) {})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, row := range c.Entries {
		if row.Relative == "sub/nested.txt" {
			found = row.Status == "same"
		}
	}
	if !found {
		t.Fatal("recursive missing")
	}
	for _, name := range []string{"CON", "sub/aux.txt", "a:b", "trailing.", "trailing ", "Com¹.log", "LPT²", "conout$", "NUL .txt"} {
		if WindowsRelativeName(name) {
			t.Fatal("unsafe windows name", name)
		}
	}
	if !WindowsRelativeName("中文/valid-file.txt") {
		t.Fatal("valid unicode rejected")
	}
}

type comparisonCloseWriter struct {
	io.WriteCloser
	once  sync.Once
	after func()
}

func TestWindowsNameConflictIncludesDescendants(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	for _, file := range []string{
		filepath.Join(lp, "Docs", "sub", "local.txt"),
		filepath.Join(rp, "docs", "sub", "remote.txt"),
		filepath.Join(rp, "safe", "remote.txt"),
	} {
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c, err := CompareTrees(context.Background(), left, right, "/", "/", true, true, func(int, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, entry := range c.Entries {
		status[entry.Relative] = entry.Status
	}
	for _, name := range []string{"Docs", "docs", "Docs/sub", "docs/sub", "Docs/sub/local.txt", "docs/sub/remote.txt"} {
		if status[name] != "name-conflict" {
			t.Errorf("%s: got %q", name, status[name])
		}
	}
	if status["safe/remote.txt"] != "right-only" {
		t.Fatal(status)
	}
	actions, err := PlanBilateral(c, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if status[action.Path] == "name-conflict" && action.Kind != "conflict" {
			t.Fatalf("ambiguous path scheduled for %s: %s", action.Kind, action.Path)
		}
	}
}

func (w *comparisonCloseWriter) Close() error {
	err := w.WriteCloser.Close()
	w.once.Do(w.after)
	return err
}
func TestReviewedCopyRechecksFullTargetHashAfterStaging(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	os.WriteFile(filepath.Join(lp, "target.txt"), []byte("AAAA"), 0600)
	os.WriteFile(filepath.Join(rp, "target.txt"), []byte("BBBB"), 0600)
	c, e := CompareTrees(context.Background(), left, right, "/", "/", false, false, func(int, int64) {})
	if e != nil {
		t.Fatal(e)
	}
	plan, e := BuildReviewedSync(c, "/", "/", "upload", []string{"target.txt"})
	if e != nil {
		t.Fatal(e)
	}
	info, _ := os.Stat(filepath.Join(rp, "target.txt"))
	changed := textFaultFS{FS: right, create: func(p string) (io.WriteCloser, error) {
		out, e := right.Create(p)
		if e != nil {
			return nil, e
		}
		return &comparisonCloseWriter{WriteCloser: out, after: func() {
			os.WriteFile(filepath.Join(rp, "target.txt"), []byte("CCCC"), 0600)
			os.Chtimes(filepath.Join(rp, "target.txt"), info.ModTime(), info.ModTime())
		}}, nil
	}}
	source, dest := plan.Wrap(context.Background(), left, changed)
	e = CopyResumable(context.Background(), source, dest, plan.Items, plan.DestinationRoot, "stage-check", plan.Total, &CopyCheckpoint{}, func(string, bool) (string, error) { return "overwrite", nil }, func(Progress) {}, func(string) {})
	if e == nil {
		t.Fatal("same metadata mutation during staging was overwritten")
	}
	got, _ := os.ReadFile(filepath.Join(rp, "target.txt"))
	if string(got) != "CCCC" {
		t.Fatal("original changed file lost")
	}
}
func TestComparisonNameConflictsLargeFilesAndCancellation(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	os.WriteFile(filepath.Join(lp, "Case.txt"), nil, 0600)
	os.WriteFile(filepath.Join(rp, "case.txt"), nil, 0600)
	c, e := CompareTrees(context.Background(), left, right, "/", "/", false, true, func(int, int64) {})
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range c.Entries {
		if row.Status != "name-conflict" {
			t.Fatal("case ambiguity", row.Status)
		}
	}
	os.WriteFile(filepath.Join(lp, "large.bin"), nil, 0600)
	os.WriteFile(filepath.Join(rp, "large.bin"), nil, 0600)
	os.Truncate(filepath.Join(lp, "large.bin"), MaxCompareFileBytes+1)
	os.Truncate(filepath.Join(rp, "large.bin"), MaxCompareFileBytes+1)
	c, e = CompareTrees(context.Background(), left, right, "/large.bin", "/large.bin", false, false, func(int, int64) {})
	if e != nil || c.Entries[0].Status != "unverified" {
		t.Fatal("large file falsely equal", e)
	}
	if _, e = BuildReviewedSync(c, "/large.bin", "/large.bin", "upload", []string{"."}); e == nil {
		t.Fatal("unverified copied")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = CompareTrees(ctx, left, right, "/", "/", true, false, func(int, int64) {}); e != context.Canceled {
		t.Fatal("cancel ignored", e)
	}
}
func TestReviewedCopyCreatesDirectoriesAndBlocksChangesBeforeCommit(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "copy", true: "stale-destination"}[changed], func(t *testing.T) {
			left, lp := sandbox(t)
			right, rp := sandbox(t)
			os.Mkdir(filepath.Join(lp, "new"), 0700)
			os.WriteFile(filepath.Join(lp, "new", "中文.txt"), []byte("source"), 0600)
			os.WriteFile(filepath.Join(lp, "target.txt"), []byte("AAAA"), 0600)
			os.WriteFile(filepath.Join(rp, "target.txt"), []byte("BBBB"), 0600)
			c, e := CompareTrees(context.Background(), left, right, "/", "/", true, false, func(int, int64) {})
			if e != nil {
				t.Fatal(e)
			}
			plan, e := BuildReviewedSync(c, "/", "/", "upload", []string{"new", "target.txt"})
			if e != nil {
				t.Fatal(e)
			}
			if e = plan.Check(context.Background(), left, right); e != nil {
				t.Fatal("preflight", e)
			}
			if changed {
				info, _ := os.Stat(filepath.Join(rp, "target.txt"))
				os.WriteFile(filepath.Join(rp, "target.txt"), []byte("CCCC"), 0600)
				os.Chtimes(filepath.Join(rp, "target.txt"), info.ModTime(), info.ModTime())
			}
			source, dest := plan.Wrap(context.Background(), left, right)
			cp := &CopyCheckpoint{}
			e = CopyResumable(context.Background(), source, dest, plan.Items, plan.DestinationRoot, "reviewed", plan.Total, cp, func(string, bool) (string, error) { return "overwrite", nil }, func(Progress) {}, func(string) {})
			if changed {
				if e == nil {
					t.Fatal("stale bytes overwritten")
				}
				got, _ := os.ReadFile(filepath.Join(rp, "target.txt"))
				if !bytes.Equal(got, []byte("CCCC")) {
					t.Fatal("changed destination lost")
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				got, e := os.ReadFile(filepath.Join(rp, "new", "中文.txt"))
				if e != nil || string(got) != "source" {
					t.Fatal("nested copy", e)
				}
			}
		})
	}
}
func TestReviewedSingleFileDifferentBasenamesAndSourceMutation(t *testing.T) {
	left, lp := sandbox(t)
	right, rp := sandbox(t)
	os.WriteFile(filepath.Join(lp, "local.txt"), []byte("local"), 0600)
	os.WriteFile(filepath.Join(rp, "remote.txt"), []byte("remote"), 0600)
	c, e := CompareTrees(context.Background(), left, right, "/local.txt", "/remote.txt", false, false, func(int, int64) {})
	if e != nil {
		t.Fatal(e)
	}
	plan, e := BuildReviewedSync(c, "/local.txt", "/remote.txt", "upload", []string{"."})
	if e != nil {
		t.Fatal(e)
	}
	source, dest := plan.Wrap(context.Background(), left, right)
	if e = CopyResumable(context.Background(), source, dest, plan.Items, plan.DestinationRoot, "single", plan.Total, &CopyCheckpoint{}, func(string, bool) (string, error) { return "overwrite", nil }, func(Progress) {}, func(string) {}); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(filepath.Join(rp, "remote.txt"))
	if string(got) != "local" {
		t.Fatal("wrong destination basename")
	}
	old, _ := os.Stat(filepath.Join(lp, "local.txt"))
	os.WriteFile(filepath.Join(lp, "local.txt"), []byte("other"), 0600)
	os.Chtimes(filepath.Join(lp, "local.txt"), old.ModTime(), old.ModTime())
	if plan.Check(context.Background(), left, right) == nil {
		t.Fatal("same metadata source change ignored")
	}
}

type ownerWrappedInfo struct {
	fs.FileInfo
	uid, gid uint32
}

func (o ownerWrappedInfo) Sys() any {
	return &sftp.FileStat{UID: o.uid, GID: o.gid}
}

type countingCompareFS struct {
	FS
	lstats atomic.Int32
	opens  atomic.Int32
	uid    atomic.Uint32
	gid    atomic.Uint32
	wrap   bool
}

func (f *countingCompareFS) wrapInfo(info fs.FileInfo) fs.FileInfo {
	if !f.wrap || info == nil {
		return info
	}
	return ownerWrappedInfo{FileInfo: info, uid: f.uid.Load(), gid: f.gid.Load()}
}

func (f *countingCompareFS) Lstat(p string) (fs.FileInfo, error) {
	f.lstats.Add(1)
	info, err := f.FS.Lstat(p)
	return f.wrapInfo(info), err
}

func (f *countingCompareFS) ReadDir(p string) ([]fs.FileInfo, error) {
	entries, err := f.FS.ReadDir(p)
	if err != nil || !f.wrap {
		return entries, err
	}
	wrapped := make([]fs.FileInfo, len(entries))
	for i, info := range entries {
		wrapped[i] = f.wrapInfo(info)
	}
	return wrapped, nil
}

func (f *countingCompareFS) Open(p string) (io.ReadCloser, error) {
	f.opens.Add(1)
	return f.FS.Open(p)
}

func TestCompareReusesReadDirInfoChecksOwnerCacheAndReviewedSingleRead(t *testing.T) {
	leftRaw, lp := sandbox(t)
	rightRaw, rp := sandbox(t)
	os.MkdirAll(filepath.Join(lp, "sub", "d1", "d2"), 0700)
	os.MkdirAll(filepath.Join(rp, "sub", "d1", "d2"), 0700)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(lp, "sub", name), []byte("same-"+name), 0600)
		os.WriteFile(filepath.Join(rp, "sub", name), []byte("same-"+name), 0600)
	}
	os.WriteFile(filepath.Join(lp, "only-left.txt"), []byte("new-payload"), 0600)
	os.WriteFile(filepath.Join(lp, "changed.txt"), []byte("AAAA"), 0600)
	os.WriteFile(filepath.Join(rp, "changed.txt"), []byte("BBBB"), 0600)

	left := &countingCompareFS{FS: leftRaw, wrap: true}
	right := &countingCompareFS{FS: rightRaw, wrap: true}
	left.uid.Store(1000)
	left.gid.Store(1000)
	right.uid.Store(1000)
	right.gid.Store(1000)

	c, err := CompareTrees(context.Background(), left, right, "/", "/", true, false, func(int, int64) {})
	if err != nil || !c.Complete {
		t.Fatal(err)
	}
	if left.lstats.Load() != 6 || right.lstats.Load() != 5 {
		t.Fatalf("expected 1 root + 1 post-read Lstat per file (left=6 right=5, 0 dir/pre-read Lstats), got left=%d right=%d", left.lstats.Load(), right.lstats.Load())
	}

	var cache SyncScanCache
	left.opens.Store(0)
	if _, err := CompareTreesCached(context.Background(), left, right, "/", "/", false, &cache); err != nil {
		t.Fatal(err)
	}
	firstOpens := left.opens.Load()
	if firstOpens == 0 {
		t.Fatal("expected initial cached scan to hash files")
	}
	left.opens.Store(0)
	if _, err := CompareTreesCached(context.Background(), left, right, "/", "/", false, &cache); err != nil {
		t.Fatal(err)
	}
	if left.opens.Load() != 0 {
		t.Fatalf("expected cache hit with 0 opens, got %d", left.opens.Load())
	}
	left.uid.Store(1001)
	if _, err := CompareTreesCached(context.Background(), left, right, "/", "/", false, &cache); err != nil {
		t.Fatal(err)
	}
	if left.opens.Load() != firstOpens {
		t.Fatalf("expected UID change to invalidate SyncScanCache (%d opens), got %d", firstOpens, left.opens.Load())
	}

	left.uid.Store(1000)
	c, err = CompareTrees(context.Background(), left, right, "/", "/", true, false, func(int, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildReviewedSync(c, "/", "/", "upload", []string{"only-left.txt", "changed.txt"})
	if err != nil {
		t.Fatal(err)
	}
	left.opens.Store(0)
	right.opens.Store(0)
	if err := plan.CheckMetadata(context.Background(), left, right); err != nil {
		t.Fatal("CheckMetadata:", err)
	}
	if left.opens.Load() != 0 || right.opens.Load() != 0 {
		t.Fatalf("CheckMetadata should perform 0 file opens, got left=%d right=%d", left.opens.Load(), right.opens.Load())
	}
	srcWrapped, dstWrapped := plan.Wrap(context.Background(), left, right)
	if err := CopyResumable(context.Background(), srcWrapped, dstWrapped, plan.Items, plan.DestinationRoot, "single-read", plan.Total, &CopyCheckpoint{}, overwriteCopy, func(Progress) {}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if left.opens.Load() != 2 {
		t.Fatalf("expected exactly 2 source opens (1 per file, no pre-open or post-stream re-read), got %d", left.opens.Load())
	}
	if right.opens.Load() != 1 {
		t.Fatalf("expected exactly 1 destination open (existing changed.txt target re-hash only, no staged re-read), got %d", right.opens.Load())
	}

	os.WriteFile(filepath.Join(lp, "mutate.txt"), []byte("1111"), 0600)
	c2, err := CompareTrees(context.Background(), leftRaw, rightRaw, "/", "/", false, false, func(int, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	plan2, err := BuildReviewedSync(c2, "/", "/", "upload", []string{"mutate.txt"})
	if err != nil {
		t.Fatal(err)
	}
	oldInfo, _ := os.Stat(filepath.Join(lp, "mutate.txt"))
	os.WriteFile(filepath.Join(lp, "mutate.txt"), []byte("2222"), 0600)
	os.Chtimes(filepath.Join(lp, "mutate.txt"), oldInfo.ModTime(), oldInfo.ModTime())
	src2, dst2 := plan2.Wrap(context.Background(), leftRaw, rightRaw)
	if err := CopyResumable(context.Background(), src2, dst2, plan2.Items, plan2.DestinationRoot, "mutate-src", plan2.Total, &CopyCheckpoint{}, overwriteCopy, func(Progress) {}, func(string) {}); err == nil {
		t.Fatal("same-metadata source mutation escaped streaming reviewedReader verification")
	}
	if _, err := os.Stat(filepath.Join(rp, "mutate.txt")); !os.IsNotExist(err) {
		t.Fatal("mutated source committed to destination")
	}
}
