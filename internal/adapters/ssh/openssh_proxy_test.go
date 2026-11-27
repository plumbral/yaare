package sshclient

import (
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"yaare/internal/domain/workspace"
)

func proxyTestRun(t *testing.T, cfg Config, wantFailure string) *routingSession {
	t.Helper()
	if os.Getenv("YAARE_PROXY_TEST") != "1" {
		t.Skip("requires isolated Docker Dante/Squid fixture")
	}
	h := &routingSession{runResult: begin(t, cfg, store(t)), states: map[string]ForwardState{}}
	for {
		event := h.next(t)
		switch event.Type {
		case "hostkey":
			if wantFailure != "" {
				t.Fatal("proxy failed to reject before SSH handshake")
			}
			ch := event.Challenge
			if !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+ch.Fingerprint+",") {
				t.Fatal("unexpected SSH fixture key", ch.Address)
			}
			h.addresses = append(h.addresses, ch.Address)
			if !h.session.Resolve(ch.ID, "once") {
				t.Fatal("host trust response failed")
			}
		case "authentication":
			h.authCount++
			challenge := event.Authentication
			secret := "Yaare-OpenSSH-Test-Only!"
			if challenge.Kind == "proxy" {
				secret = "Yaare-Proxy-Test-Only!"
			}
			values := make([]string, len(challenge.Prompts))
			for i := range values {
				values[i] = secret
			}
			if !h.session.AnswerAuthentication(challenge.ID, values) {
				t.Fatal("proxy response rejected")
			}
			if h.session.AnswerAuthentication(challenge.ID, values) {
				t.Fatal("proxy secret replay accepted")
			}
			clear(values)
		case "connected":
			if wantFailure != "" {
				t.Fatal("proxy rejection silently connected")
			}
			return h
		case "failed":
			if event.Code != wantFailure {
				t.Fatalf("proxy run failed: %s %s (wanted %s)", event.Code, event.Message, wantFailure)
			}
			h.ended(t)
			return h
		case "disconnected":
			t.Fatal("unexpected disconnect", event.Code, event.Message)
		}
	}
}

func TestRealProxyDirectAndTwoHopSSHWithDanteAndSquid(t *testing.T) {
	for _, test := range []struct {
		kind string
		port int
		auth bool
	}{{"http", 23080, false}, {"http", 23081, true}, {"socks5", 23082, false}, {"socks5", 23083, true}} {
		for _, jumps := range []bool{false, true} {
			name := test.kind + "-" + strconv.Itoa(test.port)
			if jumps {
				name += "-two-hop"
			}
			t.Run(name, func(t *testing.T) {
				cfg := routingConfig(22, "SSH target")
				cfg.Host = "yaare-test-openssh"
				cfg.Proxy = workspace.ProxyConfig{Kind: test.kind, Address: "127.0.0.1", Port: test.port, UseAuthentication: test.auth}
				if test.auth {
					cfg.Proxy.Username = "yaareproxy"
				}
				if jumps {
					entry := routingConfig(22, "entry jump")
					entry.Host = "yaare-test-openssh"
					cfg.Jumps = []Config{entry, routingConfig(22223, "inner jump")}
					cfg.Host = "127.0.0.1"
					cfg.Port = 22224
				}
				local := freePort(t)
				cfg.Forwards = []workspace.ForwardRule{rule("proxy-local", "local", local, 22223)}
				h := proxyTestRun(t, cfg, "")
				count := 1
				if jumps {
					count = 3
				}
				if len(h.addresses) != count {
					t.Fatal("not all SSH endpoints verified", h.addresses)
				}
				if test.auth && h.authCount != 1 {
					t.Fatal("proxy password did not use one-use runtime prompt", h.authCount)
				}
				if !h.session.Write([]byte("printf '__PROXY_PTY__\\n'\r")) {
					t.Fatal("PTY unavailable through proxy")
				}
				eventually(t, func() bool { return strings.Contains(h.text(), "__PROXY_PTY__") })
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
					t.Fatal("SFTP bytes differ", e)
				}
				h.waitForward(t, "proxy-local", "running")
				c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local)), 3*time.Second)
				if e != nil {
					t.Fatal(e)
				}
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				banner := make([]byte, 8)
				_, e = io.ReadFull(c, banner)
				c.Close()
				if e != nil || string(banner) != "SSH-2.0-" {
					t.Fatal("forward through proxy unavailable", e)
				}
				h.session.Close()
				h.ended(t)
				l, e := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local)))
				if e != nil {
					t.Fatal("proxy session leaked listener", e)
				}
				l.Close()
			})
		}
	}
}

func TestRealProxyWrongCredentialsAndAccessPolicy(t *testing.T) {
	for _, tc := range []struct {
		kind string
		port int
	}{{"http", 23081}, {"socks5", 23083}} {
		t.Run(tc.kind, func(t *testing.T) {
			cfg := routingConfig(22, "target")
			cfg.Host = "yaare-test-openssh"
			cfg.Proxy = workspace.ProxyConfig{Kind: tc.kind, Address: "127.0.0.1", Port: tc.port, Username: "yaareproxy", UseAuthentication: true}
			cfg.ProxyPassword = []byte("wrong-synthetic-password")
			proxyTestRun(t, cfg, "PROXY_AUTH_FAILED")
		})
	}
	t.Run("HTTP-policy", func(t *testing.T) {
		cfg := routingConfig(23, "blocked")
		cfg.Host = "yaare-test-openssh"
		cfg.Proxy = workspace.ProxyConfig{Kind: "http", Address: "127.0.0.1", Port: 23080}
		proxyTestRun(t, cfg, "PROXY_DENIED")
	})
	t.Run("SOCKS-policy", func(t *testing.T) {
		cfg := routingConfig(23, "blocked")
		cfg.Host = "yaare-test-openssh"
		cfg.Proxy = workspace.ProxyConfig{Kind: "socks5", Address: "127.0.0.1", Port: 23082}
		proxyTestRun(t, cfg, "PROXY_DENIED")
	})
}
