package sshclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	filefs "yaare/internal/adapters/files"
	"testing"
	"time"
)

func TestRealOpenSSHUploadNativeSelectionAndConflicts(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, closeClient, e := r.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	remote := filefs.Remote{Client: client}
	dir := t.TempDir()
	name := fmt.Sprintf("dropped-%d", time.Now().UnixNano())
	os.Mkdir(filepath.Join(dir, name), 0700)
	os.Mkdir(filepath.Join(dir, name, "empty"), 0700)
	os.WriteFile(filepath.Join(dir, name, "中文.txt"), []byte("本地拖入🦂\n"), 0600)
	source, e := filefs.OpenDropped([]string{filepath.Join(dir, name)})
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	items, total, e := filefs.Plan(ctx, source, "/", source.Names())
	if e != nil {
		t.Fatal(e)
	}
	target := path.Join("/home/yaaretest/files", name)
	defer func() { budget := 500; _ = filefs.RemoveTree(ctx, remote, target, &budget) }()
	copy := func(policy string) error {
		return filefs.CopyResumable(ctx, source, remote, items, "/home/yaaretest/files", name, total, &filefs.CopyCheckpoint{}, func(string, bool) (string, error) {
			if policy == "ask" {
				return "", filefs.ErrConflict
			}
			return policy, nil
		}, func(filefs.Progress) {}, func(string) {})
	}
	if e = copy("ask"); e != nil {
		t.Fatal(e)
	}
	f, e := client.Open(path.Join(target, "中文.txt"))
	if e != nil {
		t.Fatal(e)
	}
	bytes, e := io.ReadAll(f)
	f.Close()
	if e != nil || string(bytes) != "本地拖入🦂\n" {
		t.Fatal("upload content", e)
	}
	if e = copy("ask"); e != filefs.ErrConflict {
		t.Fatal("missing conflict", e)
	}
	if e = copy("skip"); e != nil {
		t.Fatal(e)
	}
	if !r.session.Connected() {
		t.Fatal("upload disrupted SSH")
	}
}
