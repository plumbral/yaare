//go:build windows

package recovery

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"yaare/internal/adapters/secrets"
)

func TestEncryptedJournalRestartAndRecordBinding(t *testing.T) {
	dir := t.TempDir()
	root := func() (string, error) { return dir, nil }
	journal := New(root, secrets.CurrentUser{})
	if entries, err := journal.Initialize(); err != nil || len(entries) != 0 {
		t.Fatal(err)
	}
	manifest := []byte(`{"localRoot":"C:\\private\\transfer-folder","items":["a","b"]}`)
	state := []byte(`{"checkpoint":"step-1"}`)
	if err := journal.SaveManifest("task-one", manifest); err != nil {
		t.Fatal(err)
	}
	if err := journal.SaveState("task-one", state); err != nil {
		t.Fatal(err)
	}
	sealedManifest, err := os.ReadFile(filepath.Join(dir, "transfer-recovery", "task-one.manifest"))
	if err != nil || bytes.Contains(sealedManifest, []byte("transfer-folder")) {
		t.Fatal("plaintext manifest", err)
	}
	sealedState, err := os.ReadFile(filepath.Join(dir, "transfer-recovery", "task-one.state"))
	if err != nil || bytes.Contains(sealedState, []byte("step-1")) {
		t.Fatal("plaintext state", err)
	}
	restarted := New(root, secrets.CurrentUser{})
	entries, err := restarted.Initialize()
	if err != nil || len(entries) != 1 || !bytes.Equal(entries[0].Manifest, manifest) || !bytes.Equal(entries[0].State, state) {
		t.Fatal("restart failed", err)
	}
	if err = journal.SaveManifest("task-one", nil); err == nil {
		t.Fatal("empty manifest accepted")
	}
	if err = journal.SaveState("task-one", nil); err == nil {
		t.Fatal("empty state accepted")
	}
	nextState := []byte(`{"checkpoint":"step-2"}`)
	if err = journal.SaveState("task-one", nextState); err != nil {
		t.Fatal(err)
	}
	entries, err = restarted.Initialize()
	if err != nil || !bytes.Equal(entries[0].Manifest, manifest) || !bytes.Equal(entries[0].State, nextState) {
		t.Fatal("independent state update altered manifest or failed", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "transfer-recovery", "task-other.manifest"), sealedManifest, 0600); err != nil {
		t.Fatal(err)
	}
	entries, err = restarted.Initialize()
	if err == nil || len(entries) != 1 {
		t.Fatal("manifest identity was not bound")
	}
	_ = os.Remove(filepath.Join(dir, "transfer-recovery", "task-other.manifest"))
	if err = journal.Remove("task-one"); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".manifest", ".state"} {
		if _, statErr := os.Stat(filepath.Join(dir, "transfer-recovery", "task-one"+suffix)); !os.IsNotExist(statErr) {
			t.Fatalf("remove left %s behind: %v", suffix, statErr)
		}
	}
	if err = journal.Remove("../outside"); err == nil {
		t.Fatal("invalid id removed")
	}
}
