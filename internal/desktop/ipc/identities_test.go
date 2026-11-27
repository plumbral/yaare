//go:build windows || linux

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app"
	"yaare/internal/app/vault"
	"yaare/internal/domain/workspace"
)

func TestSavedIdentityProtectionReuseAndReferences(t *testing.T) {
	requireCurrentUserCredentialTests(t)
	ctx := context.Background()
	dir := t.TempDir()
	store := sqlite.New(func() (string, error) { return dir, nil })
	defer store.Close()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	service := NewIdentityService(store, NewPersistentKeyService(store))
	request := SaveIdentityRequest{ID: "identity-test", Name: "部署", Username: "deploy", Authentication: "password", ReplaceSecret: true, Password: "synthetic-identity-secret-unique"}
	if err := service.save(ctx, request); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Identities(ctx)
	if err != nil || len(metadata) != 1 {
		t.Fatal("metadata missing", err)
	}
	encoded, _ := json.Marshal(metadata)
	if bytes.Contains(encoded, []byte(request.Password)) || bytes.Contains(encoded, []byte("cipher")) {
		t.Fatal("secret returned in metadata")
	}
	sealed, err := store.Identity(ctx, request.ID)
	if err != nil || bytes.Contains(sealed.Cipher, []byte(request.Password)) {
		t.Fatal("not encrypted", err)
	}
	if _, err = secrets.Unprotect(sealed.Cipher, "other-identity"); err == nil {
		t.Fatal("cipher not bound to identity")
	}
	for n := 0; n < 2; n++ {
		i, secret, err := service.resolve(ctx, request.ID)
		if err != nil || i.Username != "deploy" || string(secret.Password) != request.Password {
			t.Fatal("backend resolution failed", err)
		}
	}
	request.Version = 1
	request.ReplaceSecret = false
	request.Password = ""
	request.Name = "生产部署"
	if err = service.save(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err = service.save(ctx, request); err == nil {
		t.Fatal("stale edit accepted")
	}
	report := store.Load(ctx)
	host := workspace.NormalizeHost(workspace.Host{ID: "host-test", Name: "主机", Address: "127.0.0.1", Port: 2222, Username: "deploy", Group: workspace.Ungrouped, Authentication: "password", IdentityID: request.ID})
	if _, err = app.NewWorkspace(store).Apply(ctx, workspace.Change{Kind: "saveHost", Host: host, Revision: report.Snapshot.Revision}); err != nil {
		t.Fatal(err)
	}
	sealed, _ = store.Identity(ctx, request.ID)
	if err = store.SaveIdentity(ctx, sealed, true); err == nil {
		t.Fatal("referenced identity deleted")
	}
	if err = service.Save(ctx, request); err == nil {
		t.Fatal("IPC accepted non-desktop caller")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store = sqlite.New(func() (string, error) { return dir, nil })
	defer store.Close()
	if err = store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	service = NewIdentityService(store, NewPersistentKeyService(store))
	if _, secret, err := service.resolve(ctx, request.ID); err != nil || string(secret.Password) != "synthetic-identity-secret-unique" {
		t.Fatal("restart lost identity", err)
	}
	filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			b, _ := os.ReadFile(path)
			if bytes.Contains(b, []byte("synthetic-identity-secret-unique")) {
				t.Error("plaintext credential persisted")
			}
		}
		return nil
	})
	if err := service.save(ctx, SaveIdentityRequest{
		ID:             "pkcs8-identity",
		Name:           "PKCS8",
		Username:       "deploy",
		Authentication: "key",
		ReplaceSecret:  true,
		PrivateKey:     "-----BEGIN ENCRYPTED PRIVATE KEY-----\nsynthetic\n-----END ENCRYPTED PRIVATE KEY-----\n",
		Passphrase:     "pass",
	}); !errors.Is(err, vault.ErrEncryptedPKCS8) {
		t.Fatalf("expected vault.ErrEncryptedPKCS8, got %v", err)
	}
}
