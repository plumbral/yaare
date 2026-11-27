//go:build windows

package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"os"
	"strings"
	"testing"
	"time"
)

func testAgent(t *testing.T, ring agent.Agent) {
	t.Helper()
	pipe := fmt.Sprintf(`\\.\pipe\yaare-ssh-test-%d`, time.Now().UnixNano())
	t.Setenv("SSH_AUTH_SOCK", pipe)
	l, err := winio.ListenPipe(pipe, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); agent.ServeAgent(ring, c) }()
		}
	}()
}
func TestAgentHandshakeEd25519RSAAndEmpty(t *testing.T) {
	for _, algorithm := range []string{"ed25519", "rsa"} {
		t.Run(algorithm, func(t *testing.T) {
			var private any
			if algorithm == "rsa" {
				k, e := rsa.GenerateKey(rand.Reader, 2048)
				if e != nil {
					t.Fatal(e)
				}
				private = k
			} else {
				_, k, e := ed25519.GenerateKey(rand.Reader)
				if e != nil {
					t.Fatal(e)
				}
				private = k
			}
			signer, e := ssh.NewSignerFromKey(private)
			if e != nil {
				t.Fatal(e)
			}
			ring := agent.NewKeyring()
			if e = ring.Add(agent.AddedKey{PrivateKey: private}); e != nil {
				t.Fatal(e)
			}
			testAgent(t, ring)
			server := fixture(t)
			server.Authorize(signer.PublicKey())
			cfg := config(server.Address())
			cfg.Authentication = "key"
			cfg.Password = nil
			cfg.UseAgent = true
			r := begin(t, cfg, store(t))
			ch := r.wait(t, "hostkey").Challenge
			r.session.Resolve(ch.ID, "once")
			r.wait(t, "connected")
			if !r.session.Write([]byte("agent-input\r")) {
				t.Fatal("input rejected")
			}
			eventually(t, func() bool { return strings.Contains(r.text(), "received: agent-input") })
			r.session.Close()
			r.ended(t)
		})
	}
	t.Run("empty", func(t *testing.T) {
		testAgent(t, agent.NewKeyring())
		server := fixture(t)
		cfg := config(server.Address())
		cfg.Authentication = "key"
		cfg.Password = nil
		cfg.UseAgent = true
		r := begin(t, cfg, store(t))
		ch := r.wait(t, "hostkey").Challenge
		r.session.Resolve(ch.ID, "once")
		if e := r.wait(t, "failed"); e.Code != "AGENT_EMPTY" {
			t.Fatal(e.Code, e.Message)
		}
		r.ended(t)
	})
	t.Run("missing", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", `\\.\pipe\yaare-does-not-exist`)
		server := fixture(t)
		cfg := config(server.Address())
		cfg.Authentication = "key"
		cfg.Password = nil
		cfg.UseAgent = true
		r := begin(t, cfg, store(t))
		ch := r.wait(t, "hostkey").Challenge
		r.session.Resolve(ch.ID, "once")
		if e := r.wait(t, "failed"); e.Code != "AGENT_UNAVAILABLE" {
			t.Fatal(e.Code, e.Message)
		}
		r.ended(t)
	})
}

func TestRealOpenSSHAgentDirectAndJump(t *testing.T) {
	if os.Getenv("YAARE_AGENT_TEST") != "1" {
		t.Skip("isolated Docker fixture only")
	}
	data, err := os.ReadFile(os.Getenv("YAARE_AGENT_TEST_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	private, err := ssh.ParseRawPrivateKey(data)
	clear(data)
	if err != nil {
		t.Fatal(err)
	}
	ring := agent.NewKeyring()
	if err = ring.Add(agent.AddedKey{PrivateKey: private}); err != nil {
		t.Fatal(err)
	}
	testAgent(t, ring)
	for _, jump := range []bool{false, true} {
		t.Run(fmt.Sprintf("jump-%t", jump), func(t *testing.T) {
			cfg := routingConfig(22222, "Agent fixture")
			cfg.Authentication = "key"
			cfg.Password = nil
			cfg.UseAgent = true
			if jump {
				hop := cfg
				cfg.Port = 22
				cfg.Jumps = []Config{hop}
			}
			h := startRoutingSession(t, cfg)
			if h.authCount != 0 {
				t.Fatal("Agent unexpectedly asked for password")
			}
			if !h.session.Write([]byte("printf '__AGENT_%s__\\n' 'PTY'\r")) {
				t.Fatal("PTY rejected")
			}
			eventually(t, func() bool { return strings.Contains(h.text(), "__AGENT_PTY__") })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, closeFiles, e := h.session.OpenFiles(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer closeFiles()
			if _, e = client.Stat("/home/yaaretest/files/hello.txt"); e != nil {
				t.Fatal(e)
			}
		})
	}
}
