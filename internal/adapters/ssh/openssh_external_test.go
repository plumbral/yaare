package sshclient

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	filefs "yaare/internal/adapters/files"
)

func TestRealOpenSSHSFTPAndTerminalIsolation(t *testing.T) {
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" {
		t.Skip("set YAARE_OPENSSH_TEST=1 for the local Docker fixture")
	}
	fingerprint := os.Getenv("YAARE_OPENSSH_FINGERPRINT")
	if fingerprint == "" {
		t.Fatal("fixture fingerprint is required")
	}
	cfg := config("127.0.0.1:22222")
	cfg.Username = "yaaretest"
	cfg.Password = []byte("Yaare-OpenSSH-Test-Only!")
	cfg.Timeout = 10 * time.Second
	run := begin(t, cfg, store(t))
	challenge := run.wait(t, "hostkey").Challenge
	if !strings.Contains(","+fingerprint+",", ","+challenge.Fingerprint+",") {
		t.Fatalf("unexpected real server fingerprint (%s %s)", challenge.Algorithm, challenge.Fingerprint)
	}
	if !run.session.Resolve(challenge.ID, "once") {
		t.Fatal("trust failed")
	}
	run.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	client, closeClient, e := run.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	remote := filefs.Remote{Client: client}
	home, e := client.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	target := path.Join(home, "files", fmt.Sprintf("yaare-test-%d", time.Now().UnixNano()))
	if e = client.Mkdir(target); e != nil {
		t.Fatal(e)
	}
	defer func() { budget := 100; _ = filefs.RemoveTree(context.Background(), remote, target, &budget) }()
	localPath := t.TempDir()
	content := []byte(strings.Repeat("中文🦂\n", 50000))
	if e = os.WriteFile(filepath.Join(localPath, "sample.txt"), content, 0600); e != nil {
		t.Fatal(e)
	}
	root, e := os.OpenRoot(localPath)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	local := filefs.Local{Root: root}
	items, total, e := filefs.Plan(ctx, local, "/", []string{"sample.txt"})
	if e != nil {
		t.Fatal(e)
	}
	if e = filefs.Copy(ctx, local, remote, items, target, "external", total, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}); e != nil {
		t.Fatal(e)
	}
	handle, e := client.Open(path.Join(target, "sample.txt"))
	if e != nil {
		t.Fatal(e)
	}
	received, e := io.ReadAll(handle)
	handle.Close()
	if e != nil || sha256.Sum256(received) != sha256.Sum256(content) {
		t.Fatal("real SFTP bytes differ", e)
	}
	if e = client.Chmod(path.Join(target, "sample.txt"), 0640); e != nil {
		t.Fatal(e)
	}
	info, e := client.Stat(path.Join(target, "sample.txt"))
	if e != nil || info.Mode().Perm() != 0640 {
		t.Fatal("chmod failed", e)
	}
	downloads := t.TempDir()
	downloadRoot, e := os.OpenRoot(downloads)
	if e != nil {
		t.Fatal(e)
	}
	defer downloadRoot.Close()
	plan, size, e := filefs.Plan(ctx, remote, target, []string{"sample.txt"})
	if e != nil {
		t.Fatal(e)
	}
	if e = filefs.Copy(ctx, remote, filefs.Local{Root: downloadRoot}, plan, "/", "download", size, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}); e != nil {
		t.Fatal(e)
	}
	bytes, e := os.ReadFile(filepath.Join(downloads, "sample.txt"))
	if e != nil || sha256.Sum256(bytes) != sha256.Sum256(content) {
		t.Fatal("download bytes differ", e)
	}
	if _, e = client.OpenFile(path.Join(home, "readonly", "denied"), os.O_CREATE|os.O_WRONLY); e == nil {
		t.Fatal("permission refusal was not enforced")
	}
	other, closeOther, e := run.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	closeOther()
	if _, e = other.ReadDir(home); e == nil {
		t.Fatal("cancelled file channel stayed usable")
	}
	if !run.session.Write([]byte("printf '__YAARE_PTY_AFTER_SFTP__\\n'\r")) {
		t.Fatal("SFTP cancellation killed terminal")
	}
	eventually(t, func() bool { return strings.Contains(run.text(), "__YAARE_PTY_AFTER_SFTP__") })
	if !run.session.Resize(110, 35) {
		t.Fatal("real PTY resize failed")
	}
}

func TestRealOpenSSHBrowseYieldsAtSessionLimit(t *testing.T) {
	run := realFixture(t, "yaaretest")
	run.wait(t, "connected")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	home, _, err := run.session.BrowseDirectory(ctx, ".")
	if err != nil {
		t.Fatal(err)
	}
	const fileChannelsWithPTY = 9
	for opened := 0; opened < fileChannelsWithPTY; opened++ {
		client, closeClient, err := run.session.OpenFiles(ctx)
		if err != nil {
			t.Fatalf("open file channel %d: %v", opened+1, err)
		}
		defer closeClient()
		if _, err = client.Stat(home); err != nil {
			t.Fatal(err)
		}
	}
	run.session.browseSlot <- struct{}{}
	browseStillOpen := run.session.browseIdle != nil
	<-run.session.browseSlot
	if browseStillOpen {
		t.Fatal("fixture MaxSessions=10 did not reclaim the idle browse channel")
	}
	if !run.session.Write([]byte("printf '%s%s\\n' YAARE_LIMIT_ PTY_OK\r")) {
		t.Fatal("PTY stopped accepting input after the browse channel yielded")
	}
	eventually(t, func() bool { return strings.Contains(run.text(), "YAARE_LIMIT_PTY_OK") })
}
