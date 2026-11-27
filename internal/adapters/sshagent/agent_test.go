package sshagent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"golang.org/x/crypto/ssh/agent"
	"net"
	"testing"
	"time"
)

func TestAgentSignsWithoutExportAndCancels(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ring := agent.NewKeyring()
	if err = ring.Add(agent.AddedKey{PrivateKey: private, Comment: "test"}); err != nil {
		t.Fatal(err)
	}
	local, remote := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); defer remote.Close(); _ = agent.ServeAgent(ring, remote) }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newClient(ctx, local)
	defer client.Close()
	signers, err := client.Signers()
	if err != nil || len(signers) != 1 {
		t.Fatal(err)
	}
	signature, err := signers[0].Sign(rand.Reader, []byte("challenge"))
	if err != nil {
		t.Fatal(err)
	}
	if err = signers[0].PublicKey().Verify([]byte("challenge"), signature); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not close agent")
	}
}
func TestEmptyAgent(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()
	go agent.ServeAgent(agent.NewKeyring(), remote)
	c := newClient(context.Background(), local)
	defer c.Close()
	if _, err := c.Signers(); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
}
