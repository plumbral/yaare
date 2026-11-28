package sshclient

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"yaare/internal/testutil/sshfixture"
)

func TestReconnectNetworkFailuresAreDistinguished(t *testing.T) {
	for _, sample := range []struct {
		err  error
		code string
	}{
		{&net.DNSError{IsNotFound: true}, "DNS_NOT_FOUND"},
		{&net.DNSError{IsTemporary: true}, "DNS_FAILED"},
		{syscall.ENETUNREACH, "NETWORK_UNREACHABLE"},
		{io.ErrUnexpectedEOF, "CONNECTION_LOST"},
		{fault("HOST_KEY_CHANGED", "changed"), "HOST_KEY_CHANGED"},
	} {
		if code, _ := classify(sample.err); code != sample.code {
			t.Errorf("classify(%v) = %s, want %s", sample.err, code, sample.code)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("the isolated closed port unexpectedly accepted a connection")
	}
	if code, _ := classify(err); code != "CONNECTION_REFUSED" {
		t.Fatalf("loopback port refusal classified as %s: %v", code, err)
	}
}

type runResult struct {
	session      *Session
	events       chan Event
	done         chan struct{}
	ctx          context.Context
	mu           sync.Mutex
	output       bytes.Buffer
	outputEvents int
}

func fixture(t *testing.T) *sshfixture.Server {
	t.Helper()
	s, err := sshfixture.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func config(address string) Config {
	host, port, _ := net.SplitHostPort(address)
	n, _ := strconv.Atoi(port)
	return Config{Host: host, Port: n, Username: sshfixture.Username, Authentication: "password", Password: []byte(sshfixture.Password), Timeout: 3 * time.Second}
}
func store(t *testing.T) *TrustStore {
	path := filepath.Join(t.TempDir(), "known_hosts")
	return &TrustStore{Path: func() (string, error) { return path, nil }}
}
func begin(t *testing.T, cfg Config, trust *TrustStore) *runResult {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &runResult{events: make(chan Event, 64), done: make(chan struct{}), ctx: ctx}
	r.session = NewSession(ctx, trust, func(event Event) error {
		if event.Type == "output" {
			r.mu.Lock()
			r.outputEvents++
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
	go func() { r.session.Run(&cfg); close(r.done) }()
	return r
}
func (r *runResult) wait(t *testing.T, kind string) Event {
	t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-r.events:
			if event.Type == kind {
				return event
			}
			if event.Type == "progress" {
				continue
			}
			t.Fatalf("expected %s, got %s (%s)", kind, event.Type, event.Code)
		case <-timer.C:
			t.Fatalf("timeout waiting for %s", kind)
		}
	}
}
func (r *runResult) ended(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not release its network resources")
	}
}
func (r *runResult) text() string { r.mu.Lock(); defer r.mu.Unlock(); return r.output.String() }

func TestAuthenticationBannerPrecedesLoginAndSurvivesRejectedAuthentication(t *testing.T) {
	server := fixture(t)
	server.SetBanner("欢迎\npolicy\t✦\r\n\x1b[2J\a\rnext\n")
	const expected = "欢迎\r\npolicy\t✦\r\n�[2J�\r\nnext\r\n"
	trust := store(t)
	for _, accepted := range []bool{true, false} {
		cfg := config(server.Address())
		if !accepted {
			cfg.Password = []byte("wrong")
		}
		run := begin(t, cfg, trust)
		if accepted {
			challenge := run.wait(t, "hostkey").Challenge
			run.session.Resolve(challenge.ID, "remember")
			run.wait(t, "connected")
			eventually(t, func() bool { return strings.Contains(run.text(), sshfixture.Greeting) })
			if out := run.text(); out != expected+sshfixture.Greeting {
				t.Fatalf("banner and login output out of order: %q", out)
			}
			run.session.Close()
		} else {
			run.wait(t, "failed")
			if out := run.text(); out != expected {
				t.Fatalf("banner lost during authentication: %q", out)
			}
		}
		run.ended(t)
	}
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestPasswordPTYUnicodeResizeAndRemoteExit(t *testing.T) {
	server := fixture(t)
	trust := store(t)
	r := begin(t, config(server.Address()), trust)
	if got := r.wait(t, "progress").Message; got != "正在连接目标主机 · "+server.Address() {
		t.Fatalf("unexpected dial stage: %q", got)
	}
	if got := r.wait(t, "progress").Message; got != "正在进行 SSH 握手 · "+server.Address() {
		t.Fatalf("unexpected handshake stage: %q", got)
	}
	challenge := r.wait(t, "hostkey").Challenge
	if challenge == nil || challenge.Fingerprint != server.Fingerprint() {
		t.Fatal("fingerprint was not the server key")
	}
	if server.AuthCalls.Load() != 0 {
		t.Fatal("credentials used before host trust")
	}
	if r.session.Resolve("stale-challenge", "remember") {
		t.Fatal("stale challenge accepted")
	}
	if !r.session.Resolve(challenge.ID, "remember") || r.session.Resolve(challenge.ID, "once") {
		t.Fatal("decision was not exactly once")
	}
	if got := r.wait(t, "progress").Message; got != "正在进行 SSH 认证 · "+server.Address() {
		t.Fatalf("unexpected authentication stage: %q", got)
	}
	if got := r.wait(t, "progress").Message; got != "正在启动远程终端…" {
		t.Fatalf("unexpected terminal startup stage: %q", got)
	}
	r.wait(t, "connected")
	if !r.session.Write([]byte("你好\r")) {
		t.Fatal("input rejected")
	}
	if !r.session.Resize(132, 43) {
		t.Fatal("resize rejected")
	}
	if r.session.Resize(0, 43) || r.session.Resize(1001, 43) {
		t.Fatal("invalid dimensions accepted")
	}
	eventually(t, func() bool { return strings.Contains(r.text(), "received: 你好") })
	var found bool
	deadline := time.After(2 * time.Second)
	for !found {
		select {
		case size := <-server.Resizes:
			found = size.Columns == 132 && size.Rows == 43
		case <-deadline:
			t.Fatal("PTY resize did not reach SSH server")
		}
	}
	if !r.session.Write([]byte("exit\r")) {
		t.Fatal("exit rejected")
	}
	r.wait(t, "disconnected")
	r.ended(t)
	if !strings.HasPrefix(r.text(), sshfixture.Greeting) {
		t.Fatal("client injected terminal output")
	}
	if r.session.Write([]byte("after-close")) {
		t.Fatal("closed session accepted input")
	}
	r2 := begin(t, config(server.Address()), trust)
	r2.wait(t, "connected")
	r2.session.Close()
	r2.ended(t)
}

func TestPastedPrivateKeyAuthenticationAndCleanup(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	server := fixture(t)
	server.Authorize(key.PublicKey())
	for _, encrypted := range []bool{false, true} {
		var block *pem.Block
		if encrypted {
			block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "fixture", []byte("test-phrase"))
		} else {
			block, err = ssh.MarshalPrivateKey(private, "fixture")
		}
		if err != nil {
			t.Fatal(err)
		}
		data := pem.EncodeToMemory(block)
		cfg := config(server.Address())
		cfg.Authentication = "key"
		cfg.Password = nil
		cfg.PrivateKey = data
		if encrypted {
			cfg.Passphrase = []byte("test-phrase")
		}
		r := begin(t, cfg, store(t))
		challenge := r.wait(t, "hostkey").Challenge
		r.session.Resolve(challenge.ID, "once")
		r.wait(t, "connected")
		if !r.session.Write([]byte("pasted-key-pty\r")) {
			t.Fatal("pasted key did not authenticate a PTY")
		}
		eventually(t, func() bool { return strings.Contains(r.text(), "received: pasted-key-pty") })
		r.session.Close()
		r.ended(t)
		if !bytes.Equal(data, make([]byte, len(data))) {
			t.Fatal("private key bytes retained after parse")
		}
	}
	for _, data := range [][]byte{[]byte("invalid-private-material"), make([]byte, 1024*1024+1)} {
		cfg := config(server.Address())
		cfg.Authentication = "key"
		cfg.Password = nil
		cfg.PrivateKey = data
		r := begin(t, cfg, store(t))
		event := r.wait(t, "failed")
		if event.Code != "KEY_INVALID" || strings.Contains(event.Message, "invalid-private-material") {
			t.Fatal("unsafe private-key error")
		}
		r.ended(t)
		if !bytes.Equal(data, make([]byte, len(data))) {
			t.Fatal("invalid private key not cleared")
		}
	}
}

func TestRejectAndCancelHostTrustNeverAuthenticate(t *testing.T) {
	for _, decision := range []string{"reject", "cancel"} {
		t.Run(decision, func(t *testing.T) {
			server := fixture(t)
			r := begin(t, config(server.Address()), store(t))
			challenge := r.wait(t, "hostkey").Challenge
			if decision == "cancel" {
				r.session.Close()
				r.wait(t, "disconnected")
			} else {
				r.session.Resolve(challenge.ID, "reject")
				event := r.wait(t, "failed")
				if event.Code != "HOST_KEY_REJECTED" {
					t.Fatal(event.Code)
				}
			}
			r.ended(t)
			if server.AuthCalls.Load() != 0 {
				t.Fatal("authentication happened without approval")
			}
		})
	}
}

func TestChangedHostKeyCanBeReplacedOnlyAfterApproval(t *testing.T) {
	server := fixture(t)
	trust := store(t)
	path, _ := trust.Path()
	other, err := sshfixture.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	unrelated := knownhosts.Line([]string{"other.example:22"}, other.PublicKey()) + "\n"
	if err = os.WriteFile(path, []byte(unrelated), 0600); err != nil {
		t.Fatal(err)
	}
	r := begin(t, config(server.Address()), trust)
	challenge := r.wait(t, "hostkey").Challenge
	if r.session.Resolve(challenge.ID, "replace") {
		t.Fatal("first connection accepted a replacement decision")
	}
	r.session.Resolve(challenge.ID, "remember")
	r.wait(t, "connected")
	r.session.Close()
	r.ended(t)
	calls := server.AuthCalls.Load()
	key, err := sshfixture.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	server.SetHostKey(key)
	r2 := begin(t, config(server.Address()), trust)
	changed := r2.wait(t, "hostkey").Challenge
	if changed.Fingerprint != server.Fingerprint() || len(changed.PreviousFingerprints) != 1 || changed.PreviousFingerprints[0] != challenge.Fingerprint || server.AuthCalls.Load() != calls {
		t.Fatal("changed key was not paused before auth")
	}
	if r2.session.Resolve(challenge.ID, "replace") || r2.session.Resolve(changed.ID, "once") || r2.session.Resolve(changed.ID, "remember") {
		t.Fatal("invalid approval advanced a changed key")
	}
	r2.session.Resolve(changed.ID, "reject")
	event := r2.wait(t, "failed")
	r2.ended(t)
	if event.Code != "HOST_KEY_REJECTED" || server.AuthCalls.Load() != calls {
		t.Fatal("rejecting the fingerprint did not stop before authentication", event)
	}
	r3 := begin(t, config(server.Address()), trust)
	changed = r3.wait(t, "hostkey").Challenge
	if len(changed.PreviousFingerprints) != 1 || changed.PreviousFingerprints[0] != challenge.Fingerprint || !r3.session.Resolve(changed.ID, "replace") {
		t.Fatal("decline changed the trust record or replacement was rejected")
	}
	r3.wait(t, "connected")
	r3.session.Close()
	r3.ended(t)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != unrelated+knownhosts.Line([]string{server.Address()}, key.PublicKey())+"\n" {
		t.Fatal("replacement did not preserve unrelated entries and replace just the old key", err)
	}
	r4 := begin(t, config(server.Address()), trust)
	r4.wait(t, "connected")
	r4.session.Close()
	r4.ended(t)
}

func TestHostKeyReplacementRejectsStaleAndRevokedRecords(t *testing.T) {
	t.Run("another decision changed the record", func(t *testing.T) {
		server, trust := fixture(t), store(t)
		path, _ := trust.Path()
		seed := func() []byte {
			t.Helper()
			signer, err := sshfixture.NewSigner()
			if err != nil {
				t.Fatal(err)
			}
			data := []byte(knownhosts.Line([]string{server.Address()}, signer.PublicKey()) + "\n")
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			return data
		}
		seed()
		run := begin(t, config(server.Address()), trust)
		challenge := run.wait(t, "hostkey").Challenge
		newer := seed()
		if !run.session.Resolve(challenge.ID, "replace") || run.wait(t, "failed").Code != "HOST_KEY_CHANGED" {
			t.Fatal("stale confirmation did not fail closed")
		}
		run.ended(t)
		record, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(record, newer) || server.AuthCalls.Load() != 0 {
			t.Fatal("stale approval changed trust or allowed authentication", err)
		}
	})
	t.Run("revoked server key", func(t *testing.T) {
		server, trust := fixture(t), store(t)
		signer, err := sshfixture.NewSigner()
		if err != nil {
			t.Fatal(err)
		}
		server.SetHostKey(signer)
		path, _ := trust.Path()
		line := "@revoked " + knownhosts.Line([]string{server.Address()}, signer.PublicKey()) + "\n"
		if err = os.WriteFile(path, []byte(line), 0600); err != nil {
			t.Fatal(err)
		}
		run := begin(t, config(server.Address()), trust)
		if event := run.wait(t, "failed"); event.Code != "HOST_KEY_REVOKED" || server.AuthCalls.Load() != 0 {
			t.Fatal("revoked key was not stopped before authentication", event)
		}
		run.ended(t)
	})
}

func TestTrustOnceDoesNotPersistAndAuthenticationErrorsAreSanitized(t *testing.T) {
	server := fixture(t)
	trust := store(t)
	cfg := config(server.Address())
	cfg.Password = []byte("secret-that-must-not-appear")
	r := begin(t, cfg, trust)
	challenge := r.wait(t, "hostkey").Challenge
	r.session.Resolve(challenge.ID, "once")
	event := r.wait(t, "failed")
	r.ended(t)
	if event.Code != "AUTHENTICATION_FAILED" || strings.Contains(event.Message, "secret-that-must-not-appear") {
		t.Fatal("unsafe or incorrect error", event)
	}
	path, _ := trust.Path()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("trust-once persisted host key")
	}
}

func TestPrivateKeyAlgorithmsAndEncryptedKeys(t *testing.T) {
	for _, kind := range []string{"ed25519", "ecdsa", "rsa"} {
		t.Run(kind, func(t *testing.T) {
			var private any
			var err error
			switch kind {
			case "ed25519":
				_, private, err = ed25519.GenerateKey(rand.Reader)
			case "ecdsa":
				private, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			case "rsa":
				private, err = rsa.GenerateKey(rand.Reader, 2048)
			}
			if err != nil {
				t.Fatal(err)
			}
			key, err := ssh.NewSignerFromKey(private)
			if err != nil {
				t.Fatal(err)
			}
			block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "fixture", []byte("test-passphrase"))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "id_test")
			if err = os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
				t.Fatal(err)
			}
			server := fixture(t)
			server.Authorize(key.PublicKey())
			cfg := config(server.Address())
			cfg.Authentication = "key"
			cfg.Password = nil
			cfg.PrivateKeyPath = path
			cfg.Passphrase = []byte("test-passphrase")
			r := begin(t, cfg, store(t))
			challenge := r.wait(t, "hostkey").Challenge
			r.session.Resolve(challenge.ID, "once")
			r.wait(t, "connected")
			r.session.Close()
			r.ended(t)
			cfg.Passphrase = nil
			missing := begin(t, cfg, store(t))
			if event := missing.wait(t, "failed"); event.Code != "KEY_PASSPHRASE_REQUIRED" {
				t.Fatal(event.Code)
			}
			missing.ended(t)
			cfg.Passphrase = []byte("incorrect")
			wrong := begin(t, cfg, store(t))
			if event := wrong.wait(t, "failed"); event.Code != "KEY_DECRYPT_FAILED" {
				t.Fatal(event.Code)
			}
			wrong.ended(t)
			var der []byte
			pemType := "PRIVATE KEY"
			switch value := private.(type) {
			case *rsa.PrivateKey:
				der = x509.MarshalPKCS1PrivateKey(value)
				pemType = "RSA PRIVATE KEY"
			case *ecdsa.PrivateKey:
				der, err = x509.MarshalECPrivateKey(value)
				pemType = "EC PRIVATE KEY"
			default:
				der, err = x509.MarshalPKCS8PrivateKey(value)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: der}), 0600); err != nil {
				t.Fatal(err)
			}
			cfg.Passphrase = nil
			plain := begin(t, cfg, store(t))
			challenge = plain.wait(t, "hostkey").Challenge
			plain.session.Resolve(challenge.ID, "once")
			plain.wait(t, "connected")
			plain.session.Close()
			plain.ended(t)
		})
	}
}

func TestBulkOutputAndCancellation(t *testing.T) {
	server := fixture(t)
	server.RekeyThreshold.Store(32768)
	r := begin(t, config(server.Address()), store(t))
	challenge := r.wait(t, "hostkey").Challenge
	r.session.Resolve(challenge.ID, "once")
	r.wait(t, "connected")
	r.session.Write([]byte("burst\r"))
	eventually(t, func() bool { return strings.Contains(r.text(), "remote-output-08191 中文") })
	if strings.Count(r.text(), "remote-output-") != 8192 {
		t.Fatal("output dropped or duplicated")
	}
	r.mu.Lock()
	chunks := r.outputEvents
	r.mu.Unlock()
	if chunks >= 1024 {
		t.Fatalf("tiny output packets were not coalesced: %d events", chunks)
	}
	r.session.Close()
	r.ended(t)
}

func TestHandshakeTimeoutAndCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancel), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					accepted <- conn
				}
			}()
			r := begin(t, config(listener.Addr().String()), store(t))
			conn := <-accepted
			defer conn.Close()
			if cancel {
				r.session.Close()
				r.wait(t, "disconnected")
			} else {
				event := r.wait(t, "failed")
				if event.Code != "CONNECTION_TIMEOUT" {
					t.Fatal(event.Code)
				}
			}
			r.ended(t)
		})
	}
}

func TestPTYRefusalAndCorruptTrustStoreFailClosed(t *testing.T) {
	server := fixture(t)
	server.DenyPTY.Store(true)
	trust := store(t)
	r := begin(t, config(server.Address()), trust)
	challenge := r.wait(t, "hostkey").Challenge
	r.session.Resolve(challenge.ID, "once")
	if event := r.wait(t, "failed"); event.Code != "PTY_FAILED" {
		t.Fatal(event.Code)
	}
	r.ended(t)
	path, _ := trust.Path()
	os.WriteFile(path, []byte("broken record\n"), 0600)
	r2 := begin(t, config(server.Address()), trust)
	if event := r2.wait(t, "failed"); event.Code != "KNOWN_HOSTS_INVALID" {
		t.Fatal(event.Code)
	}
	r2.ended(t)
}

func TestCancellationReleasesBackpressuredOutput(t *testing.T) {
	server := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outputBlocked := make(chan struct{}, 1)
	connected := make(chan struct{}, 1)
	var session *Session
	session = NewSession(ctx, store(t), func(event Event) error {
		switch event.Type {
		case "hostkey":
			session.Resolve(event.Challenge.ID, "once")
		case "connected":
			connected <- struct{}{}
		case "output":
			select {
			case outputBlocked <- struct{}{}:
			default:
			}
			<-session.ctx.Done()
			return session.ctx.Err()
		}
		return nil
	})
	done := make(chan struct{})
	cfg := config(server.Address())
	password := cfg.Password
	go func() { session.Run(&cfg); close(done) }()
	select {
	case <-connected:
		if !bytes.Equal(password, make([]byte, len(password))) {
			t.Fatal("handshake retained password bytes")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connection not established")
	}
	select {
	case <-outputBlocked:
	case <-time.After(5 * time.Second):
		t.Fatal("no remote output")
	}
	session.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("backpressure prevented cancellation")
	}
	if len(cfg.Password) != 0 || len(cfg.Passphrase) != 0 {
		t.Fatal("session retained authentication input")
	}
}
