//go:build windows

package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealOpenSSHAgentUserCertificateAndExpiry(t *testing.T) {
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
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := ssh.NewSignerFromKey(caKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "user-ca.pub")
	if err = os.WriteFile(path, ssh.MarshalAuthorizedKey(ca.PublicKey()), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, e := exec.Command("docker", args...).CombinedOutput(); e != nil {
			t.Fatalf("certificate fixture: %v %s", e, out)
		}
	}
	prefix := fmt.Sprintf("/tmp/yaare-cert-%d", time.Now().UnixNano())
	port := 32000 + int(time.Now().UnixNano()%10000)
	run("cp", path, "yaare-test-openssh:"+prefix+".pub")
	run("exec", "yaare-test-openssh", "/usr/sbin/sshd", "-p", fmt.Sprint(port), "-o", "PidFile="+prefix+".pid", "-o", "TrustedUserCAKeys="+prefix+".pub", "-o", "AuthorizedKeysFile=none", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no", "-o", "AuthenticationMethods=publickey")
	defer run("exec", "yaare-test-openssh", "sh", "-c", "kill $(cat "+prefix+".pid); rm -f "+prefix+".pid "+prefix+".pub")
	ring := agent.NewKeyring()
	testAgent(t, ring)
	for _, invalid := range []string{"", "expired", "wrong-principal"} {
		t.Run("certificate-"+invalid, func(t *testing.T) {
			ring.RemoveAll()
			ring.Add(agent.AddedKey{PrivateKey: private})
			now := uint64(time.Now().Unix())
			cert := &ssh.Certificate{Key: signer.PublicKey(), CertType: ssh.UserCert, KeyId: "yaare-test", ValidPrincipals: []string{"yaaretest"}, ValidAfter: now - 60, ValidBefore: now + 3600, Permissions: ssh.Permissions{Extensions: map[string]string{"permit-pty": ""}}}
			if invalid == "expired" {
				cert.ValidAfter = now - 3600
				cert.ValidBefore = now - 60
			}
			if invalid == "wrong-principal" {
				cert.ValidPrincipals = []string{"other-user"}
			}
			if e := cert.SignCert(rand.Reader, ca); e != nil {
				t.Fatal(e)
			}
			if e := ring.Add(agent.AddedKey{PrivateKey: private, Certificate: cert}); e != nil {
				t.Fatal(e)
			}
			hop := routingConfig(22222, "Agent jump")
			hop.Authentication = "key"
			hop.Password = nil
			hop.UseAgent = true
			cfg := hop
			cfg.Name = "Certificate target"
			cfg.Port = port
			cfg.Jumps = []Config{hop}
			result := begin(t, cfg, store(t))
			deadline := time.After(15 * time.Second)
			challenges := 0
			for {
				select {
				case event := <-result.events:
					switch event.Type {
					case "hostkey":
						challenges++
						if !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+event.Challenge.Fingerprint+",") {
							t.Fatal("unexpected fingerprint")
						}
						result.session.Resolve(event.Challenge.ID, "once")
					case "connected":
						if invalid != "" {
							t.Fatal("invalid certificate authenticated")
						}
						if challenges != 2 {
							t.Fatal("each hop trust missing")
						}
						ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
						defer cancel()
						files, closeFiles, e := result.session.OpenFiles(ctx)
						if e != nil {
							t.Fatal(e)
						}
						defer closeFiles()
						if _, e = files.Stat("/home/yaaretest/files/hello.txt"); e != nil {
							t.Fatal(e)
						}
						result.session.Close()
						result.ended(t)
						return
					case "failed":
						if invalid == "" || event.Code != "AUTHENTICATION_FAILED" {
							t.Fatal(event.Code, event.Message)
						}
						result.ended(t)
						return
					case "authentication":
						t.Fatal("certificate-only server unexpectedly requested a password")
					}
				case <-deadline:
					t.Fatal("certificate test timed out")
				}
			}
		})
	}
}
