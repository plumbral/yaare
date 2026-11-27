package sshclient

import (
	"context"
	"fmt"
	"path"
	filefs "yaare/internal/adapters/files"
	"testing"
	"time"
)

func TestRealOpenSSHScopedFilenameSearch(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, closeClient, e := r.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	remote := filefs.Remote{Client: client}
	base := path.Join("/home/yaaretest/files", fmt.Sprintf("search-%d", time.Now().UnixNano()))
	client.Mkdir(base)
	client.Mkdir(path.Join(base, "nested"))
	client.Mkdir(path.Join(base, ".hidden"))
	defer func() { budget := 100; filefs.RemoveTree(ctx, remote, base, &budget) }()
	for _, p := range []string{"nested/中文-needle.txt", ".hidden/needle.txt"} {
		f, e := client.Create(path.Join(base, p))
		if e != nil {
			t.Fatal(e)
		}
		f.Close()
	}
	client.Symlink(path.Join(base, ".hidden"), path.Join(base, "linked"))
	hits := []filefs.SearchHit{}
	var last filefs.SearchProgress
	e = filefs.SearchNames(ctx, remote, base, "NEEDLE", false, func(p filefs.SearchProgress) error { hits = append(hits, p.Hits...); last = p; return nil })
	if e != nil || len(hits) != 1 || !last.Done {
		t.Fatal("real search", len(hits), e)
	}
	if !r.session.Connected() {
		t.Fatal("PTY disrupted")
	}
}
