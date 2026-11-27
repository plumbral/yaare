//go:build windows || linux

package ipc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cryptossh "golang.org/x/crypto/ssh"
	"yaare/internal/adapters/secrets"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/domain/workspace"
)

type passwordSSHServer struct {
	port        int
	fingerprint string
	password    atomic.Value
	logins      atomic.Int32
	forwards    atomic.Int32
}

func TestRealOpenSSHStandalonePasswordAndSavedJump(t *testing.T) {
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" {
		t.Skip("requires isolated Docker OpenSSH")
	}
	requireCurrentUserCredentialTests(t)
	fingerprints := strings.Split(os.Getenv("YAARE_OPENSSH_FINGERPRINT"), ",")
	trusted := map[string]bool{}
	for _, f := range fingerprints {
		if f != "" {
			trusted[f] = true
		}
	}
	if len(trusted) == 0 {
		t.Fatal("verified fixture fingerprint required")
	}
	_, service, ctx, dir := passwordFixture(t, secrets.CurrentUser{})
	ssh, closeSSH := NewSSHService(NewKeyService(), func() (string, error) { return dir, nil })
	defer closeSSH()
	ConfigureSSHWorkspace(ssh, service.application)
	host := passwordHost()
	if user := os.Getenv("YAARE_OPENSSH_USER"); user != "" {
		host.Username = user
	}
	password := os.Getenv("YAARE_OPENSSH_PASSWORD")
	if password == "" {
		password = "Yaare-OpenSSH-Test-Only!"
	}
	h := saveHostPassword(t, service, ctx, host, password, true)
	connectPasswordFixture(t, ssh, ctx, h, 1, trusted)
	connectPasswordFixture(t, ssh, ctx, h, 2, trusted)
	target := h
	target.ID = "saved-target-behind-jump"
	target.Name = "Target through saved-password jump"
	target.Port = 22
	target.JumpHostIDs = []string{h.ID}
	target.HasCredential = false
	target = saveHostPassword(t, service, ctx, target, password, true)
	connectPasswordFixture(t, ssh, ctx, target, 3, trusted)
}

func newPasswordSSHServer(t *testing.T, password string, allowedTarget int) *passwordSSHServer {
	t.Helper()
	_, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := cryptossh.NewSignerFromKey(key)
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := &passwordSSHServer{port: l.Addr().(*net.TCPAddr).Port, fingerprint: cryptossh.FingerprintSHA256(signer.PublicKey())}
	s.password.Store(password)
	cfg := &cryptossh.ServerConfig{PasswordCallback: func(c cryptossh.ConnMetadata, p []byte) (*cryptossh.Permissions, error) {
		if c.User() == "fixture" && string(p) == s.password.Load().(string) {
			s.logins.Add(1)
			return nil, nil
		}
		return nil, errors.New("fixture authentication rejected")
	}}
	cfg.AddHostKey(signer)
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		l.Close()
		mu.Lock()
		for c := range connections {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			connections[c] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				defer func() { mu.Lock(); delete(connections, c); mu.Unlock() }()
				conn, chans, requests, e := cryptossh.NewServerConn(c, cfg)
				if e != nil {
					return
				}
				defer conn.Close()
				go cryptossh.DiscardRequests(requests)
				for incoming := range chans {
					switch incoming.ChannelType() {
					case "session":
						channel, requests, e := incoming.Accept()
						if e != nil {
							continue
						}
						go func() {
							defer channel.Close()
							for r := range requests {
								switch r.Type {
								case "pty-req", "env", "window-change":
									r.Reply(true, nil)
								case "shell":
									r.Reply(true, nil)
									channel.Write([]byte("fixture ready\r\n"))
									go io.Copy(io.Discard, channel)
								default:
									r.Reply(false, nil)
								}
							}
						}()
					case "direct-tcpip":
						var request struct {
							Host       string
							Port       uint32
							Origin     string
							OriginPort uint32
						}
						if cryptossh.Unmarshal(incoming.ExtraData(), &request) != nil || request.Host != "127.0.0.1" || int(request.Port) != allowedTarget {
							incoming.Reject(cryptossh.Prohibited, "isolated fixture target only")
							continue
						}
						remote, e := net.DialTimeout("tcp", net.JoinHostPort(request.Host, strconv.Itoa(int(request.Port))), time.Second)
						if e != nil {
							incoming.Reject(cryptossh.ConnectionFailed, "target unavailable")
							continue
						}
						channel, requests, e := incoming.Accept()
						if e != nil {
							remote.Close()
							continue
						}
						s.forwards.Add(1)
						go cryptossh.DiscardRequests(requests)
						go func() {
							defer remote.Close()
							defer channel.Close()
							go func() { io.Copy(channel, remote); channel.Close() }()
							io.Copy(remote, channel)
						}()
					default:
						incoming.Reject(cryptossh.UnknownChannelType, "unsupported fixture channel")
					}
				}
			}()
		}
	}()
	return s
}

type passwordEventSink struct {
	ctx    context.Context
	events chan sshclient.Event
}

func (s passwordEventSink) Send(e sshclient.Event) error {
	if e.Type == "output" {
		return nil
	}
	select {
	case s.events <- e:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
func connectPasswordFixture(t *testing.T, service *SSHService, ctx context.Context, h workspace.Host, attempt int, trusted map[string]bool) {
	t.Helper()
	r := passwordRequest(h)
	r.SessionID = "password-protocol"
	r.Attempt = attempt
	r.JumpHostIDs = h.JumpHostIDs
	events := make(chan sshclient.Event, 32)
	done := make(chan error, 1)
	go func() { done <- service.connectStream(ctx, r, passwordEventSink{ctx, events}) }()
	timeout := time.NewTimer(8 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case event := <-events:
			switch event.Type {
			case "hostkey":
				if event.Challenge == nil || !trusted[event.Challenge.Fingerprint] {
					t.Fatal("unexpected loopback SSH fingerprint")
				}
				if !service.ConfirmHost(ctx, r.SessionID, attempt, event.Challenge.ID, "once") {
					t.Fatal("trust challenge not resolved")
				}
			case "connected":
				service.Disconnect(ctx, r.SessionID, attempt)
				select {
				case e := <-done:
					if e != nil {
						t.Fatal(e)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("SSH did not release after disconnect")
				}
				return
			case "failed":
				t.Fatal("saved-password SSH failed", event.Code)
			}
		case e := <-done:
			t.Fatal("SSH ended before connecting", e)
		case <-timeout.C:
			service.Disconnect(ctx, r.SessionID, attempt)
			t.Fatal("saved-password SSH timed out")
		}
	}
}

func TestConnectionPasswordsRealSSHReconnectReplacementAndTwoHop(t *testing.T) {
	target := newPasswordSSHServer(t, "Target-Password-First!", 0)
	hop := newPasswordSSHServer(t, "Jump-Password-Only!", target.port)
	store, service, ctx, dir := passwordFixture(t, passwordProtector(t, 45))
	ssh, shutdown := NewSSHService(NewKeyService(), func() (string, error) { return filepath.Join(dir, "trust"), nil })
	defer shutdown()
	ssh.credentialProtector = service.protector
	ConfigureSSHWorkspace(ssh, service.application)
	h := passwordHost()
	h.Username = "fixture"
	h.Port = target.port
	h = saveHostPassword(t, service, ctx, h, "Target-Password-First!", true)
	trusted := map[string]bool{target.fingerprint: true, hop.fingerprint: true}
	connectPasswordFixture(t, ssh, ctx, h, 1, trusted)
	connectPasswordFixture(t, ssh, ctx, h, 2, trusted)
	target.password.Store("Target-Password-Replaced!")
	h = saveHostPassword(t, service, ctx, h, "Target-Password-Replaced!", true)
	connectPasswordFixture(t, ssh, ctx, h, 3, trusted)
	j := h
	j.ID = "jump-host"
	j.Name = "Jump host"
	j.Port = hop.port
	j.HasCredential = false
	j = saveHostPassword(t, service, ctx, j, "Jump-Password-Only!", true)
	h.JumpHostIDs = []string{j.ID}
	h = saveHostPassword(t, service, ctx, h, "", true)
	connectPasswordFixture(t, ssh, ctx, h, 4, trusted)
	if target.logins.Load() != 4 || hop.logins.Load() != 1 || hop.forwards.Load() != 1 {
		t.Fatal("expected separate target and jump authentications", target.logins.Load(), hop.logins.Load(), hop.forwards.Load())
	}
	identities, e := store.Identities(ctx)
	if e != nil || len(identities) != 0 {
		t.Fatal("protocol test required identities", e)
	}
	hop.password.Store("Jump-Password-Replaced!")
	j = saveHostPassword(t, service, ctx, j, "Jump-Password-Replaced!", true)
	connectPasswordFixture(t, ssh, ctx, h, 5, trusted)
	if hop.logins.Load() != 2 || target.logins.Load() != 5 {
		t.Fatal("jump reconnect used stale authentication")
	}
}
