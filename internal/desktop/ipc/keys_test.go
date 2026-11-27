package ipc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	cryptossh "golang.org/x/crypto/ssh"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app/vault"
)

func listed(t *testing.T, service *KeyService) []KeyReference {
	t.Helper()
	result, err := service.List(testOwner(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPersistentKeyRestartMissingAndActiveUsage(t *testing.T) {
	dir := t.TempDir()
	store := sqlite.New(func() (string, error) { return dir, nil })
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewPersistentKeyService(store)
	path := filepath.Join(dir, "identity")
	os.WriteFile(path, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nNOT-KEY-MATERIAL\n"), 0600)
	key, err := service.register(path)
	if err != nil {
		t.Fatal(err)
	}
	_, release, ok := service.acquireForSSH(key.ID)
	if !ok {
		t.Fatal("key could not be acquired")
	}
	if err := service.forget(context.Background(), key.ID); err == nil {
		t.Fatal("active key removed")
	}
	release()
	release()
	store.Close()
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	service = NewPersistentKeyService(store)
	again, err := service.register(path)
	if err != nil || again.ID != key.ID {
		t.Fatal("stable key identity lost")
	}
	os.Remove(path)
	keys := listed(t, service)
	if len(keys) != 1 || keys[0].Available {
		t.Fatal("missing key not reported")
	}
	if _, _, ok := service.acquireForSSH(key.ID); ok {
		t.Fatal("missing key acquired")
	}
	if err := service.forget(context.Background(), key.ID); err != nil {
		t.Fatal(err)
	}
	if len(listed(t, service)) != 0 {
		t.Fatal("key not forgotten")
	}
	if _, err := service.List(context.Background()); err == nil {
		t.Fatal("KeyService.List accepted unowned caller")
	}
}

func TestKeyReferenceBoundary(t *testing.T) {
	service := NewKeyService()
	dir := t.TempDir()
	path := filepath.Join(dir, "sample-key")
	if err := os.WriteFile(path, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nTEST-ONLY-NOT-A-PRIVATE-KEY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := service.register(path)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := service.register(path)
	if err != nil || duplicate.ID != key.ID || len(listed(t, service)) != 1 {
		t.Fatal("duplicate reference was not reused")
	}
	serialized, err := json.Marshal(listed(t, service))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{dir, "TEST-ONLY", "BEGIN", "path"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatal("private metadata crossed serialization boundary")
		}
	}
	copy := listed(t, service)
	copy[0].Name = "mutated"
	if listed(t, service)[0].Name != "sample-key" {
		t.Fatal("list exposed mutable registry state")
	}
	service.Forget(context.Background(), key.ID)
	if len(listed(t, service)) != 0 {
		t.Fatal("reference not removed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("forget modified the underlying file")
	}
}

func TestRejectNonPrivateKeyFiles(t *testing.T) {
	service := NewKeyService()
	dir := t.TempDir()
	for _, content := range []string{"ssh-ed25519 AAAA public-key", "", strings.Repeat("x", 300), "-----BEGIN CERTIFICATE-----\nAAAA"} {
		path := filepath.Join(dir, "invalid-key")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := service.register(path); err == nil {
			t.Fatal("non-private-key file accepted")
		} else if strings.Contains(err.Error(), dir) {
			t.Fatal("error exposed local path")
		}
	}
	if _, err := service.register(dir); err == nil {
		t.Fatal("directory accepted")
	}
	if len(listed(t, service)) != 0 {
		t.Fatal("failed picks changed the registry")
	}
}

func TestKeyPickerRejectsEncryptedPKCS8WithoutBindingSelection(t *testing.T) {
	ctx := testOwner(context.Background())
	service := NewKeyService()
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.pem")
	if err := os.WriteFile(plain, []byte("-----BEGIN PRIVATE KEY-----\nsynthetic\n"), 0600); err != nil {
		t.Fatal(err)
	}
	selected, err := service.pickFile(callerPage(ctx), plain)
	if err != nil || selected.Format != "PEM" || selected.Name != "plain.pem" {
		t.Fatal(selected, err)
	}
	if service.selectedFile(testOwner(context.Background()), selected.Token) != "" {
		t.Fatal("another page can use the picker token")
	}
	unsupported := filepath.Join(dir, "encrypted.pem")
	const encryptedPKCS8 = "-----BEGIN ENCRYPTED PRIVATE KEY-----\nsynthetic\n"
	if err := os.WriteFile(unsupported, []byte(encryptedPKCS8), 0600); err != nil {
		t.Fatal(err)
	}
	if ref, err := service.pickFile(callerPage(ctx), unsupported); ref != nil || !errors.Is(err, vault.ErrEncryptedPKCS8) {
		t.Fatal("unsupported file selected", err)
	}
	if service.selectedFile(ctx, selected.Token) == "" {
		t.Fatal("rejected file replaced a valid selection")
	}
	if ref, err := service.register(unsupported); ref != nil || !errors.Is(err, vault.ErrEncryptedPKCS8) {
		t.Fatal("unsupported file registered", err)
	}
	for _, passphrase := range []string{"", "test-passphrase"} {
		for _, source := range []string{"file", "text"} {
			path := unsupported
			request := SaveKeyRequest{Name: "unsupported", Source: source, Passphrase: passphrase}
			if source == "text" {
				path, request.PrivateKey = "", encryptedPKCS8
			}
			if ref, err := service.saveKey(ctx, request, path); ref != nil || !errors.Is(err, vault.ErrEncryptedPKCS8) {
				t.Fatal("unsupported key saved", err)
			}
		}
	}
	if len(listed(t, service)) != 0 {
		t.Fatal("rejected keys changed the registry")
	}
}

func TestKeyMaterialWrongPassphraseIsNotFormatError(t *testing.T) {
	data := []byte(syntheticKey(t, "fixture-passphrase"))
	defer clear(data)
	if err := vault.ValidateMaterial(data, []byte("wrong")); err == nil || err.Error() != "私钥口令错误。" {
		t.Fatal("did not distinguish wrong passphrase", err)
	}
	if err := vault.ValidateMaterial(data, []byte("")); !errors.Is(err, vault.ErrPassphraseMissing) {
		t.Fatal("did not distinguish missing passphrase", err)
	}
	if err := vault.ValidateMaterial(data, []byte("fixture-passphrase")); err != nil {
		t.Fatal(err)
	}
}

func syntheticKey(t *testing.T, passphrase string) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = cryptossh.MarshalPrivateKey(key, "synthetic-test-key")
	} else {
		block, err = cryptossh.MarshalPrivateKeyWithPassphrase(key, "synthetic-test-key", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}
