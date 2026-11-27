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

func realFixture(t *testing.T, username string) *runResult {
	t.Helper()
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" {
		t.Skip("requires isolated Docker fixture")
	}
	cfg := config("127.0.0.1:22222")
	cfg.Username = username
	cfg.Password = []byte("Yaare-OpenSSH-Test-Only!")
	cfg.Timeout = 10 * time.Second
	r := begin(t, cfg, store(t))
	host := r.wait(t, "hostkey").Challenge
	if !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+host.Fingerprint+",") {
		t.Fatal("unrecognized fixture host key")
	}
	r.session.Resolve(host.ID, "once")
	return r
}
func TestRealOpenSSHInteractivePAM(t *testing.T) {
	r := realFixture(t, "yaarenteractive")
	challenge := r.wait(t, "authentication").Authentication
	if len(challenge.Prompts) != 1 || challenge.Prompts[0].Echo {
		t.Fatal("PAM password prompt invalid")
	}
	r.session.AnswerAuthentication(challenge.ID, []string{"Yaare-OpenSSH-Test-Only!"})
	timeout := time.NewTimer(12 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case event := <-r.events:
			if event.Type == "connected" {
				return
			}
			if event.Type == "progress" {
				continue
			}
			if event.Type == "authentication" && len(event.Authentication.Prompts) == 0 {
				r.session.AnswerAuthentication(event.Authentication.ID, []string{})
				continue
			}
			t.Fatalf("PAM failed: %s %s", event.Type, event.Code)
		case <-timeout.C:
			t.Fatal("PAM authentication timed out")
		}
	}
}
func TestRealOpenSSHCancelledLargeTransferRetryAndPTY(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, closeClient, e := r.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	remote := filefs.Remote{Client: client}
	home, _ := client.Getwd()
	target := path.Join(home, "files", fmt.Sprintf("yaare-fault-%d", time.Now().UnixNano()))
	if e = client.Mkdir(target); e != nil {
		t.Fatal(e)
	}
	defer func() {
		budget := 500
		if err := filefs.RemoveTree(ctx, remote, target, &budget); err != nil {
			t.Errorf("isolated fixture cleanup failed: %v", err)
		}
		if closeClient != nil {
			closeClient()
		}
	}()
	localPath := t.TempDir()
	root, e := os.OpenRoot(localPath)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	local := filefs.Local{Root: root}
	data := []byte(strings.Repeat("Yaare large-file 中文🦂\n", 1024*1024))
	if e = os.WriteFile(filepath.Join(localPath, "large.bin"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(localPath, "folder"), 0700); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(localPath, "folder", "child.txt"), []byte("nested\n"), 0600)
	plan, total, e := filefs.Plan(ctx, local, "/", []string{"large.bin", "folder"})
	if e != nil {
		t.Fatal(e)
	}
	cancelled, stop := context.WithCancel(ctx)
	e = filefs.Copy(cancelled, local, remote, plan, target, "cancel", total, func(string, bool) (string, error) { return "overwrite", nil }, func(p filefs.Progress) {
		if p.Done > 1024*1024 {
			stop()
		}
	})
	if e == nil {
		t.Fatal("cancelled transfer reported success")
	}
	if _, e = client.Stat(path.Join(target, "large.bin")); !os.IsNotExist(e) {
		t.Fatal("incomplete transfer committed")
	}
	old, e := client.Create(path.Join(target, "large.bin"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = old.Write([]byte("original"))
	old.Close()
	if e != nil {
		t.Fatal(e)
	}
	broken := false
	e = filefs.Copy(ctx, local, remote, plan, target, "broken-channel", total, func(string, bool) (string, error) { return "overwrite", nil }, func(p filefs.Progress) {
		if !broken && p.Done > 1024*1024 {
			broken = true
			closeClient()
		}
	})
	if e == nil || !broken {
		t.Fatal("broken SFTP channel reported success")
	}
	client, closeClient, e = r.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	remote = filefs.Remote{Client: client}
	original, e := client.Open(path.Join(target, "large.bin"))
	if e != nil {
		t.Fatal(e)
	}
	bytes, e := io.ReadAll(original)
	original.Close()
	if e != nil || string(bytes) != "original" {
		t.Fatal("failed overwrite changed original", e)
	}
	marker := "__PTY_DURING_LARGE_UPLOAD__"
	sent := false
	e = filefs.Copy(ctx, local, remote, plan, target, "retry", total, func(string, bool) (string, error) { return "overwrite", nil }, func(p filefs.Progress) {
		if p.Done > 1024*1024 && !sent {
			sent = true
			if !r.session.Write([]byte("printf '" + marker + "\\n'\r")) {
				t.Error("PTY input blocked")
			}
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return strings.Contains(r.text(), marker) })
	handle, e := client.Open(path.Join(target, "large.bin"))
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.New()
	_, e = io.Copy(hash, handle)
	handle.Close()
	if e != nil || fmt.Sprintf("%x", hash.Sum(nil)) != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatal("retry hash mismatch", e)
	}
	if _, e = client.Stat(path.Join(target, "folder", "child.txt")); e != nil {
		t.Fatal("folder upload failed", e)
	}
	if e = client.Rename(path.Join(target, "folder"), path.Join(target, "renamed")); e != nil {
		t.Fatal(e)
	}
	if e = client.Mkdir(path.Join(target, "moved")); e != nil {
		t.Fatal(e)
	}
	if e = client.Rename(path.Join(target, "renamed", "child.txt"), path.Join(target, "moved", "child.txt")); e != nil {
		t.Fatal(e)
	}
}
