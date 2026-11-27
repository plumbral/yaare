package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/domain/workspace"
)

func TestRealRemoteTextSaveConflictAndIsolation(t *testing.T) {
	if os.Getenv("YAARE_TEXT_TEST") != "1" {
		t.Skip("requires isolated Docker OpenSSH and proxy fixtures")
	}
	for _, route := range []string{"direct", "two-hop", "socks5-two-hop"} {
		t.Run(route, func(t *testing.T) {
			cfg := routingConfig(22222, "text fixture")
			if route != "direct" {
				cfg = routingConfig(22224, "text target")
				cfg.Jumps = []Config{routingConfig(22222, "entry"), routingConfig(22223, "internal jump")}
			}
			var h *routingSession
			if route == "socks5-two-hop" {
				cfg.Jumps[0].Host = "yaare-test-openssh"
				cfg.Jumps[0].Port = 22
				cfg.Proxy = workspace.ProxyConfig{Kind: "socks5", Address: "127.0.0.1", Port: 23083, UseAuthentication: true, Username: "yaareproxy"}
				h = proxyTestRun(t, cfg, "")
			} else {
				h = startRoutingSession(t, cfg)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			client, closeClient, err := h.session.OpenFiles(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer closeClient()
			root := fmt.Sprintf("/home/yaaretest/files/.yaare-text-%d", time.Now().UnixNano())
			if err = client.Mkdir(root); err != nil {
				t.Fatal(err)
			}
			defer func() { client.Chmod(root+"/locked", 0700); client.RemoveAll(root); closeClient() }()
			write := func(p, value string) {
				t.Helper()
				out, e := client.Create(p)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = io.WriteString(out, value); e != nil {
					t.Fatal(e)
				}
				if e = out.Close(); e != nil {
					t.Fatal(e)
				}
			}
			read := func(p string) string {
				t.Helper()
				in, e := client.Open(p)
				if e != nil {
					t.Fatal(e)
				}
				defer in.Close()
				data, e := io.ReadAll(in)
				if e != nil {
					t.Fatal(e)
				}
				return string(data)
			}
			p := root + "/设置.yaml"
			write(p, "\ufeffname: 中文\r\nport: 22\r\n")
			client.Chmod(p, 0640)
			original, _ := client.Lstat(p)
			fs := filefs.Remote{Client: client}
			file, err := filefs.ReadText(ctx, fs, p)
			if err != nil || !file.BOM || file.LineEnding != "CRLF" {
				t.Fatal("read lost format", err)
			}
			result, err := filefs.SaveText(ctx, fs, p, file.Version, "name: 中文\nport: 23\n", "save")
			if err != nil || result.Status != "saved" {
				t.Fatal("save failed", err)
			}
			if read(p) != "\ufeffname: 中文\r\nport: 23\r\n" {
				t.Fatal("saved bytes differ")
			}
			after, _ := client.Lstat(p)
			if after.Mode().Perm() != 0640 {
				t.Fatal("permissions changed")
			}
			a, b := original.Sys().(*sftp.FileStat), after.Sys().(*sftp.FileStat)
			if a.UID != b.UID || a.GID != b.GID {
				t.Fatal("ownership changed")
			}
			file = result.File
			write(p, "\ufeffname: 外部\r\nport: 24\r\n")
			client.Chmod(p, 0640)
			client.Chtimes(p, after.ModTime(), after.ModTime())
			result, err = filefs.SaveText(ctx, fs, p, file.Version, "name: 本地\nport: 25\n", "conflict")
			if err != nil || result.Status != "conflict" || !strings.Contains(result.File.Content, "外部") {
				t.Fatal("same-size conflict was not detected", err)
			}
			approved := result.File.Version
			write(p, "\ufeffname: 再改\r\nport: 26\r\n")
			client.Chmod(p, 0640)
			result, err = filefs.SaveText(ctx, fs, p, approved, "name: 本地\nport: 25\n", "again")
			if err != nil || result.Status != "conflict" {
				t.Fatal("overwrite bypassed second conflict", err)
			}
			result, err = filefs.SaveText(ctx, fs, p, result.File.Version, "name: 本地\nport: 25\n", "confirmed")
			if err != nil || result.Status != "saved" {
				t.Fatal("confirmed overwrite failed", err)
			}
			write(root+"/binary", "abc\x00def")
			_, err = filefs.ReadText(ctx, fs, root+"/binary")
			var problem *filefs.TextProblem
			if !errors.As(err, &problem) || problem.Code != "TEXT_BINARY" {
				t.Fatal("binary accepted", err)
			}
			if err = client.Symlink(p, root+"/link"); err != nil {
				t.Fatal(err)
			}
			if _, err = filefs.ReadText(ctx, fs, root+"/link"); err == nil {
				t.Fatal("symlink accepted")
			}
			client.Mkdir(root + "/locked")
			write(root+"/locked/note.txt", "preserve")
			client.Chmod(root+"/locked", 0500)
			locked, err := filefs.ReadText(ctx, fs, root+"/locked/note.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = filefs.SaveText(ctx, fs, locked.Path, locked.Version, "replacement", "denied"); err == nil {
				t.Fatal("read-only directory save accepted")
			}
			if read(locked.Path) != "preserve" {
				t.Fatal("denied save damaged original")
			}
			client.Chmod(root+"/locked", 0700)
			entries, err := client.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".yaare-edit-") {
					t.Fatal("temporary file leaked")
				}
			}
			closeClient()
			if !h.session.Write([]byte("printf '__TEXT_EDITOR_TERMINAL_ALIVE__\\n'\r")) {
				t.Fatal("editor closed terminal")
			}
			eventually(t, func() bool { return strings.Contains(h.text(), "__TEXT_EDITOR_TERMINAL_ALIVE__") })
			client, closeClient, err = h.session.OpenFiles(ctx)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
