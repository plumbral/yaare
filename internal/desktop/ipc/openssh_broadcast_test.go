package ipc

import (
	"bytes"
	"context"
	"fmt"

	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	sshclient "yaare/internal/adapters/ssh"
)

type realBroadcastRun struct {
	session *sshclient.Session
	mu      sync.Mutex
	output  bytes.Buffer
	events  chan sshclient.Event
}

func (r *realBroadcastRun) text() string { r.mu.Lock(); defer r.mu.Unlock(); return r.output.String() }
func realBroadcastSession(t *testing.T) *realBroadcastRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &realBroadcastRun{events: make(chan sshclient.Event, 96)}
	known := filepath.Join(t.TempDir(), "known_hosts")
	r.session = sshclient.NewSession(ctx, &sshclient.TrustStore{Path: func() (string, error) { return known, nil }}, func(event sshclient.Event) error {
		if event.Type == "output" {
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
	t.Cleanup(r.session.Close)
	cfg := sshclient.Config{Host: "127.0.0.1", Port: 22222, Username: "yaaretest", Authentication: "password", Password: []byte("Yaare-OpenSSH-Test-Only!"), Timeout: 10 * time.Second}
	go r.session.Run(&cfg)
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-r.events:
			switch event.Type {
			case "hostkey":
				if event.Challenge == nil || !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+event.Challenge.Fingerprint+",") {
					t.Fatal("unexpected isolated fixture fingerprint")
				}
				r.session.Resolve(event.Challenge.ID, "once")
			case "connected":
				return r
			case "failed":
				t.Fatal("fixture connection failed", event.Code)
			}
		case <-timer.C:
			t.Fatal("fixture connection timeout")
		}
	}
}
func waitBroadcastOutput(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("broadcast output not observed")
}
func TestRealOpenSSHServiceEightTargetBroadcastOnceAndPartialDisconnect(t *testing.T) {
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" {
		t.Skip("requires isolated Docker fixture")
	}
	if os.Getenv("YAARE_OPENSSH_FINGERPRINT") == "" {
		t.Fatal("verified fingerprint required")
	}
	s, shutdown := NewSSHService(NewKeyService())
	defer shutdown()
	page := &callerScope{}
	runs := make([]*realBroadcastRun, 8)
	writers := make([]broadcastWriter, 8)
	request := BroadcastRequest{ID: "real-eight", Command: "printf 'BROADCAST_%s\\n' 'DONE'", Execute: true}
	for i := range runs {
		r := realBroadcastSession(t)
		runs[i] = r
		r.session.Write([]byte(fmt.Sprintf("stty -echo; printf 'READY_%%s\\n' '%d'\n", i)))
		ready := fmt.Sprintf("READY_%d", i)
		waitBroadcastOutput(t, func() bool { return strings.Contains(r.text(), ready) })
		writers[i] = broadcastWriter{valid: r.session.Connected, write: r.session.Write}
		request.Targets = append(request.Targets, BroadcastTarget{SessionID: fmt.Sprintf("real-%d", i), Attempt: 1})
	}
	result, e := s.beginBroadcast(context.Background(), page, request, writers)
	if e != nil || result.Pending {
		t.Fatal("broadcast", e)
	}
	for i, item := range result.Items {
		if item.State != "sent" {
			t.Fatal("target not acknowledged", i, item.State)
		}
	}
	for _, r := range runs {
		waitBroadcastOutput(t, func() bool { return strings.Contains(r.text(), "BROADCAST_DONE") })
	}
	if _, e = s.beginBroadcast(context.Background(), page, request, writers); e != nil {
		t.Fatal(e)
	}
	time.Sleep(50 * time.Millisecond)
	for i, r := range runs {
		if strings.Count(r.text(), "BROADCAST_DONE") != 1 {
			t.Fatal("repeated command delivery", i)
		}
	}
	runs[3].session.Close()
	request.ID = "real-partial"
	request.Targets = []BroadcastTarget{{SessionID: "real-0", Attempt: 1}, {SessionID: "real-3", Attempt: 1}}
	request.Command = "printf 'AFTER_%s\\n' 'DONE'"
	result, e = s.beginBroadcast(context.Background(), page, request, []broadcastWriter{writers[0], writers[3]})
	if e != nil || result.Items[0].State != "sent" || result.Items[1].State != "not-sent" {
		t.Fatal("partial isolation", e)
	}
	waitBroadcastOutput(t, func() bool { return strings.Contains(runs[0].text(), "AFTER_DONE") })
}
