package sshclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	filefs "yaare/internal/adapters/files"
	"strings"
	"testing"
	"time"
)

func TestRealOpenSSHSCPBinaryEmptyDirectoryAndQuotedNames(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	client, closeClient, e := r.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	home, _ := client.Getwd()
	target := path.Join(home, "files", fmt.Sprintf("yaare-scp-%d", time.Now().UnixNano()))
	if e = client.Mkdir(target); e != nil {
		t.Fatal(e)
	}
	remote := filefs.Remote{Client: client}
	defer func() { budget := 100; filefs.RemoveTree(context.Background(), remote, target, &budget) }()
	scp := filefs.SCP{FS: remote, Context: ctx, OpenCommand: r.session.OpenCommand}
	localPath := t.TempDir()
	root, e := os.OpenRoot(localPath)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	local := filefs.Local{Root: root}
	binary := make([]byte, 1<<20)
	for i := range binary {
		binary[i] = byte(i % 256)
	}
	name := "中文' $; name.bin"
	os.WriteFile(filepath.Join(localPath, name), binary, 0600)
	os.WriteFile(filepath.Join(localPath, "empty.txt"), nil, 0600)
	os.Mkdir(filepath.Join(localPath, "directory"), 0700)
	os.WriteFile(filepath.Join(localPath, "directory", "nested"), []byte("nested\n"), 0600)
	os.Mkdir(filepath.Join(localPath, "download"), 0700)
	names := []string{name, "empty.txt", "directory"}
	items, total, e := filefs.Plan(ctx, local, "/", names)
	if e != nil {
		t.Fatal(e)
	}
	cp := filefs.CopyCheckpoint{}
	if e = filefs.CopyResumable(ctx, local, scp, items, target, "scp-upload", total, &cp, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}, func(string) {}); e != nil {
		t.Fatal("SCP upload", e)
	}
	f, e := client.Open(path.Join(target, name))
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(f)
	f.Close()
	if e != nil || !bytes.Equal(b, binary) {
		t.Fatal("SCP bytes", e)
	}
	items, total, e = filefs.Plan(ctx, remote, target, names)
	if e != nil {
		t.Fatal(e)
	}
	cp = filefs.CopyCheckpoint{}
	if e = filefs.CopyResumable(ctx, scp, local, items, "/download", "scp-download", total, &cp, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}, func(string) {}); e != nil {
		t.Fatal("SCP download", e)
	}
	b, e = os.ReadFile(filepath.Join(localPath, "download", name))
	if e != nil || !bytes.Equal(b, binary) {
		t.Fatal("downloaded bytes", e)
	}
	if st, e := os.Stat(filepath.Join(localPath, "download", "empty.txt")); e != nil || st.Size() != 0 {
		t.Fatal("empty", e)
	}
	if !r.session.Connected() {
		t.Fatal("SCP closed shared SSH")
	}
	larger := bytes.Repeat([]byte("restart current SCP file\n"), 150000)
	os.WriteFile(filepath.Join(localPath, name), larger, 0600)
	items, total, e = filefs.Plan(ctx, local, "/", []string{name})
	if e != nil {
		t.Fatal(e)
	}
	interrupted, stop := context.WithCancel(ctx)
	cancelledSCP := scp
	cancelledSCP.Context = interrupted
	cp = filefs.CopyCheckpoint{}
	e = filefs.CopyResumable(interrupted, local, cancelledSCP, items, target, "scp-cancel", total, &cp, func(string, bool) (string, error) { return "overwrite", nil }, func(p filefs.Progress) {
		if p.Done >= 65536 {
			stop()
		}
	}, func(string) {})
	if !errors.Is(e, context.Canceled) || cp.File == nil {
		t.Fatal("SCP cancellation", e)
	}
	f, e = client.Open(path.Join(target, name))
	if e != nil {
		t.Fatal(e)
	}
	b, e = io.ReadAll(f)
	f.Close()
	if e != nil || !bytes.Equal(b, binary) {
		t.Fatal("cancel overwrote original target", e)
	}
	if e = cp.Abort(remote); e != nil {
		t.Fatal(e)
	}
	cp.Progress.Done -= cp.File.Offset
	cp.File = nil
	if e = filefs.CopyResumable(ctx, local, scp, items, target, "scp-from-head", total, &cp, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}, func(string) {}); e != nil {
		t.Fatal("restart from head", e)
	}
	f, e = client.Open(path.Join(target, name))
	if e != nil {
		t.Fatal(e)
	}
	b, e = io.ReadAll(f)
	f.Close()
	if e != nil || !bytes.Equal(b, larger) {
		t.Fatal("restart bytes", e)
	}
	if !r.session.Write([]byte("printf '\\163\\143\\160\\055\\160\\164\\171\\055\\157\\153\\n'\n")) {
		t.Fatal("terminal write after SCP")
	}
	eventually(t, func() bool { return strings.Contains(r.text(), "scp-pty-ok") })
	if strings.Contains(r.text(), "\x00\x01\x02\x03") {
		t.Fatal("SCP binary bytes leaked to terminal")
	}
	t.Logf("real SCP upload/download with empty files, recursive directory and literal shell metacharacters; SHA256 %x", sha256.Sum256(binary))
}
