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
	"testing"
	"time"
)

func TestRealOpenSSHResumableUploadDownloadAndReconnect(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	identity, err := r.session.FileIdentity()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client, closeClient, err := r.session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { closeClient() }()
	home, _ := client.Getwd()
	target := path.Join(home, "files", fmt.Sprintf("yaare-resume-%d", time.Now().UnixNano()))
	if err = client.Mkdir(target); err != nil {
		t.Fatal(err)
	}
	localPath := t.TempDir()
	root, err := os.OpenRoot(localPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	local := filefs.Local{Root: root}
	data := bytes.Repeat([]byte("真实 SFTP resume 中文\n"), 150000)
	os.WriteFile(filepath.Join(localPath, "large.bin"), data, 0600)
	os.Mkdir(filepath.Join(localPath, "download"), 0700)
	items, total, err := filefs.Plan(ctx, local, "/", []string{"large.bin"})
	if err != nil {
		t.Fatal(err)
	}
	cp := &filefs.CopyCheckpoint{}
	paused, stop := context.WithCancel(ctx)
	choice := func(string, bool) (string, error) { return "overwrite", nil }
	err = filefs.CopyResumable(paused, local, filefs.Remote{Client: client}, items, target, "upload", total, cp, choice, func(p filefs.Progress) {
		if p.Done >= 256*1024 {
			stop()
		}
	}, func(string) {})
	if !errors.Is(err, context.Canceled) || cp.File == nil {
		t.Fatal("upload pause lost checkpoint", err)
	}
	offset := cp.File.Offset
	encoded, err := filefs.EncodeCheckpoint(*cp)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := filefs.DecodeCheckpoint(encoded)
	if err != nil {
		t.Fatal(err)
	}
	cp = &restored
	closeClient()
	client, closeClient, err = r.session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	remote := filefs.Remote{Client: client}
	if err = filefs.CopyResumable(ctx, local, remote, items, target, "upload", total, cp, choice, func(filefs.Progress) {}, func(string) {}); err != nil {
		t.Fatal("upload resume", err)
	}
	file, err := client.Open(path.Join(target, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	io.Copy(hash, file)
	file.Close()
	expected := sha256.Sum256(data)
	if !bytes.Equal(hash.Sum(nil), expected[:]) || cp.Progress.Done != total || offset >= total {
		t.Fatal("uploaded checksum/progress mismatch")
	}
	items, total, err = filefs.Plan(ctx, remote, target, []string{"large.bin"})
	if err != nil {
		t.Fatal(err)
	}
	cp = &filefs.CopyCheckpoint{}
	interrupted := false
	err = filefs.CopyResumable(ctx, remote, local, items, "/download", "download", total, cp, choice, func(p filefs.Progress) {
		if !interrupted && p.Done >= 256*1024 {
			interrupted = true
			closeClient()
		}
	}, func(string) {})
	if err == nil || cp.File == nil || cp.File.Offset == 0 {
		t.Fatal("SFTP disconnect was reported completed", err)
	}
	if !r.session.Write([]byte("printf 'YAARE_PTY_AFTER_TRANSFER\\n'\n")) {
		t.Fatal("transfer closed PTY")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		alive := bytes.Contains(r.output.Bytes(), []byte("YAARE_PTY_AFTER_TRANSFER"))
		r.mu.Unlock()
		if alive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("PTY not responsive after subsystem interruption")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.session.Close()
	r.ended(t)
	encoded, err = filefs.EncodeCheckpoint(*cp)
	if err != nil {
		t.Fatal(err)
	}
	restored, err = filefs.DecodeCheckpoint(encoded)
	if err != nil {
		t.Fatal(err)
	}
	cp = &restored
	r2 := realFixture(t, "yaaretest")
	r2.wait(t, "connected")
	identity2, err := r2.session.FileIdentity()
	if err != nil || identity2 != identity {
		t.Fatal("verified endpoint identity changed", err)
	}
	client, closeClient, err = r2.session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	remote = filefs.Remote{Client: client}
	defer func() {
		budget := 100
		if e := filefs.RemoveTree(ctx, remote, target, &budget); e != nil {
			t.Error(e)
		}
	}()
	if err = filefs.CopyResumable(ctx, remote, local, items, "/download", "download", total, cp, choice, func(filefs.Progress) {}, func(string) {}); err != nil {
		t.Fatal("download after SSH reconnect", err)
	}
	actual, err := os.ReadFile(filepath.Join(localPath, "download", "large.bin"))
	if err != nil || sha256.Sum256(actual) != expected || cp.Progress.Done != total {
		t.Fatal("download checksum/progress mismatch", err)
	}
	t.Logf("Verified upload pause/resume and download disconnect/new-SSH resume: %d bytes, SHA256 %x; PTY remains live", total, expected)
}
