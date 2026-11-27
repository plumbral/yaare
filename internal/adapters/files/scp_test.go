package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testSCPInput struct{ bytes.Buffer }

func (w *testSCPInput) Close() error { return nil }
func TestSCPRejectsUnexpectedNamesSizesAndCommands(t *testing.T) {
	local, dir := sandbox(t)
	os.WriteFile(filepath.Join(dir, "a"), []byte("data"), 0600)
	for _, response := range []string{"C0600 4 ../other\n", "C0600 5 a\n", "C9999 4 a\n", strings.Repeat("C", 5000), "\x01server error\n"} {
		closed := false
		f := SCP{FS: local, Context: context.Background(), OpenCommand: func(context.Context, string) (RemoteCommand, error) {
			return RemoteCommand{Input: &testSCPInput{}, Output: strings.NewReader(response), Wait: func() error { return nil }, Close: func() { closed = true }}, nil
		}}
		if _, e := f.Open("/a"); e == nil || !closed {
			t.Fatal("bad protocol accepted or channel leaked", response[:min(len(response), 40)], closed)
		}
	}
	for _, p := range []string{"relative", "/new\ncommand", "/new\rcommand", "/new\x00"} {
		if _, e := scpPath(p); e == nil {
			t.Fatal("invalid SCP path", p)
		}
	}
	quoted, e := scpPath("/literal' $; path")
	if e != nil || quoted != "'/literal'\\'' $; path'" {
		t.Fatal("shell quoting", quoted, e)
	}
}

type closeFailureFS struct{ FS }
type closeFailureReader struct{ io.ReadCloser }

func (r closeFailureReader) Close() error {
	_ = r.ReadCloser.Close()
	return errors.New("stream close unconfirmed")
}
func (f closeFailureFS) Open(p string) (io.ReadCloser, error) {
	r, e := f.FS.Open(p)
	return closeFailureReader{r}, e
}
func TestUnconfirmedSourceCloseNeverCommitsTarget(t *testing.T) {
	for _, resumable := range []bool{false, true} {
		t.Run(map[bool]string{false: "copy", true: "resumable"}[resumable], func(t *testing.T) {
			source, sp := sandbox(t)
			dest, dp := sandbox(t)
			os.WriteFile(filepath.Join(sp, "a"), []byte("new content"), 0600)
			os.WriteFile(filepath.Join(dp, "a"), []byte("original"), 0600)
			items, total, e := Plan(context.Background(), source, "/", []string{"a"})
			if e != nil {
				t.Fatal(e)
			}
			choice := func(string, bool) (string, error) { return "overwrite", nil }
			if resumable {
				e = CopyResumable(context.Background(), closeFailureFS{source}, dest, items, "/", "unconfirmed", total, &CopyCheckpoint{}, choice, func(Progress) {}, func(string) {})
			} else {
				e = Copy(context.Background(), closeFailureFS{source}, dest, items, "/", "unconfirmed", total, choice, func(Progress) {})
			}
			if e == nil {
				t.Fatal("unconfirmed stream reported success")
			}
			b, _ := os.ReadFile(filepath.Join(dp, "a"))
			if string(b) != "original" {
				t.Fatal("target committed before source confirmed")
			}
		})
	}
}
