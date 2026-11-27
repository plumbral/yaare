package sshclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"testing"
	"time"
)

func TestRealOpenSSHFullScreenApplications(t *testing.T) {
	for _, app := range []string{"vim", "nano", "top", "htop", "tmux"} {
		t.Run(app, func(t *testing.T) {
			r := realFixture(t, "yaaretest")
			r.wait(t, "connected")
			t.Cleanup(func() {
				if dir := os.Getenv("YAARE_PTY_CAPTURE_DIR"); dir != "" {
					_ = os.MkdirAll(dir, 0700)
					_ = os.WriteFile(path.Join(dir, app+".bin"), []byte(r.text()), 0600)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			client, closeFiles, err := r.session.OpenFiles(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer closeFiles()
			name := path.Join("/home/yaaretest/files", fmt.Sprintf("app-%s-%d.txt", app, time.Now().UnixNano()))
			defer client.Remove(name)
			command := map[string]string{"vim": "vim -Nu NONE -n " + name, "nano": "nano -I " + name, "top": "top -d 1", "htop": "htop -d 10", "tmux": "tmux -L yaare-" + app + "-" + fmt.Sprint(time.Now().UnixNano()) + " new-session"}[app]
			baseline := len(r.text())
			if !r.session.Write([]byte("export LANG=C.UTF-8 TERM=xterm-256color; " + command + "\r")) {
				t.Fatal("launch rejected")
			}
			eventually(t, func() bool {
				out := r.text()[baseline:]
				switch app {
				case "nano":
					return strings.Contains(out, "GNU nano")
				case "vim":
					return strings.Contains(out, "\x1b[?1049h")
				case "top":
					return strings.Contains(out, "Tasks:")
				case "htop":
					return strings.Contains(out, "\x1b[?1049h")
				default:
					return strings.Contains(out, "\x1b[?1049h")
				}
			})
			if !r.session.Resize(105, 32) {
				t.Fatal("fullscreen resize rejected")
			}
			resizeDeadline := time.Now().Add(2 * time.Second)
			stableSince := time.Now()
			lastLen := len(r.text())
			for time.Now().Before(resizeDeadline) && time.Since(stableSince) < 40*time.Millisecond {
				time.Sleep(5 * time.Millisecond)
				n := len(r.text())
				if n != lastLen {
					lastLen = n
					stableSince = time.Now()
				}
			}
			text := "OpenSSH 中文 🦂 UTF-8"
			finishStart := len(r.text())
			switch app {
			case "vim":
				r.session.Write([]byte("i" + text + "\x1b:wq\r"))
			case "nano":
				r.session.Write([]byte(text + "\x0f"))
				eventually(t, func() bool { return strings.Contains(r.text()[baseline:], "File Name to Write") })
				r.session.Write([]byte("\r\x18"))
			case "top", "htop":
				r.session.Write([]byte("q"))
			case "tmux":
				r.session.Write([]byte("exit\r"))
			}
			eventually(t, func() bool { return strings.Contains(r.text()[finishStart:], "$ ") })
			marker := "__APP_FINISHED_" + app + "__"
			r.session.Write([]byte("printf '__APP_FINISHED_%s__\\n' " + app + "\r"))
			eventually(t, func() bool { return strings.Contains(r.text(), marker) })
			if app == "vim" || app == "nano" {
				f, e := client.Open(name)
				if e != nil {
					t.Fatal(e)
				}
				bytes, e := io.ReadAll(f)
				f.Close()
				if e != nil || !strings.Contains(string(bytes), text) {
					t.Fatalf("UTF-8 editing mismatch %q: %v", string(bytes), e)
				}
			}
			if dir := os.Getenv("YAARE_PTY_CAPTURE_DIR"); dir != "" {
				if e := os.MkdirAll(dir, 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(path.Join(dir, app+".bin"), []byte(r.text()), 0600); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
