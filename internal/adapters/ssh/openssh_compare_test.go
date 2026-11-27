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

func TestRealOpenSSHCompareAndReviewedCopyBothDirections(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client, closeClient, e := r.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	remote := filefs.Remote{Client: client}
	base := path.Join("/home/yaaretest/files", fmt.Sprintf("compare-%d", time.Now().UnixNano()))
	client.Mkdir(base)
	defer func() { budget := 100; filefs.RemoveTree(ctx, remote, base, &budget) }()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "nested"), 0700)
	os.WriteFile(filepath.Join(dir, "nested", "中文.txt"), []byte("local中文\n"), 0600)
	os.WriteFile(filepath.Join(dir, "changed.txt"), []byte("local"), 0600)
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	local := filefs.Local{Root: root}
	for _, item := range []struct{ name, body string }{{"changed.txt", "remote"}, {"remote-only.txt", "download"}} {
		f, e := client.Create(path.Join(base, item.name))
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte(item.body))
		f.Close()
	}
	if e = client.Chmod(path.Join(base, "changed.txt"), 0640); e != nil {
		t.Fatal(e)
	}
	compare := func() filefs.Comparison {
		c, e := filefs.CompareTrees(ctx, local, remote, "/", base, true, true, func(int, int64) {})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	c := compare()
	plan, e := filefs.BuildReviewedSync(c, "/", base, "upload", []string{"nested", "changed.txt"})
	if e != nil {
		t.Fatal(e)
	}
	if e = plan.Check(ctx, local, remote); e != nil {
		t.Fatal(e)
	}
	source, dest := plan.Wrap(ctx, local, remote)
	e = filefs.CopyResumable(ctx, source, dest, plan.Items, plan.DestinationRoot, "comparison-upload", plan.Total, &filefs.CopyCheckpoint{}, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}, func(string) {})
	if e != nil {
		t.Fatal("upload", e)
	}
	permissions, e := client.Lstat(path.Join(base, "changed.txt"))
	if e != nil || permissions.Mode().Perm() != 0640 {
		t.Fatal("overwrite lost permissions", e)
	}
	f, e := client.Open(path.Join(base, "nested/中文.txt"))
	if e != nil {
		t.Fatal(e)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "local中文\n" {
		t.Fatal("wire content")
	}
	plan, e = filefs.BuildReviewedSync(compare(), "/", base, "download", []string{"remote-only.txt"})
	if e != nil {
		t.Fatal(e)
	}
	if e = plan.Check(ctx, remote, local); e != nil {
		t.Fatal(e)
	}
	source, dest = plan.Wrap(ctx, remote, local)
	e = filefs.CopyResumable(ctx, source, dest, plan.Items, plan.DestinationRoot, "comparison-download", plan.Total, &filefs.CopyCheckpoint{}, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}, func(string) {})
	if e != nil {
		t.Fatal("download", e)
	}
	downloaded, e := os.ReadFile(filepath.Join(dir, "remote-only.txt"))
	if e != nil || string(downloaded) != "download" {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "changed.txt"), []byte("new local"), 0600)
	snapshot := compare()
	plan, e = filefs.BuildReviewedSync(snapshot, "/", base, "upload", []string{"changed.txt"})
	if e != nil {
		t.Fatal(e)
	}
	f, _ = client.Create(path.Join(base, "changed.txt"))
	f.Write([]byte("external change"))
	f.Close()
	if plan.Check(ctx, local, remote) == nil {
		t.Fatal("real preview conflict bypassed")
	}
	if !r.session.Connected() {
		t.Fatal("comparison disrupted PTY")
	}
}
