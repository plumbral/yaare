package sshclient

import (
	"context"
	"net"
	"os"
	"yaare/internal/adapters/server"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealOpenSSHX11CookieForwardAndReuse(t *testing.T) {
	if os.Getenv("YAARE_X11_TEST") != "1" {
		t.Skip("isolated Xvfb and OpenSSH xauth opt-in")
	}
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var attached atomic.Int32
	options := X11Options{Address: "127.0.0.1:6000", Command: "xev -name Yaare-X11-Acceptance -event structure >/dev/null 2>&1 & sleep 0.2", Notify: func(kind string) {
		if kind == "attached" {
			attached.Add(1)
		}
	}, localDial: func(ctx context.Context, _ string) (net.Conn, error) {
		return r.session.OpenTCP(ctx, "yaare-display-fixture:6099")
	}}
	forward, e := r.session.StartX11(ctx, options)
	if e != nil {
		t.Fatal(e)
	}
	defer forward.Close()
	eventually(t, func() bool { return attached.Load() > 0 })
	eventually(t, func() bool { forward.mu.Lock(); defer forward.mu.Unlock(); return forward.finished })
	select {
	case <-forward.Done():
		t.Fatal("launcher exit closed an existing X client")
	default:
	}
	visible := false
	for i := 0; i < 10; i++ {
		raw, e := server.Run(ctx, r.session.OpenCommand, "DISPLAY=yaare-display-fixture:99 xwininfo -root -tree", 64<<10)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(raw), "Yaare-X11-Acceptance") {
			visible = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !visible {
		t.Fatal("forwarded X client did not create its own fixture window")
	}
	forward.Close()
	options.Command = "xdpyinfo >/dev/null"
	second, e := r.session.StartX11(ctx, options)
	if e != nil {
		t.Fatal("reuse", e)
	}
	defer second.Close()
	select {
	case <-second.Done():
	case <-ctx.Done():
		t.Fatal("X11 command did not end")
	}
	if e = second.Err(); e != nil {
		t.Fatal(e)
	}
	second.Close()
	if attached.Load() < 2 {
		t.Fatal("second X client did not authenticate")
	}
	if !r.session.Write([]byte("printf 'x11-%s\\n' terminal-ok\r")) {
		t.Fatal("PTY unavailable")
	}
	eventually(t, func() bool { return strings.Contains(r.text(), "x11-terminal-ok") })
	t.Log("real OpenSSH fake-cookie X11 to isolated Xvfb, actual X client window, stop/reuse and PTY passed; Windows X server checked separately")
}
