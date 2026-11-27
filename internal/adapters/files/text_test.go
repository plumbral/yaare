package files

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTextSavePreservesBOMAndLineEndings(t *testing.T) {
	for _, source := range []string{"中文\nhello\n", "\ufeff中文\r\nhello\r\n", "", "\ufeff"} {
		t.Run(source, func(t *testing.T) {
			f, root := sandbox(t)
			target := filepath.Join(root, "test.txt")
			if err := os.WriteFile(target, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := ReadText(context.Background(), f, "/test.txt")
			if err != nil {
				t.Fatal(err)
			}
			result, err := SaveText(context.Background(), f, "/test.txt", file.Version, file.Content+"修改\n", "test")
			if err != nil || result.Status != "saved" {
				t.Fatal(result, err)
			}
			data, _ := os.ReadFile(target)
			want := file.Content + "修改\n"
			if file.LineEnding == "CRLF" {
				want = strings.ReplaceAll(want, "\n", "\r\n")
			}
			if file.BOM {
				want = "\ufeff" + want
			}
			if string(data) != want {
				t.Fatal("encoding or newlines changed")
			}
		})
	}
}

func TestTextConflictDetectsSameSizeAndTimestampAndRechecksOverwrite(t *testing.T) {
	f, root := sandbox(t)
	target := filepath.Join(root, "note.txt")
	os.WriteFile(target, []byte("first"), 0600)
	file, err := ReadText(context.Background(), f, "/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(target)
	os.WriteFile(target, []byte("other"), 0600)
	os.Chtimes(target, info.ModTime(), info.ModTime())
	result, err := SaveText(context.Background(), f, "/note.txt", file.Version, "local", "first")
	if err != nil || result.Status != "conflict" || result.File.Content != "other" {
		t.Fatal("content conflict missed", err)
	}
	approved := result.File.Version
	os.WriteFile(target, []byte("third"), 0600)
	os.Chtimes(target, info.ModTime(), info.ModTime())
	result, err = SaveText(context.Background(), f, "/note.txt", approved, "local", "second")
	if err != nil || result.Status != "conflict" || result.File.Content != "third" {
		t.Fatal("stale overwrite bypassed conflict", err)
	}
	result, err = SaveText(context.Background(), f, "/note.txt", result.File.Version, "local", "third")
	if err != nil || result.Status != "saved" {
		t.Fatal(err)
	}
}

type textFaultFS struct {
	FS
	create func(string) (io.WriteCloser, error)
	rename func(string, string, bool) error
}

func (f textFaultFS) Create(p string) (io.WriteCloser, error) {
	if f.create != nil {
		return f.create(p)
	}
	return f.FS.Create(p)
}
func (f textFaultFS) Rename(a, b string, overwrite bool) error {
	if f.rename != nil {
		return f.rename(a, b, overwrite)
	}
	return f.FS.Rename(a, b, overwrite)
}

type failingTextWriter struct{ io.WriteCloser }

func (f failingTextWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }

func TestTextSaveWriteRenameAndCancellationPreserveOriginal(t *testing.T) {
	for _, kind := range []string{"write", "rename", "cancel", "concurrent"} {
		t.Run(kind, func(t *testing.T) {
			f, root := sandbox(t)
			target := filepath.Join(root, "note.txt")
			os.WriteFile(target, []byte("original"), 0600)
			file, _ := ReadText(context.Background(), f, "/note.txt")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wrapped := textFaultFS{FS: f}
			wrapped.create = func(p string) (io.WriteCloser, error) {
				out, err := f.Create(p)
				if err != nil {
					return nil, err
				}
				switch kind {
				case "write":
					return failingTextWriter{out}, nil
				case "cancel":
					cancel()
				case "concurrent":
					os.WriteFile(target, []byte("external"), 0600)
				}
				return out, nil
			}
			if kind == "rename" {
				wrapped.rename = func(string, string, bool) error { return os.ErrPermission }
			}
			result, err := SaveText(ctx, wrapped, "/note.txt", file.Version, "replacement", "failure")
			if kind == "concurrent" {
				if err != nil || result.Status != "conflict" {
					t.Fatal("last check missed change", err)
				}
			} else if err == nil {
				t.Fatal("failure was reported as success")
			}
			data, _ := os.ReadFile(target)
			want := "original"
			if kind == "concurrent" {
				want = "external"
			}
			if string(data) != want {
				t.Fatal("original was truncated")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 1 {
				t.Fatal("temporary file not cleaned up")
			}
		})
	}
}

func TestTextRejectsBinaryEncodingOversizeAndInvalidVersions(t *testing.T) {
	for _, item := range []struct {
		data string
		code string
	}{{"\x00data", "TEXT_BINARY"}, {"\xff", "TEXT_ENCODING"}, {strings.Repeat("x", MaxTextBytes+1), "TEXT_TOO_LARGE"}, {"a\r\nb\n", "TEXT_LINE_ENDING"}} {
		f, root := sandbox(t)
		os.WriteFile(filepath.Join(root, "file"), []byte(item.data), 0600)
		_, err := ReadText(context.Background(), f, "/file")
		var problem *TextProblem
		if !errors.As(err, &problem) || problem.Code != item.code {
			t.Fatal("wrong rejection", item.code, err)
		}
	}
	f, root := sandbox(t)
	os.WriteFile(filepath.Join(root, "file"), []byte("safe"), 0600)
	if _, err := SaveText(context.Background(), f, "/file", strings.Repeat("z", 64), "edited", "../escape"); err == nil {
		t.Fatal("invalid revision accepted")
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := ReadText(ctx, f, "/file"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancelled read accepted")
	}
}
