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

func TestRealOpenSSHBilateralSyncDeletionAndMerge(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, closeClient, e := r.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	home, _ := client.Getwd()
	remoteRoot := path.Join(home, "files", fmt.Sprintf("yaare-bilateral-%d", time.Now().UnixNano()))
	if e = client.Mkdir(remoteRoot); e != nil {
		t.Fatal(e)
	}
	remote := filefs.Remote{Client: client}
	defer func() { budget := 100; filefs.RemoveTree(context.Background(), remote, remoteRoot, &budget) }()
	localRoot := t.TempDir()
	root, e := os.OpenRoot(localRoot)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	local := filefs.Local{Root: root}
	localPut := func(name, content string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(localRoot, name), []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	remotePut := func(name, content string) {
		t.Helper()
		f, e := client.Create(path.Join(remoteRoot, name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write([]byte(content)); e != nil {
			t.Fatal(e)
		}
		if e = f.Close(); e != nil {
			t.Fatal(e)
		}
	}
	scan := func() filefs.Comparison {
		t.Helper()
		c, e := filefs.CompareTrees(ctx, local, remote, "/", remoteRoot, true, true, func(int, int64) {})
		if e != nil || !c.Complete {
			t.Fatal("scan", e, c.Message)
		}
		return c
	}
	base := filefs.SyncBaseline{}
	cycle := func() []filefs.BilateralAction {
		t.Helper()
		c := scan()
		actions, e := filefs.PlanBilateral(c, base, true)
		if e != nil {
			t.Fatal(e)
		}
		filefs.AcceptSyncBaseline(base, c)
		for _, direction := range []string{"upload", "download"} {
			plan := filefs.BilateralCopies(actions, "/", remoteRoot, direction)
			if len(plan.Items) == 0 {
				continue
			}
			source, dest := filefs.FS(local), filefs.FS(remote)
			if direction == "download" {
				source, dest = dest, source
			}
			if e = plan.Check(ctx, source, dest); e != nil {
				t.Fatal(e)
			}
			source, dest = plan.Wrap(ctx, source, dest)
			if e = filefs.Copy(ctx, source, dest, plan.Items, plan.DestinationRoot, fmt.Sprintf("sync-%d", time.Now().UnixNano()), plan.Total, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}); e != nil {
				t.Fatal(e)
			}
		}
		if e = filefs.ApplyBilateralDeletes(ctx, local, remote, "/", remoteRoot, actions); e != nil {
			t.Fatal(e)
		}
		filefs.AcceptSyncBaseline(base, scan())
		return actions
	}
	for _, name := range []string{"shared", "remove", "conflict.yaml"} {
		localPut(name, "same\n")
		remotePut(name, "same\n")
	}
	localPut("local-only", "本地新增\n")
	remotePut("remote-only", "远程新增\n")
	cycle()
	localPut("shared", "changed locally\n")
	os.Remove(filepath.Join(localRoot, "remove"))
	remotePut("remote-only", "changed remotely\n")
	localPut("conflict.yaml", "port: 8080\n")
	remotePut("conflict.yaml", "port: 9090\n")
	actions := cycle()
	conflicts := 0
	for _, a := range actions {
		if a.Kind == "conflict" {
			conflicts++
		}
	}
	if conflicts != 1 {
		t.Fatal("unexpected conflicts", actions)
	}
	if _, e = client.Stat(path.Join(remoteRoot, "remove")); !os.IsNotExist(e) {
		t.Fatal("remote deletion", e)
	}
	b, _ := os.ReadFile(filepath.Join(localRoot, "remote-only"))
	if string(b) != "changed remotely\n" {
		t.Fatal("download mismatch")
	}
	f, e := client.Open(path.Join(remoteRoot, "shared"))
	if e != nil {
		t.Fatal(e)
	}
	b, e = io.ReadAll(f)
	f.Close()
	if e != nil || string(b) != "changed locally\n" {
		t.Fatal("upload mismatch", e)
	}
	l, e := filefs.ReadText(ctx, local, "/conflict.yaml")
	if e != nil {
		t.Fatal(e)
	}
	rr, e := filefs.ReadText(ctx, remote, path.Join(remoteRoot, "conflict.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	if e = filefs.MergeBilateralText(ctx, local, remote, l.Path, rr.Path, l.Version, rr.Version, "port: 8081\n", "merge-fixture"); e != nil {
		t.Fatal(e)
	}
	final := scan()
	filefs.AcceptSyncBaseline(base, final)
	pending, e := filefs.PlanBilateral(final, base, true)
	if e != nil || len(pending) != 0 {
		t.Fatal("merge did not converge", e, pending)
	}
	t.Log("real SFTP new files, bidirectional changes, deletion propagation, isolated conflict and identical merge passed")
}
