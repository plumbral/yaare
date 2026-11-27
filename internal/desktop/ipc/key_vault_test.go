//go:build windows || linux

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yaare/internal/app/vault"

	cryptossh "golang.org/x/crypto/ssh"
	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

func TestGeneratedKeyDraftOwnershipAndSavedPublicKey(t *testing.T) {
	requireCurrentUserCredentialTests(t)
	s := NewKeyService()
	owner := testOwner(context.Background())
	other := testOwner(context.Background())
	for range 2 {
		draft, err := s.Generate(owner)
		if err != nil || !strings.HasPrefix(draft.PublicKey, "ssh-ed25519 ") {
			t.Fatalf("generate: %v", err)
		}
		if keys, err := s.List(owner); err != nil || len(keys) != 0 {
			t.Fatal("draft was persisted")
		}
		r := SaveKeyRequest{Name: "Test Ed25519", Source: "generated", Token: draft.Token}
		if _, err := s.Save(other, r); err == nil {
			t.Fatal("another page used the draft")
		}
		ref, err := s.Save(owner, r)
		if err != nil || ref.Source != "stored" {
			t.Fatalf("save: %v", err)
		}
		if key, err := s.PublicKey(owner, ref.ID); err != nil || key != draft.PublicKey {
			t.Fatalf("public key mismatch: %v", err)
		}
		if _, err := s.Save(owner, r); err == nil {
			t.Fatal("draft replay accepted")
		}
		if err := s.Forget(owner, ref.ID); err != nil {
			t.Fatal(err)
		}
	}
	draft, err := s.Generate(owner)
	if err != nil {
		t.Fatal(err)
	}
	s.Discard(owner, draft.Token)
	if _, err := s.Save(owner, SaveKeyRequest{Name: "Discarded", Source: "generated", Token: draft.Token}); err == nil {
		t.Fatal("discarded draft saved")
	}
}

func TestConnectionSavesPastedKeyAtomicallyAndReusesPassphrase(t *testing.T) {
	protector := passwordProtector(t, 71)
	store, svc, ctx, _ := passwordFixture(t, protector)
	host := passwordHost()
	host.Authentication = "key"
	passphrase := "test-import-passphrase"
	key := syntheticKey(t, passphrase)
	rev := store.Load(ctx).Snapshot.Revision
	bad := SaveConnectionRequest{Revision: rev, Host: host, PrivateKey: key, Passphrase: "wrong"}
	if _, err := svc.SaveConnection(ctx, bad); err == nil {
		t.Fatal("incorrect passphrase accepted")
	}
	if keys, err := store.Keys(ctx); err != nil || len(keys) != 0 {
		t.Fatal("failed save left a key")
	}
	saved, err := svc.SaveConnection(ctx, SaveConnectionRequest{Revision: rev, Host: host, PrivateKey: key, Passphrase: passphrase})
	if err != nil || len(saved.Snapshot.Hosts) != 1 || saved.Snapshot.Hosts[0].KeyID == "" {
		t.Fatalf("save host and key: %v", err)
	}
	host = saved.Snapshot.Hosts[0]
	keys := NewPersistentKeyService(store)
	keys.protector = protector
	ssh, closeSSH := NewSSHService(keys)
	defer closeSSH()
	ssh.credentialProtector = protector
	ConfigureSSHWorkspace(ssh, svc.application)
	req := passwordRequest(host)
	req.UseSavedCredential = false
	cfg, release, err := ssh.resolveSSHConfig(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.ValidateMaterial(cfg.PrivateKey, cfg.Passphrase); err != nil {
		t.Fatal(err)
	}
	release()
	updated, err := svc.SaveConnection(ctx, SaveConnectionRequest{Revision: saved.Snapshot.Revision, Host: host, Passphrase: passphrase})
	if err != nil || !updated.Snapshot.Hosts[0].HasCredential {
		t.Fatalf("save scoped passphrase: %v", err)
	}
	req.UseSavedCredential = true
	cfg, release, err = ssh.resolveSSHConfig(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if string(cfg.Passphrase) != passphrase {
		t.Fatal("saved connection passphrase was not loaded")
	}
}

func TestGeneratedKeyCommitsWithConnectionOrIdentity(t *testing.T) {
	protector := passwordProtector(t, 83)
	store, connections, ctx, _ := passwordFixture(t, protector)
	keys := NewPersistentKeyService(store)
	keys.protector = protector
	ConfigureWorkspaceKeys(connections, keys)
	identities := NewIdentityService(store, keys)
	identities.protector = protector

	hostDraft, err := keys.Generate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	host := passwordHost()
	host.Authentication = "key"
	req := SaveConnectionRequest{Revision: store.Load(ctx).Snapshot.Revision, Host: host, GeneratedKeyToken: hostDraft.Token}
	if _, err = connections.SaveConnection(testOwner(context.Background()), req); err == nil {
		t.Fatal("a foreign page used a generated draft")
	}
	stale := req
	stale.Revision = req.Revision + 99
	if _, err = connections.SaveConnection(ctx, stale); err == nil {
		t.Fatal("stale save succeeded")
	}
	result, err := connections.SaveConnection(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if public, err := keys.PublicKey(ctx, result.Snapshot.Hosts[0].KeyID); err != nil || public != hostDraft.PublicKey {
		t.Fatal("connection must link the generated key", err)
	}
	req.Revision = result.Snapshot.Revision
	if _, err = connections.SaveConnection(ctx, req); err == nil {
		t.Fatal("the committed draft was reused")
	}

	identityDraft, err := keys.Generate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := workspace.NewID()
	if err = identities.Save(ctx, SaveIdentityRequest{ID: id, Name: "generated identity", Username: "root",
		Authentication: "key", ReplaceSecret: true, GeneratedKeyToken: identityDraft.Token}); err != nil {
		t.Fatal(err)
	}
	identity, err := store.Identity(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if public, err := keys.PublicKey(ctx, identity.KeyID); err != nil || public != identityDraft.PublicKey {
		t.Fatal("identity must link the generated key", err)
	}
	if err := keys.CopyPrivateKey(ctx, identity.KeyID); err == nil {
		t.Fatal("browser caller could write the desktop clipboard")
	}
}

func TestConnectionSavesSelectedEncryptedFileAtomically(t *testing.T) {
	store, svc, ctx, _ := passwordFixture(t, passwordProtector(t, 72))
	keys := NewPersistentKeyService(store)
	ConfigureWorkspaceKeys(svc, keys)
	host := passwordHost()
	host.Authentication = "key"
	passphrase := "test-selected-file-passphrase"
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, []byte(syntheticKey(t, passphrase)), 0600); err != nil {
		t.Fatal(err)
	}
	draft, err := keys.rememberSelection(callerPage(ctx), keySelection{
		KeySelection: KeySelection{Token: workspace.NewID()}, path: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := SaveConnectionRequest{Revision: store.Load(ctx).Snapshot.Revision, Host: host, KeyToken: draft.Token}
	if _, err = svc.SaveConnection(ctx, request); err == nil {
		t.Fatal("encrypted key accepted without a passphrase")
	}
	request.Passphrase = passphrase
	result, err := svc.SaveConnection(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.CloudRead(ctx)
	if err != nil || len(data.Keys) != 1 || data.Keys[0].Path != "" || result.Snapshot.Hosts[0].KeyID != data.Keys[0].ID {
		t.Fatal("selected file was not stored as a portable key", err)
	}
	if keys.selectedFile(ctx, draft.Token) != "" {
		t.Fatal("committed file selection could be replayed")
	}
}

func TestTemporaryFileSelectionDoesNotRegisterKey(t *testing.T) {
	service := NewKeyService()
	owner := testOwner(context.Background())
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, []byte(syntheticKey(t, "")), 0600); err != nil {
		t.Fatal(err)
	}
	draft, err := service.rememberSelection(callerPage(owner), keySelection{
		KeySelection: KeySelection{Token: "fixture-token"}, path: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	ssh, closeSSH := NewSSHService(service)
	defer closeSSH()
	req := SSHConnectRequest{Authentication: "key", TemporaryKeyToken: draft.Token}
	if _, _, err := ssh.resolveSSHConfig(testOwner(context.Background()), req); err == nil {
		t.Fatal("temporary file used by another page")
	}
	cfg, release, err := ssh.resolveSSHConfig(owner, req)
	if err != nil || cfg.PrivateKeyPath != path || len(listed(t, service)) != 0 {
		t.Fatalf("temporary file selection was not isolated: %v", err)
	}
	release()
	if _, _, err := ssh.resolveSSHConfig(owner, req); err == nil {
		t.Fatal("temporary file token reused")
	}
}

func TestKeyVaultProtectionRestartIdentityReuseAndLeases(t *testing.T) {
	requireCurrentUserCredentialTests(t)
	ctx := context.Background()
	dir := t.TempDir()
	store := sqlite.New(func() (string, error) { return dir, nil })
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewPersistentKeyService(store)
	private := syntheticKey(t, "")
	saved, err := service.saveKey(ctx, SaveKeyRequest{Name: "粘贴密钥", Source: "text", PrivateKey: private}, "")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Source != "stored" || !saved.Available {
		t.Fatal("saved metadata invalid")
	}
	serialized, _ := json.Marshal(listed(t, service))
	if bytes.Contains(serialized, []byte("PRIVATE KEY")) || bytes.Contains(serialized, []byte("cipher")) || bytes.Contains(serialized, []byte(dir)) {
		t.Fatal("secret or path crossed metadata boundary")
	}
	keys, err := store.Keys(ctx)
	if err != nil || len(keys) != 1 || len(keys[0].Cipher) == 0 || keys[0].Path != "" {
		t.Fatal("pasted key not sealed")
	}
	if _, err = secrets.Unprotect(keys[0].Cipher, "key-v1:other"); err == nil {
		t.Fatal("cipher not bound to key ID")
	}
	path, material, passphrase, release, err := service.acquireMaterial(saved.ID)
	if err != nil || path != "" || len(passphrase) != 0 || string(material) != string(bytes.TrimSpace([]byte(private))) {
		t.Fatal("saved key did not resolve")
	}
	if err = service.forget(ctx, saved.ID); err == nil {
		t.Fatal("active key deleted")
	}
	release()
	release()
	if !bytes.Equal(material, make([]byte, len(material))) {
		t.Fatal("released private bytes not cleared")
	}
	identities := NewIdentityService(store, service)
	if err = identities.save(ctx, SaveIdentityRequest{ID: "key-identity", Name: "复用密钥", Username: "deploy", Authentication: "key", KeyID: saved.ID, ReplaceSecret: true}); err != nil {
		t.Fatal(err)
	}
	if err = service.forget(ctx, saved.ID); err == nil {
		t.Fatal("identity-associated key deleted")
	}
	store.Close()
	if err = store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	service = NewPersistentKeyService(store)
	_, material, _, release, err = service.acquireMaterial(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cryptossh.ParsePrivateKey(material); err != nil {
		t.Fatal("persisted key unusable")
	}
	release()
	files, _ := filepath.Glob(filepath.Join(dir, "*.sqlite3"))
	backups, _ := filepath.Glob(filepath.Join(dir, "backups", "*.sqlite3"))
	files = append(files, backups...)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(private)) || bytes.Contains(data, []byte("BEGIN OPENSSH PRIVATE KEY")) {
			t.Fatal("plaintext in database or backup")
		}
	}
	if _, err = service.Save(ctx, SaveKeyRequest{Name: "unauthorized", Source: "text", PrivateKey: private}); err == nil {
		t.Fatal("IPC save without caller accepted")
	}
}
func TestKeyVaultEncryptedKeysFileImportAndInvalidInput(t *testing.T) {
	requireCurrentUserCredentialTests(t)
	ctx := context.Background()
	service := NewKeyService()
	private := syntheticKey(t, "fixture-key-passphrase")
	for _, request := range []SaveKeyRequest{
		{Name: "bad", Source: "text", PrivateKey: "ssh-ed25519 AAAA public-key"},
		{Name: "bad", Source: "text", PrivateKey: private, Passphrase: "wrong"},
		{Name: "bad", Source: "text", PrivateKey: private},
		{Name: "bad", Source: "text", Token: "unexpected", PrivateKey: private},
		{Name: "bad", Source: "file", PrivateKey: private},
	} {
		if _, err := service.saveKey(ctx, request, ""); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	if len(listed(t, service)) != 0 {
		t.Fatal("invalid inputs changed registry")
	}
	key, err := service.saveKey(ctx, SaveKeyRequest{Name: "encrypted", Source: "text", PrivateKey: private, Passphrase: "fixture-key-passphrase"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, data, password, release, err := service.acquireMaterial(key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cryptossh.ParsePrivateKeyWithPassphrase(data, password); err != nil {
		t.Fatal("encrypted saved key unusable")
	}
	release()
	if !bytes.Equal(password, make([]byte, len(password))) {
		t.Fatal("released passphrase bytes not cleared")
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err = os.WriteFile(path, []byte(private), 0600); err != nil {
		t.Fatal(err)
	}
	fileKey, err := service.saveKey(ctx, SaveKeyRequest{Name: "file", Source: "file", Passphrase: "fixture-key-passphrase"}, path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, data, password, release, err := service.acquireMaterial(fileKey.ID)
	if err != nil || resolved != "" || !bytes.Equal(data, []byte(private)) || string(password) != "fixture-key-passphrase" || fileKey.Source != "stored" {
		t.Fatal("file contents were not imported into the vault")
	}
	release()
	if original, err := os.ReadFile(path); err != nil || !bytes.Equal(original, []byte(private)) {
		t.Fatal("original file modified")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if public, err := service.PublicKey(testOwner(ctx), fileKey.ID); err != nil || !strings.HasPrefix(public, "ssh-ed25519 ") {
		t.Fatalf("imported key failed after removal of source: %v", err)
	}
}
