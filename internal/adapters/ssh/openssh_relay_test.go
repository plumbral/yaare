package sshclient

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	filefs "yaare/internal/adapters/files"
	"strings"
	"testing"
	"time"
)

func TestRealOpenSSHRemoteToRemoteRelayAndConflict(t *testing.T) {
	sourceSession := realFixture(t, "yaaretest")
	sourceSession.wait(t, "connected")
	targetSession := realFixture(t, "yaaretest")
	targetSession.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	left, cl, e := sourceSession.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer cl()
	right, cr, e := targetSession.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer cr()
	home, _ := left.Getwd()
	base := path.Join(home, "files", fmt.Sprintf("yaare-relay-%d", time.Now().UnixNano()))
	if e = left.Mkdir(base); e != nil {
		t.Fatal(e)
	}
	l, r := filefs.Remote{Client: left}, filefs.Remote{Client: right}
	defer func() { budget := 100; filefs.RemoveTree(context.Background(), l, base, &budget) }()
	from, to := path.Join(base, "source"), path.Join(base, "target")
	left.Mkdir(from)
	right.Mkdir(to)
	left.Mkdir(path.Join(from, "中文目录"))
	content := strings.Repeat("remote relay 中文\n", 50000)
	name := path.Join(from, "中文目录", "file.txt")
	f, e := left.Create(name)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = io.WriteString(f, content); e != nil {
		t.Fatal(e)
	}
	f.Close()
	plan, total, e := filefs.Plan(ctx, l, from, []string{"中文目录"})
	if e != nil {
		t.Fatal(e)
	}
	cp := filefs.CopyCheckpoint{}
	if e = filefs.CopyResumable(ctx, l, r, plan, to, "relay-first", total, &cp, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}, func(string) {}); e != nil {
		t.Fatal(e)
	}
	check, e := right.Open(path.Join(to, "中文目录", "file.txt"))
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.New()
	_, e = io.Copy(h, check)
	check.Close()
	expected := sha256.Sum256([]byte(content))
	if e != nil || fmt.Sprintf("%x", h.Sum(nil)) != fmt.Sprintf("%x", expected) {
		t.Fatal("relay hash", e)
	}
	names := []string{}
	if e = filefs.Copy(ctx, l, r, plan, to, "relay-keep", total, func(name string, dir bool) (string, error) {
		names = append(names, name)
		if dir {
			return "overwrite", nil
		}
		return "keepBoth", nil
	}, func(filefs.Progress) {}); e != nil {
		t.Fatal(e)
	}
	if _, e = right.Stat(path.Join(to, "中文目录", "file (2).txt")); e != nil {
		t.Fatal("keep both", names, e)
	}
	if !sourceSession.session.Connected() || !targetSession.session.Connected() {
		t.Fatal("relay closed shared SSH")
	}
	t.Logf("two independent verified SSH clients relayed %d bytes; SHA256 %x; keep-both and directory copy passed", total, expected)
}
