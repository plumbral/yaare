package files

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSerializedCheckpointResumesAndRejectsChangedPrefix(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(map[bool]string{false: "restart", true: "changed-prefix"}[tamper], func(t *testing.T) {
			source, src := sandbox(t)
			dest, dst := sandbox(t)
			content := bytes.Repeat([]byte("private-file-body-"), 20000)
			if err := os.WriteFile(filepath.Join(src, "large"), content, 0600); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(dst, "large"), []byte("original"), 0600)
			items, total, err := Plan(context.Background(), source, "/", []string{"large"})
			if err != nil {
				t.Fatal(err)
			}
			cp := CopyCheckpoint{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			choice := func(string, bool) (string, error) { return "overwrite", nil }
			err = CopyResumable(ctx, source, dest, items, "/", "restart", total, &cp, choice, func(p Progress) {
				if p.Done >= 65536 {
					cancel()
				}
			}, func(string) {})
			if !errors.Is(err, context.Canceled) || cp.File == nil {
				t.Fatal("no paused checkpoint", err)
			}
			encoded, err := EncodeCheckpoint(cp)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte("private-file-body")) {
				t.Fatal("file plaintext was serialized")
			}
			restored, err := DecodeCheckpoint(encoded)
			if err != nil || restored.File.Digest != nil || len(restored.File.ExpectedDigest) != 32 {
				t.Fatal("bad restored digest", err)
			}
			if tamper {
				file, _ := os.OpenFile(filepath.Join(src, "large"), os.O_WRONLY, 0)
				file.WriteAt([]byte("changed"), 0)
				file.Close()
				os.Chtimes(filepath.Join(src, "large"), items[0].Modified, items[0].Modified)
			}
			err = CopyResumable(context.Background(), source, dest, items, "/", "restart", total, &restored, choice, func(Progress) {}, func(string) {})
			actual, _ := os.ReadFile(filepath.Join(dst, "large"))
			if tamper {
				var problem *ResumeProblem
				if !errors.As(err, &problem) || string(actual) != "original" {
					t.Fatal("changed prefix committed", err)
				}
			} else if err != nil || !bytes.Equal(actual, content) {
				t.Fatal("restart resume differs", err)
			}
		})
	}
}
