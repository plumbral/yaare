package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"yaare/internal/domain/workspace"
)

func TestDeletedHostRecoveryPreservesNewerConfigurationAndSavedPassword(t *testing.T) {
	_, service, ctx, _ := passwordFixture(t, passwordProtector(t, 48))
	password := "Recovery-Fixture-Only!"
	deleted := saveHostPassword(t, service, ctx, passwordHost(), password, true)
	snapshot := service.application.Load(ctx).Snapshot
	if _, err := service.Apply(ctx, workspace.Change{Kind: "deleteHost", ID: deleted.ID, Revision: snapshot.Revision}); err != nil {
		t.Fatal(err)
	}
	remaining := passwordHost()
	remaining.ID, remaining.Name = "added-after-delete", "Added after deletion"
	remaining = saveHostPassword(t, service, ctx, remaining, "Unrelated-Fixture-Only!", true)
	for index := 0; index < 12; index++ {
		snapshot = service.application.Load(ctx).Snapshot
		if _, err := service.Apply(ctx, workspace.Change{Kind: "markHostUsed", ID: remaining.ID, Revision: snapshot.Revision}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.DeletedHosts(context.Background()); err == nil {
		t.Fatal("unowned recovery list accepted")
	}
	candidates, err := service.DeletedHosts(ctx)
	if err != nil || len(candidates) != 1 || candidates[0].Host.ID != deleted.ID || !candidates[0].Host.HasCredential {
		t.Fatalf("expected one deleted host with a recoverable credential, got %+v (%v)", candidates, err)
	}
	serialized, err := json.Marshal(candidates)
	if err != nil || bytes.Contains(serialized, []byte(password)) || bytes.Contains(serialized, []byte("cipher")) || bytes.Contains(serialized, []byte(".sqlite3")) {
		t.Fatal("recovery metadata returned private details", err)
	}
	revision := service.application.Load(ctx).Snapshot.Revision
	backupID := candidates[0].BackupID
	if _, err = service.RestoreDeletedHost(context.Background(), backupID, deleted.ID, revision); err == nil {
		t.Fatal("unowned recovery accepted")
	}
	if _, err = service.RestoreDeletedHost(ctx, backupID, deleted.ID, revision-1); err == nil {
		t.Fatal("stale recovery accepted")
	}
	restored, err := service.RestoreDeletedHost(ctx, backupID, deleted.ID, revision)
	if err != nil || len(restored.Snapshot.Hosts) != 2 || restored.EntityID != deleted.ID {
		t.Fatalf("restore did not add exactly one host: %+v (%v)", restored, err)
	}
	ssh := passwordSSH(t, service)
	assertResolvedPassword(t, ssh, ctx, deleted, password)
	assertResolvedPassword(t, ssh, ctx, remaining, "Unrelated-Fixture-Only!")
	if _, err = service.RestoreDeletedHost(ctx, backupID, deleted.ID, restored.Snapshot.Revision); err == nil {
		t.Fatal("restore overwrote an existing host")
	}
}
