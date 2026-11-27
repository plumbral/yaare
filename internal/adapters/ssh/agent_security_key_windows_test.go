//go:build windows

package sshclient

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"io"
	"strings"
	"testing"
)

type securityKeySigner struct {
	public  ssh.PublicKey
	private ed25519.PrivateKey
}

func (s securityKeySigner) PublicKey() ssh.PublicKey { return s.public }
func (s securityKeySigner) Sign(_ io.Reader, data []byte) (*ssh.Signature, error) {
	application := sha256.Sum256([]byte("ssh:"))
	message := sha256.Sum256(data)
	signed := ssh.Marshal(struct {
		App     []byte `ssh:"rest"`
		Flags   byte
		Counter uint32
		Message []byte `ssh:"rest"`
	}{application[:], 1, 1, message[:]})
	return &ssh.Signature{Format: ssh.KeyAlgoSKED25519, Blob: ed25519.Sign(s.private, signed), Rest: ssh.Marshal(struct {
		Flags   byte
		Counter uint32
	}{1, 1})}, nil
}

type securityKeyAgent struct {
	agent.Agent
	signer securityKeySigner
}

func (a securityKeyAgent) List() ([]*agent.Key, error) {
	return []*agent.Key{{Format: ssh.KeyAlgoSKED25519, Blob: a.signer.public.Marshal(), Comment: "software-sk-protocol-fixture"}}, nil
}
func (a securityKeyAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	if !bytes.Equal(key.Marshal(), a.signer.public.Marshal()) {
		return nil, errors.New("unknown fixture key")
	}
	return a.signer.Sign(rand.Reader, data)
}
func TestAgentSecurityKeyProtocol(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wire := ssh.Marshal(struct {
		Type        string
		Key         []byte
		Application string
	}{ssh.KeyAlgoSKED25519, public, "ssh:"})
	key, err := ssh.ParsePublicKey(wire)
	if err != nil {
		t.Fatal(err)
	}
	testAgent(t, securityKeyAgent{Agent: agent.NewKeyring(), signer: securityKeySigner{public: key, private: private}})
	server := fixture(t)
	server.Authorize(key)
	cfg := config(server.Address())
	cfg.Authentication = "key"
	cfg.Password = nil
	cfg.UseAgent = true
	result := begin(t, cfg, store(t))
	challenge := result.wait(t, "hostkey").Challenge
	result.session.Resolve(challenge.ID, "once")
	result.wait(t, "connected")
	result.session.Write([]byte("security-key-protocol\r"))
	eventually(t, func() bool { return strings.Contains(result.text(), "received: security-key-protocol") })
	result.session.Close()
	result.ended(t)
}
