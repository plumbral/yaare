package sshclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"yaare/internal/testutil/sshfixture"
)

func hostTrustSigner(t *testing.T) ssh.Signer {
	t.Helper()
	signer, err := sshfixture.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func hostTrustRSA(t *testing.T) ssh.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func trustRecord(endpoint string, key ssh.PublicKey) string {
	return knownhosts.Line([]string{endpoint}, key) + "\n"
}

func TestHostTrustEndpointAlgorithms(t *testing.T) {
	edKey, rsaKey := hostTrustSigner(t).PublicKey(), hostTrustRSA(t).PublicKey()
	supported := ssh.SupportedAlgorithms().HostKeys
	rawDefaults := []string{ssh.KeyAlgoED25519}
	for _, algorithm := range supported {
		if !isHostCertAlgorithm(algorithm) && algorithm != ssh.KeyAlgoED25519 {
			rawDefaults = append(rawDefaults, algorithm)
		}
	}
	certDefaults := slices.DeleteFunc(slices.Clone(supported), func(algo string) bool { return !isHostCertAlgorithm(algo) })
	rsaSHA2 := []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512}
	hashedEndpoint := knownhosts.HashHostname("[build.example]:2222")
	hashedIPv6 := knownhosts.HashHostname(knownhosts.Normalize("[2001:db8::1]:22"))
	for _, test := range []struct {
		name, address, records string
		wantPrefix             []string
		ca                     bool
	}{
		{"hashed exact port", "build.example:2222", trustRecord(hashedEndpoint, rsaKey), rsaSHA2, false},
		{"hashed wrong port", "build.example:22", trustRecord(hashedEndpoint, rsaKey), nil, false},
		{"hashed IPv6", "[2001:db8::1]:22", trustRecord(hashedIPv6, rsaKey), rsaSHA2, false},
		{"unhashed exact port", "build.example:2222", trustRecord("build.example:2222", rsaKey), rsaSHA2, false},
		{"unhashed wrong port", "build.example:2222", trustRecord("build.example:22", rsaKey), nil, false},
		{"RSA uses SHA-2", "build.example:22", trustRecord("build.example:22", rsaKey), rsaSHA2, false},
		{"revoked not preferred", "build.example:22", trustRecord("build.example:22", rsaKey) + "@revoked " + trustRecord("other.example:22", rsaKey), nil, false},
		{"hashed CA", "build.example:2222", "@cert-authority " + trustRecord(hashedEndpoint, edKey), nil, true},
		{"CA wrong port", "build.example:2222", "@cert-authority " + trustRecord("build.example:22", edKey), nil, false},
		{"wildcard CA", "build.example:22", "@cert-authority *.example,!blocked.example " + strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(edKey)), "\n"), nil, true},
		{"negated CA", "blocked.example:22", "@cert-authority !blocked.example,*.example " + strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(edKey)), "\n"), nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			trust, ok := parseHostTrust([]byte(test.records), test.address)
			if !ok || trust.requiresCertificate() != test.ca {
				t.Fatalf("unexpected endpoint policy: valid=%v, requires cert=%v", ok, trust.requiresCertificate())
			}
			want := certDefaults
			if !test.ca {
				want = slices.Concat(test.wantPrefix, slices.DeleteFunc(slices.Clone(rawDefaults), func(algo string) bool {
					return slices.Contains(test.wantPrefix, algo)
				}))
			}
			if got := trust.algorithms(supported); !slices.Equal(want, got) || slices.Contains(got, ssh.KeyAlgoRSA) {
				t.Fatalf("algorithms = %v, want %v without ssh-rsa", got, want)
			}
		})
	}
	if got := (hostTrust{}).algorithms([]string{ssh.CertAlgoED25519v01, ssh.KeyAlgoECDSA256}); !slices.Equal(got, []string{ssh.KeyAlgoECDSA256}) {
		t.Fatalf("algorithms = %v, want only supported raw ECDSA when Ed25519 is unavailable", got)
	}
}

func TestHostTrustHandshake(t *testing.T) {
	hostKey, ca, wrongCA, otherRaw := hostTrustSigner(t), hostTrustSigner(t), hostTrustSigner(t), hostTrustRSA(t)
	for _, test := range []struct {
		name, caRule, certProblem, revoked, fault                string
		offerCert, offerRaw, pinRaw, pinRSA, pinCert, wantPrompt bool
	}{
		{name: "no saved key prefers ordinary Ed25519 over RSA and certificate", offerCert: true, offerRaw: true, wantPrompt: true},
		{name: "pinned RSA outranks default Ed25519 and certificate", offerCert: true, offerRaw: true, pinRSA: true},
		{name: "other port CA chooses raw", caRule: "other port", offerCert: true, offerRaw: true, pinRaw: true},
		{name: "other port CA cannot validate certificate-only server", caRule: "other port", offerCert: true, fault: "HOST_CERT_UNSUPPORTED"},
		{name: "unmarked certificate does not enable certificate trust", offerCert: true, pinCert: true, fault: "HOST_CERT_UNSUPPORTED"},
		{name: "certificate-only without CA", offerCert: true, fault: "HOST_CERT_UNSUPPORTED"},
		{name: "matching CA requires certificate even with pinned raw", caRule: "exact", offerRaw: true, pinRaw: true, fault: "HOST_CERT_REQUIRED"},
		{name: "matching CA accepts certificate", caRule: "exact", offerCert: true, offerRaw: true, pinRaw: true},
		{name: "matching hashed CA accepts certificate", caRule: "hashed", offerCert: true},
		{name: "wrong CA does not downgrade", caRule: "exact", offerCert: true, offerRaw: true, pinRaw: true, certProblem: "wrong CA", fault: "HOST_CERT_INVALID"},
		{name: "expired certificate does not downgrade", caRule: "exact", offerCert: true, offerRaw: true, pinRaw: true, certProblem: "expired", fault: "HOST_CERT_INVALID"},
		{name: "wrong principal does not downgrade", caRule: "exact", offerCert: true, offerRaw: true, pinRaw: true, certProblem: "principal", fault: "HOST_CERT_INVALID"},
		{name: "invalid signature does not downgrade", caRule: "exact", offerCert: true, offerRaw: true, pinRaw: true, certProblem: "signature", fault: "HOST_CERT_INVALID"},
		{name: "revoked CA does not downgrade", caRule: "exact", offerCert: true, offerRaw: true, pinRaw: true, revoked: "ca", fault: "HOST_KEY_REVOKED"},
		{name: "revoked cert key does not downgrade", caRule: "exact", offerCert: true, offerRaw: true, pinRaw: true, revoked: "host", fault: "HOST_KEY_REVOKED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			address := listener.Addr().String()
			cfg := config(address)
			now := uint64(time.Now().Unix())
			cert := &ssh.Certificate{Key: hostKey.PublicKey(), CertType: ssh.HostCert, ValidPrincipals: []string{cfg.Host}, ValidAfter: now - 120, ValidBefore: now + 120}
			issuer := ca
			switch test.certProblem {
			case "wrong CA":
				issuer = wrongCA
			case "principal":
				cert.ValidPrincipals = []string{"wrong.example"}
			case "expired":
				cert.ValidBefore = now - 1
			}
			if err := cert.SignCert(rand.Reader, issuer); err != nil {
				t.Fatal(err)
			}
			if test.certProblem == "signature" {
				cert.KeyId = "changed after signing"
			}
			certSigner, err := ssh.NewCertSigner(cert, hostKey)
			if err != nil {
				t.Fatal(err)
			}

			var authCalls, connections atomic.Int32
			server := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
				authCalls.Add(1)
				return nil, nil
			}}
			if test.offerCert {
				server.AddHostKey(certSigner)
			}
			if test.offerRaw {
				server.AddHostKey(otherRaw)
				server.AddHostKey(hostKey)
			}
			go func() {
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					connections.Add(1)
					go func() {
						defer conn.Close()
						conn.SetDeadline(time.Now().Add(5 * time.Second))
						peer, _, _, err := ssh.NewServerConn(conn, server)
						if err == nil {
							peer.Wait()
						}
					}()
				}
			}()

			var records string
			switch test.caRule {
			case "exact":
				records += "@cert-authority " + trustRecord(address, ca.PublicKey())
			case "other port":
				records += "@cert-authority " + trustRecord(net.JoinHostPort(cfg.Host, "22"), ca.PublicKey())
			case "hashed":
				records += "@cert-authority " + trustRecord(knownhosts.HashHostname(knownhosts.Normalize(address)), ca.PublicKey())
			}
			if test.pinRaw {
				records += trustRecord(knownhosts.HashHostname(knownhosts.Normalize(address)), hostKey.PublicKey())
			}
			if test.pinRSA {
				records += trustRecord(knownhosts.HashHostname(knownhosts.Normalize(address)), otherRaw.PublicKey())
			}
			if test.pinCert {
				records += trustRecord(address, cert)
			}
			switch test.revoked {
			case "ca":
				records += "@revoked " + trustRecord(address, ca.PublicKey())
			case "host":
				records += "@revoked " + trustRecord(address, hostKey.PublicKey())
			}
			trust := store(t)
			path, _ := trust.Path()
			if err := os.WriteFile(path, []byte(records), 0600); err != nil {
				t.Fatal(err)
			}
			var prompts atomic.Int32
			var session *Session
			session = NewSession(t.Context(), trust, func(event Event) error {
				if event.Type == "hostkey" || event.Type == "authentication" {
					prompts.Add(1)
					if challenge := event.Challenge; test.wantPrompt && challenge != nil && challenge.Algorithm == ssh.KeyAlgoED25519 &&
						challenge.Fingerprint == ssh.FingerprintSHA256(hostKey.PublicKey()) && len(challenge.PreviousFingerprints) == 0 &&
						session.Resolve(challenge.ID, "once") {
						return nil
					}
					return errors.New("unexpected SSH trust or authentication prompt")
				}
				return nil
			})
			defer session.Close()
			_, _, cleanup, err := session.dialChain(&cfg)
			defer cleanup()
			if test.fault == "" {
				if err != nil || authCalls.Load() != 1 {
					t.Fatalf("trusted handshake failed: %v, auth calls=%d", err, authCalls.Load())
				}
				pinned := hostKey.PublicKey()
				if test.pinRSA {
					pinned = otherRaw.PublicKey()
				}
				if test.caRule == "exact" || test.caRule == "hashed" {
					pinned = cert
				}
				if !strings.HasSuffix(session.fileIdentity, fmt.Sprintf("\x00%x", sha256.Sum256(pinned.Marshal()))) {
					t.Fatal("handshake pinned the wrong host identity")
				}
			} else {
				var fault *Fault
				if !errors.As(err, &fault) || fault.Code != test.fault || authCalls.Load() != 0 {
					t.Fatalf("want %s without authentication, got %#v, auth calls=%d", test.fault, err, authCalls.Load())
				}
			}
			var wantPrompts int32
			if test.wantPrompt {
				wantPrompts = 1
			}
			if prompts.Load() != wantPrompts || connections.Load() != 1 {
				t.Fatalf("unexpected prompts or retry: prompts=%d, want %d; connections=%d", prompts.Load(), wantPrompts, connections.Load())
			}
			current, err := os.ReadFile(path)
			if err != nil || string(current) != records {
				t.Fatalf("handshake altered the trust store: %v", err)
			}
		})
	}
}

func TestHostTrustReplacementRules(t *testing.T) {
	const address = "build.example:22"
	oldKey, newKey := hostTrustSigner(t).PublicKey(), hostTrustSigner(t).PublicKey()
	for _, test := range []struct {
		name, pattern, newRule, fault string
	}{
		{name: "hashed endpoint", pattern: knownhosts.HashHostname(knownhosts.Normalize(address))},
		{name: "shared wildcard", pattern: "*.example", fault: "HOST_KEY_CHANGED"},
		{name: "shared hosts", pattern: "build.example,other.example", fault: "HOST_KEY_CHANGED"},
		{name: "revoked after prompt", pattern: "build.example", newRule: "@revoked " + trustRecord(address, newKey), fault: "HOST_KEY_REVOKED"},
		{name: "CA added after prompt", pattern: "build.example", newRule: "@cert-authority " + trustRecord(address, oldKey), fault: "HOST_CERT_REQUIRED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			trust := store(t)
			path, _ := trust.Path()
			original := "# comment\r\n" + trustRecord(test.pattern, oldKey)
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			err := trust.verify(t.Context(), path, address, newKey, func(challenge Challenge) (string, error) {
				if !slices.Equal(challenge.PreviousFingerprints, []string{ssh.FingerprintSHA256(oldKey)}) {
					t.Fatalf("unexpected old fingerprints: %v", challenge.PreviousFingerprints)
				}
				if test.newRule != "" {
					if err := os.WriteFile(path, []byte(original+test.newRule), 0600); err != nil {
						return "", err
					}
				}
				return "replace", nil
			})
			want := "# comment\r\n" + trustRecord(address, newKey)
			if test.fault == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var got *Fault
				if !errors.As(err, &got) || got.Code != test.fault {
					t.Fatalf("want %s, got %#v", test.fault, err)
				}
				want = original + test.newRule
			}
			if contents, err := os.ReadFile(path); err != nil || !bytes.Equal(contents, []byte(want)) {
				t.Fatalf("unexpected trust store after replacement: %v", err)
			}
		})
	}
}

func TestHostTrustFileLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts.lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := lockHostTrust(t.Context(), path)
	if err != nil {
		t.Fatalf("leftover lock filename prevented acquisition: %v", err)
	}
	t.Cleanup(func() { lock.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Millisecond)
	defer cancel()
	if contender, err := lockHostTrust(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if contender != nil {
			contender.Close()
		}
		t.Fatalf("live lock did not block a competing opener until its deadline: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	lockAgain, err := lockHostTrust(t.Context(), path)
	if err != nil {
		t.Fatalf("lock remained held after closing its handle: %v", err)
	}
	defer lockAgain.Close()
}
