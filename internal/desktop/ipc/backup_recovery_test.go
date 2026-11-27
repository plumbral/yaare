package ipc

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"
)

func TestBackupRestoreIndependentOfLostWorkspaceKey(t *testing.T) {
	s, store, ctx, parent := backupFixture(t, 21)
	host, _, _ := seedBackup(t, s, ctx, "original")
	blob, err := s.exportBytes(ctx, BackupExportRequest{Password: backupPassword, IncludeCredentials: true})
	if err != nil {
		t.Fatal(err)
	}
	file := chooseBackup(t, s, ctx, blob)
	version := store.Load(ctx).Snapshot.Revision
	original := s.cloud.protector
	other, _ := newTestVault(bytes.Repeat([]byte{92}, 32))
	s.cloud.protector = other
	if err = store.VerifyCredentialKey(other, nil); !errors.Is(err, secrets.ErrWorkspaceKey) {
		t.Fatalf("legacy workspace accepted a replacement key: %v", err)
	}
	if err = store.VerifyCredentialKey(original, nil); err != nil {
		t.Fatalf("legacy records should enroll their original key: %v", err)
	}
	if err = store.VerifyCredentialKey(other, nil); !errors.Is(err, secrets.ErrWorkspaceKey) {
		t.Fatalf("marker accepted a replacement key: %v", err)
	}
	if _, err = s.Preview(ctx, file.ID, backupPassword); err == nil {
		t.Fatal("overwriting a workspace without a usable rollback copy must fail")
	}
	envelope, _, err := s.unlockSelection(ctx, file.ID, []byte(backupPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer syncmodel.ClearPayload(envelope.Workspace)
	dir, err := restoreNewWorkspace(ctx, parent, envelope.Workspace, other)
	if err != nil {
		t.Fatal(err)
	}
	recovered := sqlite.New(func() (string, error) { return dir, nil })
	defer recovered.Close()
	if err := recovered.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	payload, _, err := syncapp.NewPortableWorkspace(recovered, other).Capture(ctx, true)
	defer syncmodel.ClearPayload(payload)
	if err != nil || len(payload.Identities) != 1 || len(payload.Keys) != 1 || len(payload.Snapshot.Hosts) != 1 || payload.Snapshot.Hosts[0].ID != host.ID {
		t.Fatalf("recovered database could not reopen and unlock its credentials: %v", err)
	}
	if _, _, err := syncapp.NewPortableWorkspace(recovered, original).Capture(ctx, true); err == nil {
		t.Fatal("the new workspace must use the destination key")
	}
	if store.Load(ctx).Snapshot.Revision != version {
		t.Fatal("recovering into another directory changed the original workspace")
	}
	if _, err := s.RestoreNewWorkspace(ctx, file.ID, backupPassword); err == nil {
		t.Fatal("non-native caller must never choose a destination or start a process")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("expected one new workspace: %v", err)
	}
}
