//go:build windows

package sshagent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh/agent"
	"testing"
	"time"
)

func TestWindowsNamedPipeAgentProbeAndUnavailable(t *testing.T) {
	pipe := fmt.Sprintf(`\\.\pipe\yaare-agent-test-%d`, time.Now().UnixNano())
	t.Setenv("SSH_AUTH_SOCK", pipe)
	listener, err := winio.ListenPipe(pipe, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	ring := agent.NewKeyring()
	ring.Add(agent.AddedKey{PrivateKey: key, Comment: `C:\private\identity-key`})
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); agent.ServeAgent(ring, c) }()
		}
	}()
	info := Probe(context.Background())
	if !info.Available || len(info.Keys) != 1 || info.Keys[0].Name != "identity-key" || info.Keys[0].Algorithm != "ssh-ed25519" {
		t.Fatalf("bad public metadata: %+v", info)
	}
	client, e := Open(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = client.Signers(); e != nil {
		t.Fatal(e)
	}
	client.Close()
	listener.Close()
	info = Probe(context.Background())
	if info.Available || len(info.Keys) != 0 {
		t.Fatal("missing agent claimed available")
	}
}
