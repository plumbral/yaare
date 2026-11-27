package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"testing"
	"yaare/internal/domain/workspace"
)

func TestCloudKeyLimitPreservesReadableWorkspace(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	data, err := s.CloudRead(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	key := func(i int) workspace.SavedKey {
		return workspace.SavedKey{ID: fmt.Sprintf("key-%03d", i), Name: "test", Format: "PEM", Cipher: []byte{1}}
	}
	for i := range workspace.MaxSavedKeys {
		data.Keys = append(data.Keys, key(i))
	}
	if err := s.RestoreCloud(t.Context(), data.Snapshot.Revision, data, IntegrationPause{}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, dir)
	data, err = s.CloudRead(t.Context())
	if err != nil || len(data.Keys) != workspace.MaxSavedKeys {
		t.Fatalf("limit did not reopen: keys=%d, err=%v", len(data.Keys), err)
	}
	revision := data.Snapshot.Revision
	data.Keys = append(data.Keys, key(workspace.MaxSavedKeys))
	if err := ValidateCloudData(data); err == nil {
		t.Fatal("accepted oversized preview")
	}
	if err := s.RestoreCloud(t.Context(), revision, data, IntegrationPause{}); err == nil {
		t.Fatal("accepted oversized restore")
	}
	state, err := s.CloudRead(t.Context())
	if err != nil || len(state.Keys) != workspace.MaxSavedKeys || state.Snapshot.Revision != revision {
		t.Fatal("failed restore changed the workspace", err)
	}
}

func TestRecoveryPreservesOversizedV1KeySet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.sqlite3")
	originalV1 := installV1Fixture(t, path)
	backupDir := filepath.Join(dir, "backups")
	if err := os.Mkdir(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "backup-20261120T120000.000000000Z-0123456789abcdef0123456789abcdef-protected.sqlite3"), originalV1, 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := dbOpen(path, "rw")
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	var originalCount int
	if err := legacy.QueryRow("SELECT count(*) FROM keys").Scan(&originalCount); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`WITH RECURSIVE seq(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM seq WHERE i<?)
			INSERT INTO keys(id,name,format,path,cipher) SELECT 'old-'||i,'legacy','PEM','',X'01' FROM seq`, workspace.MaxSavedKeys+1-originalCount); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	damaged := New(func() (string, error) { return dir, nil })
	t.Cleanup(func() { damaged.Close() })
	if err := damaged.Initialize(t.Context()); err == nil || !damaged.Load(t.Context()).CanRecover {
		t.Fatal("legacy oversized database has no recovery option", err)
	}
	if report, err := damaged.Recover(t.Context()); err != nil || !report.Ready {
		t.Fatal("legacy recovery failed", report.Code, err)
	}
	restored, err := damaged.CloudRead(t.Context())
	if err != nil || len(restored.Keys) != originalCount {
		t.Fatal("pre-change checkpoint was not recovered", err)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "recovery", "*", "workspace.sqlite3"))
	if err != nil || len(archives) != 1 {
		t.Fatal("missing preserved original", archives, err)
	}
	original, err := dbOpen(archives[0], "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	var count int
	if err := original.QueryRow("SELECT count(*) FROM keys").Scan(&count); err != nil || count != workspace.MaxSavedKeys+1 {
		t.Fatal("original keys were truncated", count, err)
	}
}

func TestCloudReplaceRollsBackAllRowsOnWriteFailure(t *testing.T) {
	s := openTest(t, t.TempDir())
	applyTest(t, s, workspace.Change{Kind: "saveHost", Host: hostForTest()})
	before, e := s.CloudRead(testContext)
	if e != nil {
		t.Fatal(e)
	}
	next, e := s.CloudRead(testContext)
	if e != nil {
		t.Fatal(e)
	}
	next.Snapshot.Hosts[0].Name = "must roll back"
	if _, e = s.db.Exec("CREATE TRIGGER reject_cloud_update BEFORE UPDATE ON meta BEGIN SELECT RAISE(ABORT,'fixture failure'); END"); e != nil {
		t.Fatal(e)
	}
	if e = s.CloudReplace(context.Background(), before.Snapshot.Revision, next); e == nil {
		t.Fatal("forced failure accepted")
	}
	after, e := s.CloudRead(testContext)
	if e != nil || after.Snapshot.Hosts[0].Name != before.Snapshot.Hosts[0].Name || after.Snapshot.Revision != before.Snapshot.Revision {
		t.Fatal("partial replacement", after, e)
	}
}
