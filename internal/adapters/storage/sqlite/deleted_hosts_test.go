package sqlite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

func assertStorageError(t *testing.T, err error, code string) {
	t.Helper()
	var failure *workspace.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestDeletedHostCandidatesSurviveBurstAndUseLatestVerifiedSnapshot(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := openTestClock(t, dir, clock)
	const count = 9
	var original workspace.Host
	for index := range count {
		host := hostForTest()
		host.ID = fmt.Sprintf("deleted-%d", index)
		host.Notes = "before deletion"
		applyTest(t, store, workspace.Change{Kind: "saveHost", Host: host})
		now = now.Add(time.Second)
		applyTest(t, store, workspace.Change{Kind: "deleteHost", ID: host.ID})
		if index == 0 {
			original = host
		}
	}
	service := app.NewWorkspace(store)
	candidates, err := service.DeletedHosts(testContext)
	if err != nil || len(candidates) != count {
		t.Fatalf("burst truncated deletion history: %d, %v", len(candidates), err)
	}
	first := candidates[len(candidates)-1]
	if first.Host.ID != original.ID {
		t.Fatal("candidates are not in newest-first order")
	}
	if _, err := time.Parse(time.RFC3339, first.Created); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RestoreDeletedHost(testContext, first.BackupID, first.Host.ID, store.Load(testContext).Snapshot.Revision); err != nil {
		t.Fatal(err)
	}
	original.Notes = "latest edit"
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: original})
	now = now.Add(time.Second)
	applyTest(t, store, workspace.Change{Kind: "deleteHost", ID: original.ID})
	latest := store.lastBackup
	store.Close()
	now = now.Add(time.Second)
	store = openTestClock(t, dir, clock)
	service = app.NewWorkspace(store)
	candidates, err = service.DeletedHosts(testContext)
	if err != nil || len(candidates) != count || candidates[0].BackupID == first.BackupID || candidates[0].Host.Notes != original.Notes {
		t.Fatalf("candidates did not keep latest deleted host version: %v", err)
	}
	if err := os.WriteFile(latest.path, []byte("damaged backup"), 0600); err != nil {
		t.Fatal(err)
	}
	current := store.Load(testContext).Snapshot
	_, err = service.RestoreDeletedHost(testContext, latest.id, original.ID, current.Revision)
	assertStorageError(t, err, "INVALID_BACKUP")
	_, err = service.RestoreDeletedHost(testContext, latest.path, original.ID, current.Revision)
	assertStorageError(t, err, "BACKUP_NOT_FOUND")
	if !reflect.DeepEqual(current, store.Load(testContext).Snapshot) {
		t.Fatal("invalid restoration modified the workspace")
	}
	candidates, err = service.DeletedHosts(testContext)
	if err != nil || len(candidates) != count || candidates[len(candidates)-1].BackupID != first.BackupID {
		t.Fatal("corrupt latest snapshot masked older valid candidate", err)
	}
}

func TestDeletedHostRestoreCopiesExactScopedCredentialsAtomically(t *testing.T) {
	store := openTest(t, t.TempDir())
	service := app.NewWorkspace(store)
	host := hostForTest()
	host.Authentication = "password"
	host.Proxy = workspace.ProxyConfig{Kind: "socks5", Address: "127.0.0.2", Port: 1080, UseAuthentication: true, Username: "proxy-user"}
	password := workspace.HostCredential{HostID: host.ID, Scope: workspace.CredentialScope(host), Cipher: []byte("protected-host-bytes")}
	proxy := workspace.HostCredential{HostID: host.ID, Scope: workspace.ProxyCredentialScope(host.ID, host.Proxy), Cipher: []byte("protected-proxy-bytes")}
	if _, err := service.SaveConnection(testContext, workspace.Change{Kind: "saveHost", Host: host, Revision: store.Load(testContext).Snapshot.Revision}, workspace.HostCredentialChange{HostID: host.ID, Action: "replace", Credential: password, Proxy: &proxy}); err != nil {
		t.Fatal(err)
	}
	deleted := applyTest(t, store, workspace.Change{Kind: "deleteHost", ID: host.ID}).Snapshot
	candidates, err := service.DeletedHosts(testContext)
	if err != nil || len(candidates) != 1 || !candidates[0].Host.HasCredential {
		t.Fatal("missing deleted credential metadata", err)
	}
	public, err := json.Marshal(candidates)
	if err != nil || bytes.Contains(public, password.Cipher) || bytes.Contains(public, proxy.Cipher) || bytes.Contains(public, []byte(".sqlite3")) {
		t.Fatal("recovery candidate exposed secrets or local backup filenames", err)
	}
	if _, err := store.db.ExecContext(testContext, "CREATE TRIGGER test_credential_failure BEFORE INSERT ON host_credentials BEGIN SELECT RAISE(ABORT, 'stop'); END"); err != nil {
		t.Fatal(err)
	}
	_, err = service.RestoreDeletedHost(testContext, candidates[0].BackupID, host.ID, deleted.Revision)
	assertStorageError(t, err, "SAVE_FAILED")
	if !reflect.DeepEqual(deleted, store.Load(testContext).Snapshot) {
		t.Fatal("failed credential write committed a partial recovery")
	}
	if _, err := store.db.ExecContext(testContext, "DROP TRIGGER test_credential_failure"); err != nil {
		t.Fatal(err)
	}
	result, err := service.RestoreDeletedHost(testContext, candidates[0].BackupID, host.ID, deleted.Revision)
	if err != nil || result.EntityID != host.ID || len(result.Snapshot.Hosts) != 1 || !result.Snapshot.Hosts[0].HasCredential || result.Snapshot.Revision != deleted.Revision+1 {
		t.Fatal("recovery did not commit host with credential availability", err)
	}
	actual, hostErr := store.HostCredential(testContext, host.ID)
	actualProxy, proxyErr := store.ProxyCredential(testContext, host.ID)
	if hostErr != nil || proxyErr != nil || !reflect.DeepEqual(actual, password) || !reflect.DeepEqual(actualProxy, proxy) {
		t.Fatal("recovered credentials differ from the scoped encrypted records", hostErr, proxyErr)
	}
}

func TestDeletedHostRestoreChecksDependenciesWithoutChangingThem(t *testing.T) {
	store := openTest(t, t.TempDir())
	service := app.NewWorkspace(store)
	key := workspace.SavedKey{ID: "saved-key", Name: "Managed key", Format: "OpenSSH", Cipher: []byte("protected-key")}
	if _, err := store.RegisterKey(testContext, key); err != nil {
		t.Fatal(err)
	}
	identity := workspace.SealedIdentity{Identity: workspace.Identity{ID: "saved-identity", Name: "Saved identity", Username: "tester", Authentication: "key", KeyID: key.ID}, Cipher: []byte("protected-identity")}
	if err := store.SaveIdentity(testContext, identity, false); err != nil {
		t.Fatal(err)
	}
	jump := hostForTest()
	jump.ID, jump.Group = "jump", workspace.Ungrouped
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: jump})
	host := hostForTest()
	host.Authentication, host.IdentityID, host.JumpHostIDs = "key", identity.ID, []string{jump.ID}
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: host})
	applyTest(t, store, workspace.Change{Kind: "deleteHost", ID: host.ID})
	candidates, err := service.DeletedHosts(testContext)
	if err != nil || len(candidates) != 1 {
		t.Fatal("missing candidate", err)
	}
	restore := func(code string) {
		t.Helper()
		before := store.Load(testContext).Snapshot
		_, err := service.RestoreDeletedHost(testContext, candidates[0].BackupID, host.ID, before.Revision)
		assertStorageError(t, err, code)
		if !reflect.DeepEqual(before, store.Load(testContext).Snapshot) {
			t.Fatal("rejection changed workspace")
		}
	}
	applyTest(t, store, workspace.Change{Kind: "deleteGroup", Name: host.Group})
	restore("RESTORE_GROUP_UNAVAILABLE")
	applyTest(t, store, workspace.Change{Kind: "addGroup", Name: host.Group})
	applyTest(t, store, workspace.Change{Kind: "deleteHost", ID: jump.ID})
	restore("RESTORE_JUMP_UNAVAILABLE")
	changedJump := jump
	changedJump.Address = "127.0.0.2"
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: changedJump})
	restore("RESTORE_JUMP_UNAVAILABLE")
	jump.Notes = "new jump note"
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: jump})
	identity.Version = 1
	if err := store.SaveIdentity(testContext, identity, true); err != nil {
		t.Fatal(err)
	}
	restore("RESTORE_IDENTITY_UNAVAILABLE")
	if err := store.ForgetKey(testContext, key.ID); err != nil {
		t.Fatal(err)
	}
	changedKey := key
	changedKey.Cipher = []byte("different-protected-key")
	if _, err := store.RegisterKey(testContext, changedKey); err != nil {
		t.Fatal(err)
	}
	identity.Version = 0
	if err := store.SaveIdentity(testContext, identity, false); err != nil {
		t.Fatal(err)
	}
	restore("RESTORE_KEY_UNAVAILABLE")
	if _, err := store.db.ExecContext(testContext, "UPDATE keys SET cipher=? WHERE id=?", key.Cipher, key.ID); err != nil {
		t.Fatal(err)
	}
	before := store.Load(testContext).Snapshot
	result, err := service.RestoreDeletedHost(testContext, candidates[0].BackupID, host.ID, before.Revision)
	if err != nil || result.EntityID != host.ID || !reflect.DeepEqual(before.Hosts, result.Snapshot.Hosts[:len(before.Hosts)]) {
		t.Fatal("compatible dependencies or current host edits were not preserved", err)
	}
}
