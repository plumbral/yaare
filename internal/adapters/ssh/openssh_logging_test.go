package sshclient

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"yaare/internal/adapters/sessionlog"
)

func TestRealOpenSSHExplicitOutputLogging(t *testing.T) {
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" {
		t.Skip("isolated Docker fixture only")
	}
	fingerprints := os.Getenv("YAARE_OPENSSH_FINGERPRINT")
	if fingerprints == "" {
		t.Fatal("fixture fingerprint required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var recording atomic.Pointer[sessionlog.Recording]
	r := &runResult{events: make(chan Event, 64), done: make(chan struct{}), ctx: ctx}
	r.session = NewSession(ctx, store(t), func(event Event) error {
		if event.Type == "output" {
			if rec := recording.Load(); rec != nil {
				rec.Capture(event.Data)
			}
			r.mu.Lock()
			r.output.Write(event.Data)
			r.mu.Unlock()
			return nil
		}
		select {
		case r.events <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	cfg := config("127.0.0.1:22222")
	cfg.Username = "yaaretest"
	cfg.Password = []byte("Yaare-OpenSSH-Test-Only!")
	cfg.Timeout = 10 * time.Second
	go func() { r.session.Run(&cfg); close(r.done) }()
	t.Cleanup(r.session.Close)
	challenge := r.wait(t, "hostkey").Challenge
	if !strings.Contains(","+fingerprints+",", ","+challenge.Fingerprint+",") {
		t.Fatal("unexpected fixture")
	}
	if !r.session.Resolve(challenge.ID, "once") {
		t.Fatal("trust")
	}
	r.wait(t, "connected")
	r.session.Write([]byte("stty -echo; printf 'LOG_READY_%s\\n' 'EXPLICIT'\n"))
	eventually(t, func() bool { return strings.Contains(r.text(), "LOG_READY_EXPLICIT") })
	root := t.TempDir()
	logs := sessionlog.New(func() (string, error) { return root, nil })
	if err := logs.Initialize(); err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	rec, err := logs.Start("real-pane", 1, "Docker fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	recording.Store(rec)
	r.session.Write([]byte("printf '\\033[31mRECORDED_%s 中文🦂\\033[0m\\n' 'REMOTE'\n"))
	eventually(t, func() bool { return strings.Contains(r.text(), "RECORDED_REMOTE 中文🦂") })
	recording.Store(nil)
	if _, err = rec.Stop(ctx, ""); err != nil {
		t.Fatal(err)
	}
	page, err := logs.Read(ctx, sessionlog.Query{ID: rec.Info().ID})
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, line := range page.Lines {
		body.WriteString(line.Text)
	}
	if !strings.Contains(body.String(), "RECORDED_REMOTE 中文🦂") || strings.Contains(body.String(), "printf") || strings.Contains(body.String(), "LOG_READY") || strings.ContainsRune(body.String(), 27) {
		t.Fatalf("incorrect transcript: %q", body.String())
	}
	r.session.Write([]byte("printf 'AFTER_STOP_%s\\n' 'CONNECTED'\n"))
	eventually(t, func() bool { return strings.Contains(r.text(), "AFTER_STOP_CONNECTED") })
	files, closeFiles, err := r.session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = files.ReadDir("/home/yaaretest/files"); err != nil {
		t.Fatal(err)
	}
	closeFiles()
	page, err = logs.Read(ctx, sessionlog.Query{ID: rec.Info().ID, Text: "AFTER_STOP"})
	if err != nil || len(page.Lines) != 0 {
		t.Fatal("recording continued after stop", err)
	}
	r.session.Close()
	r.ended(t)
}
