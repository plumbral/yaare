package sshclient

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	display "yaare/internal/adapters/desktop"
	"strings"
	"testing"
	"time"
)

func TestRealOpenSSHRemoteDesktopProtocols(t *testing.T) {
	if os.Getenv("YAARE_DESKTOP_TEST") != "1" {
		t.Skip("isolated Xvfb/x11vnc/xrdp opt-in")
	}
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, e := r.session.OpenTCP(ctx, "yaare-display-fixture:5900")
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	watch := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer watch()
	banner := make([]byte, 12)
	if _, e = io.ReadFull(conn, banner); e != nil || string(banner) != "RFB 003.008\n" {
		t.Fatal("VNC version", e)
	}
	if _, e = conn.Write(banner); e != nil {
		t.Fatal(e)
	}
	count := []byte{0}
	if _, e = io.ReadFull(conn, count); e != nil || count[0] == 0 {
		t.Fatal("VNC security", e)
	}
	security := make([]byte, count[0])
	if _, e = io.ReadFull(conn, security); e != nil {
		t.Fatal(e)
	}
	none := false
	for _, kind := range security {
		if kind == 1 {
			none = true
		}
	}
	if !none {
		t.Fatal("fixture should be SSH-private None authentication")
	}
	if _, e = conn.Write([]byte{1}); e != nil {
		t.Fatal(e)
	}
	status := make([]byte, 4)
	if _, e = io.ReadFull(conn, status); e != nil || binary.BigEndian.Uint32(status) != 0 {
		t.Fatal("VNC security result", e)
	}
	if _, e = conn.Write([]byte{1}); e != nil {
		t.Fatal(e)
	}
	init := make([]byte, 24)
	if _, e = io.ReadFull(conn, init); e != nil {
		t.Fatal(e)
	}
	width, height := binary.BigEndian.Uint16(init), binary.BigEndian.Uint16(init[2:])
	if width != 1280 || height != 720 || init[4] != 32 {
		t.Fatal("framebuffer geometry", width, height, init[4])
	}
	nameSize := binary.BigEndian.Uint32(init[20:])
	if nameSize > 1024 {
		t.Fatal("desktop name limit")
	}
	if _, e = io.CopyN(io.Discard, conn, int64(nameSize)); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Write([]byte{3, 0, 0, 0, 0, 0, 0, 64, 0, 64}); e != nil {
		t.Fatal(e)
	}
	message := []byte{255}
	if _, e = io.ReadFull(conn, message); e != nil || message[0] != 0 {
		t.Fatal("framebuffer update", message, e)
	}
	head := make([]byte, 3)
	if _, e = io.ReadFull(conn, head); e != nil || binary.BigEndian.Uint16(head[1:]) < 1 {
		t.Fatal("rectangle count", e)
	}
	rectangle := make([]byte, 12)
	if _, e = io.ReadFull(conn, rectangle); e != nil || binary.BigEndian.Uint32(rectangle[8:]) != 0 {
		t.Fatal("raw rectangle", e)
	}
	pixels := int64(binary.BigEndian.Uint16(rectangle[4:])) * int64(binary.BigEndian.Uint16(rectangle[6:])) * 4
	if pixels < 4 || pixels > 64*64*4 {
		t.Fatal("pixel budget", pixels)
	}
	if _, e = io.CopyN(io.Discard, conn, pixels); e != nil {
		t.Fatal(e)
	}
	conn.Close()
	tunnel, e := display.StartRDP(ctx, r.session.OpenTCP, "yaare-display-fixture:3389")
	if e != nil {
		t.Fatal(e)
	}
	defer tunnel.Close()
	rdp, e := net.DialTimeout("tcp", tunnel.Address(), 2*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer rdp.Close()
	_ = rdp.SetDeadline(time.Now().Add(5 * time.Second))
	request := []byte{3, 0, 0, 19, 14, 0xe0, 0, 0, 0, 0, 0, 1, 0, 8, 0, 3, 0, 0, 0}
	if _, e = rdp.Write(request); e != nil {
		t.Fatal(e)
	}
	tpkt := make([]byte, 4)
	if _, e = io.ReadFull(rdp, tpkt); e != nil || tpkt[0] != 3 {
		t.Fatal("RDP response", e)
	}
	size := int(binary.BigEndian.Uint16(tpkt[2:]))
	if size < 11 || size > 4096 {
		t.Fatal("RDP packet size", size)
	}
	body := make([]byte, size-4)
	if _, e = io.ReadFull(rdp, body); e != nil || body[1] != 0xd0 {
		t.Fatal("RDP X.224 confirm", e)
	}
	tunnel.Close()
	if !r.session.Write([]byte("printf 'desktop-%s\\n' terminal-ok\r")) {
		t.Fatal("PTY unavailable")
	}
	eventually(t, func() bool { return strings.Contains(r.text(), "desktop-terminal-ok") })
	t.Logf("real SSH VNC 3.8 %dx%d/raw frame and loopback RDP X.224 negotiation passed; native GUI checked separately", width, height)
}
