package webserver

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestInitializeRetriesAndPreservesOwner(t *testing.T) {
	dir := t.TempDir()
	c := Config{Root: filepath.Join(dir, "data"), KeyFile: filepath.Join(dir, "keys", "instance.key"), User: "admin"}
	prompt := func() ([]byte, error) { return []byte("test owner password"), nil }
	if err := Initialize(c, func() ([]byte, error) { return []byte("short"), nil }); err == nil {
		t.Fatal("accepted a short password")
	}
	if _, err := os.Stat(filepath.Join(c.Root, "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed initialization left an owner record")
	}
	if err := Initialize(c, prompt); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(c.KeyFile)
	if err != nil || len(key) != 32 {
		t.Fatal("missing instance key", err)
	}
	unexpectedPrompt := func() ([]byte, error) {
		t.Fatal("requested a password for an existing owner")
		return nil, nil
	}
	c.User = "other-user"
	if err := Initialize(c, unexpectedPrompt); err != nil {
		t.Fatal(err)
	}
	_, auth, err := instanceCredentials(c, false, nil)
	if err != nil || auth.account.User != "admin" || bcrypt.CompareHashAndPassword(auth.account.Hash, []byte("test owner password")) != nil {
		t.Fatal("owner changed after reinitialization", err)
	}
	if err := os.Remove(c.KeyFile); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(c, unexpectedPrompt); err == nil {
		t.Fatal("accepted an existing data volume with a missing key")
	}
	if _, err := os.Stat(c.KeyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("generated a replacement key")
	}
	if err := os.WriteFile(c.KeyFile, bytes.Repeat([]byte{42}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(c, unexpectedPrompt); err == nil {
		t.Fatal("accepted another instance's key")
	}
	if err := os.WriteFile(c.KeyFile, key, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(c, unexpectedPrompt); err != nil {
		t.Fatal("original key could not reopen the instance", err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, "workspace.db"), []byte("existing data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(c.Root, "auth.json")); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(c, unexpectedPrompt); err == nil {
		t.Fatal("created a new owner for an incomplete data volume")
	}
}

func TestConcurrentInstanceInitialization(t *testing.T) {
	dir := t.TempDir()
	c := Config{Root: filepath.Join(dir, "data"), KeyFile: filepath.Join(dir, "keys", "instance.key"), User: "admin"}
	start := make(chan struct{})
	results := make(chan error, 3)
	for range cap(results) {
		go func() {
			<-start
			results <- Initialize(c, func() ([]byte, error) { return []byte("test owner password"), nil })
		}()
	}
	close(start)
	for range cap(results) {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := instanceCredentials(c, false, nil); err != nil {
		t.Fatal(err)
	}
}
