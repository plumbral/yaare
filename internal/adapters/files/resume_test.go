package files

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResumeVerifiedPrefixAndUnknownWriteTail(t *testing.T) {
	source, src := sandbox(t)
	dest, dst := sandbox(t)
	data := bytes.Repeat([]byte("中文-transfer-resume\n"), 100000)
	os.WriteFile(filepath.Join(src, "large.bin"), data, 0600)
	os.WriteFile(filepath.Join(dst, "large.bin"), []byte("old"), 0600)
	items, total, err := Plan(context.Background(), source, "/", []string{"large.bin"})
	if err != nil {
		t.Fatal(err)
	}
	cp := &CopyCheckpoint{}
	ctx, cancel := context.WithCancel(context.Background())
	choice := func(string, bool) (string, error) { return "overwrite", nil }
	err = CopyResumable(ctx, source, dest, items, "/", "tail", total, cp, choice, func(p Progress) {
		if p.Done >= 100000 {
			cancel()
		}
	}, func(string) {})
	if !errors.Is(err, context.Canceled) || cp.File == nil || cp.File.Offset == 0 {
		t.Fatal("missing paused checkpoint", err)
	}
	old, _ := os.ReadFile(filepath.Join(dst, "large.bin"))
	if string(old) != "old" {
		t.Fatal("paused copy replaced target")
	}
	stage, err := dest.OpenResume(cp.File.Temporary)
	if err != nil {
		t.Fatal(err)
	}
	stage.Seek(0, 2)
	stage.Write([]byte("unacknowledged"))
	stage.Close()
	offset := cp.File.Offset
	if err = CopyResumable(context.Background(), source, dest, items, "/", "tail", total, cp, choice, func(Progress) {}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	actual, _ := os.ReadFile(filepath.Join(dst, "large.bin"))
	if !bytes.Equal(actual, data) || cp.Progress.Done != total || cp.Progress.Files != 1 || offset >= total {
		t.Fatal("resumed bytes/progress invalid")
	}
	matches, _ := filepath.Glob(filepath.Join(dst, "*.part"))
	if len(matches) != 0 {
		t.Fatal("stage not committed")
	}
}
func TestResumeRejectsChangedSourceStageAndTarget(t *testing.T) {
	for _, kind := range []string{"source-metadata", "source-prefix", "stage-prefix", "stage-short", "target"} {
		t.Run(kind, func(t *testing.T) {
			source, src := sandbox(t)
			dest, dst := sandbox(t)
			data := bytes.Repeat([]byte("resume-original-"), 50000)
			sourcePath, targetPath := filepath.Join(src, "large.bin"), filepath.Join(dst, "large.bin")
			os.WriteFile(sourcePath, data, 0600)
			os.WriteFile(targetPath, []byte("old"), 0600)
			items, total, _ := Plan(context.Background(), source, "/", []string{"large.bin"})
			cp := &CopyCheckpoint{}
			ctx, cancel := context.WithCancel(context.Background())
			choice := func(string, bool) (string, error) { return "overwrite", nil }
			err := CopyResumable(ctx, source, dest, items, "/", "tamper", total, cp, choice, func(p Progress) {
				if p.Done >= 65536 {
					cancel()
				}
			}, func(string) {})
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			stagePath := filepath.Join(dst, localPath(cp.File.Temporary))
			switch kind {
			case "source-metadata":
				os.Chtimes(sourcePath, time.Now(), time.Now().Add(time.Hour))
			case "source-prefix":
				f, _ := os.OpenFile(sourcePath, os.O_WRONLY, 0)
				f.WriteAt([]byte("CHANGED"), 0)
				f.Close()
				os.Chtimes(sourcePath, items[0].Modified, items[0].Modified)
			case "stage-prefix":
				f, _ := os.OpenFile(stagePath, os.O_WRONLY, 0)
				f.WriteAt([]byte("CHANGED"), 0)
				f.Close()
			case "stage-short":
				os.Truncate(stagePath, 1)
			case "target":
				os.WriteFile(targetPath, []byte("external change"), 0600)
			}
			err = CopyResumable(context.Background(), source, dest, items, "/", "tamper", total, cp, choice, func(Progress) {}, func(string) {})
			var problem *ResumeProblem
			if !errors.As(err, &problem) {
				t.Fatal("unsafe resume accepted", kind, err)
			}
			target, _ := os.ReadFile(targetPath)
			if kind == "target" && string(target) != "external change" || kind != "target" && string(target) != "old" {
				t.Fatal("original was overwritten")
			}
			if err = cp.Abort(dest); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(stagePath); !os.IsNotExist(err) {
				t.Fatal("cancel did not remove owned stage")
			}
		})
	}
}
func TestConflictDestinationFollowsMostSpecificDirectoryRename(t *testing.T) {
	cp := CopyCheckpoint{Renamed: map[string]string{"dir": "dir (2)", "dir/sub": "dir (2)/sub (3)"}}
	for input, want := range map[string]string{
		"dir/sub/file": "dir (2)/sub (3)/file", "dir/subdir/file": "dir (2)/subdir/file",
		"dir": "dir (2)", "other/file": "other/file",
	} {
		if got := cp.DestinationRelative(input); got != want {
			t.Errorf("DestinationRelative(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResumeKeepsCompletedEntriesAndDirectoryRename(t *testing.T) {
	source, src := sandbox(t)
	dest, dst := sandbox(t)
	os.Mkdir(filepath.Join(src, "folder"), 0700)
	os.Mkdir(filepath.Join(dst, "folder"), 0700)
	os.WriteFile(filepath.Join(src, "folder", "a.txt"), []byte("first"), 0600)
	data := bytes.Repeat([]byte("next"), 100000)
	os.WriteFile(filepath.Join(src, "folder", "b.txt"), data, 0600)
	items, total, _ := Plan(context.Background(), source, "/", []string{"folder"})
	cp := &CopyCheckpoint{}
	ctx, cancel := context.WithCancel(context.Background())
	conflicts := 0
	choice := func(string, bool) (string, error) { conflicts++; return "keepBoth", nil }
	err := CopyResumable(ctx, source, dest, items, "/", "tree", total, cp, choice, func(p Progress) {
		if p.Done > 100000 {
			cancel()
		}
	}, func(string) {})
	if !errors.Is(err, context.Canceled) || cp.Progress.Files != 1 {
		t.Fatal(err, cp.Progress)
	}
	os.WriteFile(filepath.Join(dst, "folder (2)", "a.txt"), []byte("completed file was edited"), 0600)
	if err = CopyResumable(context.Background(), source, dest, items, "/", "tree", total, cp, choice, func(Progress) {}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(dst, "folder (2)", "a.txt"))
	b, _ := os.ReadFile(filepath.Join(dst, "folder (2)", "b.txt"))
	if string(a) != "completed file was edited" || !bytes.Equal(b, data) || conflicts != 1 || cp.Progress.Done != total {
		t.Fatal("completed entries replayed", conflicts, cp.Progress)
	}
}
func TestResumableConflictAndSkipReleaseCheckpoint(t *testing.T) {
	source, src := sandbox(t)
	dest, dst := sandbox(t)
	os.WriteFile(filepath.Join(src, "a"), []byte("123"), 0600)
	os.WriteFile(filepath.Join(dst, "a"), []byte("old"), 0600)
	items, total, _ := Plan(context.Background(), source, "/", []string{"a"})
	cp := &CopyCheckpoint{}
	err := CopyResumable(context.Background(), source, dest, items, "/", "conflict", total, cp, func(string, bool) (string, error) { return "", ErrConflict }, func(Progress) {}, func(string) {})
	if !errors.Is(err, ErrConflict) || cp.Index != 0 || cp.File != nil {
		t.Fatal("conflict failed to suspend")
	}
	err = CopyResumable(context.Background(), source, dest, items, "/", "conflict", total, cp, func(string, bool) (string, error) { return "skip", nil }, func(Progress) {}, func(string) {})
	if err != nil || cp.Progress.Total != 0 || cp.Progress.Done != 0 || cp.Progress.Skipped != 1 {
		t.Fatal("skip inflated completed bytes", cp.Progress, err)
	}
}
