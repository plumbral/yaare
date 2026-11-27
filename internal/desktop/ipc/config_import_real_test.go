package ipc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/adapters/sshconfig"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

func TestRealOpenSSHConfigImportDirectAndJumpKeyAuthentication(t *testing.T) {
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" {
		t.Skip("isolated Docker OpenSSH fixture only")
	}
	if os.Getenv("YAARE_OPENSSH_FINGERPRINT") == "" {
		t.Fatal("verified fixture fingerprint required")
	}
	root := os.Getenv("YAARE_IMPORT_FIXTURE_DIR")
	if root == "" {
		root = t.TempDir()
	}
	if !filepath.IsAbs(root) {
		t.Fatal("absolute fixture directory required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "yaare-import-fixture")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "id_import_fixture")
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	pubPath := keyPath + ".pub"
	if err = os.WriteFile(pubPath, ssh.MarshalAuthorizedKey(public), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("docker", "cp", pubPath, "yaare-test-openssh:/tmp/yaare-import-fixture.pub")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture key setup: %v %s", err, out)
	}
	command = exec.Command("docker", "exec", "yaare-test-openssh", "sh", "-c", "install -d -m 700 -o yaaretest -g yaaretest /home/yaaretest/.ssh; cat /tmp/yaare-import-fixture.pub >> /home/yaaretest/.ssh/authorized_keys; chown yaaretest:yaaretest /home/yaaretest/.ssh/authorized_keys; chmod 600 /home/yaaretest/.ssh/authorized_keys; rm /tmp/yaare-import-fixture.pub")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture authorization: %v %s", err, out)
	}
	configPath := filepath.Join(root, "config")
	body := "Host 导入验收-直连\n HostName 127.0.0.1\n Port 22222\nHost 导入验收-跳板\n HostName 127.0.0.1\n Port 22\n ProxyJump 导入验收-直连\nHost *\n User yaaretest\n IdentityFile \"" + filepath.ToSlash(keyPath) + "\"\n"
	if err = os.WriteFile(configPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store := sqlite.New(func() (string, error) { return filepath.Join(root, "imported-store"), nil })
	defer store.Close()
	if err = store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	service := NewSSHConfigService(store)
	page := new(callerScope)
	preview, err := service.previewFile(ctx, page, configPath, sshconfig.Options{Home: root, Username: "yaaretest"})
	if err != nil {
		t.Fatal(err)
	}
	choices := []ConfigImportChoice{}
	for _, row := range preview.Rows {
		if row.Error != "" {
			t.Fatal(row.Error)
		}
		if !row.Duplicate {
			choices = append(choices, ConfigImportChoice{ID: row.ID, KeyIndex: 0})
		}
	}
	if len(choices) > 0 {
		if _, err = service.commitPlan(ctx, page, preview.Token, choices); err != nil {
			t.Fatal(err)
		}
	}
	report := store.Load(ctx)
	keys, err := store.Keys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	keyMap := map[string]string{}
	for _, key := range keys {
		keyMap[key.ID] = key.Path
	}
	hosts := map[string]workspace.Host{}
	for _, host := range report.Snapshot.Hosts {
		hosts[host.ID] = host
	}
	for _, host := range report.Snapshot.Hosts {
		t.Run(host.Name, func(t *testing.T) {
			configOf := func(h workspace.Host) sshclient.Config {
				return sshclient.Config{Host: h.Address, Port: h.Port, Username: h.Username, Name: h.Name, Authentication: h.Authentication, PrivateKeyPath: keyMap[h.KeyID], Timeout: 10 * time.Second}
			}
			cfg := configOf(host)
			for _, id := range host.JumpHostIDs {
				cfg.Jumps = append(cfg.Jumps, configOf(hosts[id]))
			}
			deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			r := &realBroadcastRun{events: make(chan sshclient.Event, 64)}
			known := filepath.Join(t.TempDir(), "known_hosts")
			r.session = sshclient.NewSession(deadline, &sshclient.TrustStore{Path: func() (string, error) { return known, nil }}, func(event sshclient.Event) error {
				if event.Type == "output" {
					r.mu.Lock()
					r.output.Write(event.Data)
					r.mu.Unlock()
					return nil
				}
				select {
				case r.events <- event:
					return nil
				case <-deadline.Done():
					return deadline.Err()
				}
			})
			defer r.session.Close()
			go r.session.Run(&cfg)
		connected:
			for {
				select {
				case event := <-r.events:
					switch event.Type {
					case "hostkey":
						if !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+event.Challenge.Fingerprint+",") {
							t.Fatal("unexpected fixture fingerprint")
						}
						r.session.Resolve(event.Challenge.ID, "once")
					case "connected":
						break connected
					case "failed":
						t.Fatal("imported connection failed", event.Code)
					}
				case <-deadline.Done():
					t.Fatal("imported connection timed out", strconv.Itoa(host.Port))
				}
			}
			if !r.session.Write([]byte("printf 'SSH_CONFIG_%s\\n' 'IMPORTED'\n")) {
				t.Fatal("PTY write rejected")
			}
			waitBroadcastOutput(t, func() bool { return strings.Contains(r.text(), "SSH_CONFIG_IMPORTED") })
			files, closeFiles, err := r.session.OpenFiles(deadline)
			if err != nil {
				t.Fatal(err)
			}
			defer closeFiles()
			if _, err = files.Stat("/home/yaaretest/files/hello.txt"); err != nil {
				t.Fatal("SFTP after imported connection", err)
			}
		})
	}
}
