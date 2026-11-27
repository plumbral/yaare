package sshclient

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"yaare/internal/domain/workspace"
)

type routingSession struct {
	*runResult
	states    map[string]ForwardState
	addresses []string
	authCount int
}

func routingConfig(port int, name string) Config {
	return Config{Host: "127.0.0.1", Port: port, Name: name, Username: "yaaretest", Authentication: "password", Password: []byte("Yaare-OpenSSH-Test-Only!"), Timeout: 10 * time.Second}
}
func startRoutingSession(t *testing.T, cfg Config) *routingSession {
	t.Helper()
	if os.Getenv("YAARE_SSH_ROUTING_TEST") != "1" {
		t.Skip("requires isolated Docker routing fixture")
	}
	h := &routingSession{runResult: begin(t, cfg, store(t)), states: map[string]ForwardState{}}
	for {
		event := h.next(t)
		switch event.Type {
		case "hostkey":
			ch := event.Challenge
			if !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+ch.Fingerprint+",") {
				t.Fatal("unexpected fixture host key", ch.Address)
			}
			h.addresses = append(h.addresses, ch.Address)
			if !h.session.Resolve(ch.ID, "once") {
				t.Fatal("trust resolution failed")
			}
		case "authentication":
			h.authCount++
			if event.Authentication.Address == "" {
				t.Fatal("missing authentication endpoint")
			}
			answers := make([]string, len(event.Authentication.Prompts))
			for i := range answers {
				answers[i] = "Yaare-OpenSSH-Test-Only!"
			}
			if !h.session.AnswerAuthentication(event.Authentication.ID, answers) {
				t.Fatal("hop credentials rejected")
			}
		case "connected":
			return h
		case "failed", "disconnected":
			t.Fatalf("chain failed: %s %s", event.Code, event.Message)
		}
	}
}
func (h *routingSession) next(t *testing.T) Event {
	t.Helper()
	select {
	case event := <-h.events:
		if event.Forward != nil {
			h.states[event.Forward.Rule.ID] = *event.Forward
		}
		return event
	case <-time.After(20 * time.Second):
		t.Fatal("SSH routing event timed out")
		return Event{}
	}
}
func (h *routingSession) waitForward(t *testing.T, id, state string) {
	t.Helper()
	for {
		if h.states[id].State == state {
			return
		}
		e := h.next(t)
		if e.Type == "failed" || e.Type == "disconnected" {
			t.Fatal("SSH closed during forwarding", e.Code)
		}
	}
}
func freePort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}
func rule(id, kind string, listen, target int) workspace.ForwardRule {
	return workspace.ForwardRule{ID: id, Name: id, Kind: kind, ListenAddress: "127.0.0.1", ListenPort: listen, TargetAddress: "127.0.0.1", TargetPort: target, Enabled: true}
}

func TestRealSSHRoutingMultiHopPTYAndSFTP(t *testing.T) {
	if os.Getenv("YAARE_SSH_ROUTING_TEST") != "1" {
		t.Skip("requires Docker")
	}
	cfg := routingConfig(22224, "internal target")
	hop := routingConfig(22223, "second jump")
	hop.Password = nil
	cfg.Jumps = []Config{routingConfig(22222, "first jump"), hop}
	h := startRoutingSession(t, cfg)
	if len(h.addresses) != 3 || h.authCount != 1 {
		t.Fatal("each endpoint was not verified/authenticated", h.addresses, h.authCount)
	}
	if !h.session.Write([]byte("printf '__SSH_TWO_HOPS__\\n'\r")) {
		t.Fatal("PTY write failed")
	}
	eventually(t, func() bool { return strings.Contains(h.text(), "__SSH_TWO_HOPS__") })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	files, closeFiles, e := h.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	f, e := files.Open("/home/yaaretest/files/hello.txt")
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(f)
	f.Close()
	closeFiles()
	if e != nil || !strings.Contains(string(b), "中文") {
		t.Fatal("SFTP did not reach final host", e)
	}
	if !h.session.Write([]byte("printf '__SSH_AFTER_SFTP__\\n'\r")) {
		t.Fatal("SFTP close killed chain")
	}
	eventually(t, func() bool { return strings.Contains(h.text(), "__SSH_AFTER_SFTP__") })
	h.session.Close()
	h.ended(t)
}

func TestRealSSHRoutingThreeForwardTypesStopRetryAndIsolation(t *testing.T) {
	if os.Getenv("YAARE_SSH_ROUTING_TEST") != "1" {
		t.Skip("requires Docker")
	}
	echo, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	occupied, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer occupied.Close()
	cfg := routingConfig(22224, "forward target")
	cfg.Jumps = []Config{routingConfig(22222, "jump")}
	local, dynamic, retry := freePort(t), freePort(t), occupied.Addr().(*net.TCPAddr).Port
	remote := freePort(t)
	cfg.Forwards = []workspace.ForwardRule{rule("local", "local", local, 22223), rule("remote", "remote", remote, echo.Addr().(*net.TCPAddr).Port), rule("dynamic", "dynamic", dynamic, 0), rule("retry", "local", retry, 22223)}
	cfg.Forwards[2].TargetAddress = ""
	h := startRoutingSession(t, cfg)
	for _, id := range []string{"local", "remote", "dynamic"} {
		h.waitForward(t, id, "running")
	}
	h.waitForward(t, "retry", "failed")
	readBanner := func(conn net.Conn) {
		t.Helper()
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		b := make([]byte, 12)
		if _, err := io.ReadFull(conn, b); err != nil || !strings.HasPrefix(string(b), "SSH-2.0-") {
			t.Fatal("forward did not reach real SSH service", string(b), err)
		}
	}
	c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local)), 5*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	readBanner(c)
	socks, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(dynamic)), 5*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	_ = socks.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = socks.Write([]byte{5, 1, 0})
	var greeting [2]byte
	if _, e = io.ReadFull(socks, greeting[:]); e != nil || greeting != [2]byte{5, 0} {
		t.Fatal("SOCKS negotiation failed", e)
	}
	host := []byte("localhost")
	packet := append([]byte{5, 1, 0, 3, byte(len(host))}, host...)
	var port [2]byte
	binary.BigEndian.PutUint16(port[:], 22223)
	packet = append(packet, port[:]...)
	_, _ = socks.Write(packet)
	reply := make([]byte, 10)
	if _, e = io.ReadFull(socks, reply); e != nil || reply[1] != 0 {
		t.Fatal("SOCKS CONNECT failed", e, reply)
	}
	readBanner(socks)
	h.session.mu.Lock()
	client := h.session.client
	h.session.mu.Unlock()
	remoteConn, e := client.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(remote)))
	if e != nil {
		t.Fatal(e)
	}
	_ = remoteConn.SetDeadline(time.Now().Add(5 * time.Second))
	payload := []byte("SSH 真实远程转发 🦂")
	_, _ = remoteConn.Write(payload)
	got := make([]byte, len(payload))
	if _, e = io.ReadFull(remoteConn, got); e != nil || string(got) != string(payload) {
		t.Fatal("remote bytes differ", e)
	}
	remoteConn.Close()
	occupied.Close()
	if !h.session.SetForward("retry", true) {
		t.Fatal("retry rejected")
	}
	h.waitForward(t, "retry", "running")
	if !h.session.SetForward("local", false) {
		t.Fatal("stop rejected")
	}
	h.waitForward(t, "local", "stopped")
	if l, e := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local))); e != nil {
		t.Fatal("stopped local listener leaked", e)
	} else {
		l.Close()
	}
	if !h.session.Write([]byte("printf '__SSH_FORWARD_ISOLATION__\\n'\r")) {
		t.Fatal("rule failure killed terminal")
	}
	eventually(t, func() bool { return strings.Contains(h.text(), "__SSH_FORWARD_ISOLATION__") })
	if h.session.SetForward("injected-rule", true) {
		t.Fatal("unknown listener injection accepted")
	}
	h.session.Close()
	h.ended(t)
	for _, p := range []int{local, dynamic, retry} {
		l, e := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if e != nil {
			t.Fatal("session listener leaked", p, e)
		}
		l.Close()
	}
}

func TestRealSSHRoutingRejectSecondHopTrustCancelsChain(t *testing.T) {
	if os.Getenv("YAARE_SSH_ROUTING_TEST") != "1" {
		t.Skip("requires Docker")
	}
	cfg := routingConfig(22223, "target")
	cfg.Jumps = []Config{routingConfig(22222, "jump")}
	h := &routingSession{runResult: begin(t, cfg, store(t)), states: map[string]ForwardState{}}
	count := 0
	for {
		e := h.next(t)
		if e.Type == "hostkey" {
			count++
			decision := "once"
			if count == 2 {
				decision = "reject"
			}
			if !h.session.Resolve(e.Challenge.ID, decision) {
				t.Fatal("trust response rejected")
			}
		}
		if e.Type == "connected" {
			t.Fatal("rejected target connected")
		}
		if e.Type == "failed" {
			if e.Code != "HOST_KEY_REJECTED" {
				t.Fatal("wrong refusal", e.Code)
			}
			break
		}
	}
	h.ended(t)
}
