//go:build linux

package ipc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yaare/internal/app/vault"

	"yaare/internal/adapters/secrets"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/adapters/storage/sqlite"
)

func TestLinuxEncryptedBackupUsesRealKeyring(t *testing.T) {
	requireCurrentUserCredentialTests(t)
	source, sourceStore, sourceContext, _ := backupFixture(t, 31)
	source.cloud = NewCloudService(sourceStore, secrets.CurrentUser{})
	host, identity, privateKey := seedBackup(t, source, sourceContext, "Linux portable source")
	target, targetStore, targetContext, _ := backupFixture(t, 32)
	target.cloud = NewCloudService(targetStore, secrets.CurrentUser{})
	data, err := source.exportBytes(sourceContext, BackupExportRequest{Password: backupPassword, IncludeCredentials: true})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(privateKey)) || bytes.Contains(data, []byte(host.Name)) {
		t.Fatal("portable file contains plaintext")
	}
	file := chooseBackup(t, target, targetContext, data)
	preview, err := target.Preview(targetContext, file.ID, backupPassword)
	if err != nil {
		t.Fatal(err)
	}
	result, err := target.Apply(targetContext, preview.ID)
	if err != nil || result.RestorePoint == "" {
		t.Fatal("Linux import failed", err)
	}
	loaded, err := targetStore.CloudRead(targetContext)
	if err != nil || len(loaded.Identities) != 1 || len(loaded.Keys) != 1 || loaded.Snapshot.Hosts[0].ID != host.ID {
		t.Fatal("imported references missing", err)
	}
	plain, err := secrets.Unprotect(loaded.Identities[0].Cipher, identity.ID)
	if err != nil || !bytes.Contains(plain, []byte("Identity-Backup-Public-Test!")) {
		t.Fatal("imported identity cannot use Linux keyring", err)
	}
	clear(plain)
	plain, err = secrets.Unprotect(loaded.Keys[0].Cipher, "key-v1:"+loaded.Keys[0].ID)
	var key vault.KeySecret
	if err != nil || json.Unmarshal(plain, &key) != nil || string(key.PrivateKey) != privateKey {
		t.Fatal("imported managed key cannot use Linux keyring", err)
	}
	clear(plain)
	if string(loaded.Identities[0].Cipher[:4]) != "SCL1" || string(loaded.Keys[0].Cipher[:4]) != "SCL1" {
		t.Fatal("Linux import did not use OS credential protector")
	}
}

func TestLinuxSavedIdentityRealSSHAndSFTP(t *testing.T) {
	requireCurrentUserCredentialTests(t)
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" || os.Getenv("YAARE_OPENSSH_FINGERPRINT") == "" {
		t.Skip("requires verified local OpenSSH fixture")
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := sqlite.New(func() (string, error) { return root, nil })
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	service := NewIdentityService(store, NewPersistentKeyService(store))
	if err := service.save(ctx, SaveIdentityRequest{ID: "linux-real-identity", Name: "Linux test", Username: "yaaretest", Authentication: "password", ReplaceSecret: true, Password: "Yaare-OpenSSH-Test-Only!"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = sqlite.New(func() (string, error) { return root, nil })
	defer store.Close()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	service = NewIdentityService(store, NewPersistentKeyService(store))
	identity, secret, err := service.resolve(ctx, "linux-real-identity")
	if err != nil {
		t.Fatal(err)
	}
	cfg := sshclient.Config{Host: "127.0.0.1", Port: 22222, Username: identity.Username, Authentication: "password", Password: secret.Password, Timeout: 10 * time.Second}
	secret.Password = nil
	events := make(chan sshclient.Event, 128)
	session := sshclient.NewSession(ctx, &sshclient.TrustStore{Path: func() (string, error) { return filepath.Join(root, "known_hosts"), nil }}, func(event sshclient.Event) error {
		select {
		case events <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	defer session.Close()
	go session.Run(&cfg)
connected:
	for {
		select {
		case event := <-events:
			switch event.Type {
			case "hostkey":
				if !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+event.Challenge.Fingerprint+",") {
					t.Fatal("unexpected host fingerprint")
				}
				if !session.Resolve(event.Challenge.ID, "once") {
					t.Fatal("trust challenge rejected")
				}
			case "connected":
				break connected
			case "failed":
				t.Fatal("saved Linux identity did not connect", event.Code)
			}
		case <-ctx.Done():
			t.Fatal("saved Linux identity timed out")
		}
	}
	client, closeFiles, err := session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFiles()
	remote := fmt.Sprintf("/home/yaaretest/files/yaare-linux-credential-%d.txt", time.Now().UnixNano())
	defer client.Remove(remote)
	written := bytes.Repeat([]byte("Linux 保存身份 · 中文 · 日本語\n"), 2048)
	file, err := client.Create(remote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(written); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	file, err = client.Open(remote)
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.ReadAll(file)
	file.Close()
	if err != nil || sha256.Sum256(read) != sha256.Sum256(written) {
		t.Fatal("Linux SFTP bytes differ", err)
	}
	if !session.Write([]byte("printf 'YAARE%s\\n' 'SH_LINUX_SAVED_IDENTITY_OK'\n")) {
		t.Fatal("PTY not usable after SFTP")
	}
	var output bytes.Buffer
	for {
		select {
		case event := <-events:
			if event.Type == "output" {
				output.Write(event.Data)
				if strings.Contains(output.String(), "YAARE_LINUX_SAVED_IDENTITY_OK") {
					return
				}
			}
		case <-ctx.Done():
			t.Fatal("PTY output missing after SFTP")
		}
	}
}
