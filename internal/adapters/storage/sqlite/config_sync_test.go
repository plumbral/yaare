package sqlite

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/domain/workspace"
)

func TestPortableRestoreCopyAndUnwritableDirectory(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	blob, err := cloudsync.Seal([]byte("test restore point"), "Test-Only-Password!")
	if err != nil {
		t.Fatal(err)
	}
	if e := s.PreservePortable(testContext, blob); e != nil {
		t.Fatal(e)
	}
	copies, _ := filepath.Glob(filepath.Join(dir, "portable-backups", "restore-sync-*.yaare"))
	if len(copies) != 1 {
		t.Fatal("restore copy not created")
	}
	data, e := os.ReadFile(copies[0])
	if e != nil || !bytes.Equal(data, blob) {
		t.Fatal("restore copy incomplete", e)
	}
	if e = s.PreservePortable(testContext, []byte("plaintext credentials")); e == nil {
		t.Fatal("plaintext restore copy accepted")
	}
	blockedRoot := t.TempDir()
	blocked := openTest(t, blockedRoot)
	if e = os.WriteFile(filepath.Join(blockedRoot, "portable-backups"), []byte("not a directory"), 0600); e != nil {
		t.Fatal(e)
	}
	revision := blocked.Load(testContext).Snapshot.Revision
	if e = blocked.PreservePortable(testContext, blob); e == nil {
		t.Fatal("unwritable destination reported success")
	}
	if blocked.Load(testContext).Snapshot.Revision != revision {
		t.Fatal("failed preservation modified workspace")
	}
}

func TestSyncBaselineStoredExternallyWithoutBloatingBackupsAndRecoversCleanly(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	s := openTestClock(t, dir, func() time.Time { return now })

	firstBaseline := bytes.Repeat([]byte{0x5a}, 512<<10)
	if err := s.SaveSync(testContext, SyncRecord{Settings: []byte("enc-settings"), State: firstBaseline}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadSync(testContext)
	if err != nil || !bytes.Equal(got.State, firstBaseline) {
		t.Fatalf("external baseline round-trip failed: len=%d err=%v", len(got.State), err)
	}
	now = now.Add(backupInterval)
	applyTest(t, s, workspace.Change{Kind: "addGroup", Name: "baseline-check"})
	s.waitBackups()

	files, err := s.backupFiles()
	if err != nil || len(files) < 2 {
		t.Fatalf("expected routine backup: %d, %v", len(files), err)
	}
	info, err := os.Stat(files[0].path)
	if err != nil || info.Size() >= 256<<10 {
		t.Fatalf("backup was bloated by external sync baseline: size=%d err=%v", info.Size(), err)
	}

	secondBaseline := bytes.Repeat([]byte{0x3c}, 128<<10)
	got.State = secondBaseline
	if err := s.SaveSync(testContext, got); err != nil {
		t.Fatal(err)
	}
	baselines, err := filepath.Glob(filepath.Join(dir, "sync-baselines", "*.bin"))
	if err != nil || len(baselines) != 1 {
		t.Fatalf("expected exactly 1 retained baseline file, got %d (%v)", len(baselines), err)
	}

	s.Close()
	allBackups, err := s.backupFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, backup := range allBackups {
		if backup.path > files[0].path {
			if err := os.Remove(backup.path); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "workspace.sqlite3"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	reopened := New(func() (string, error) { return dir, nil })
	defer reopened.Close()
	if reopened.Initialize(testContext) == nil {
		t.Fatal("corrupt database opened")
	}
	report, err := reopened.Recover(testContext)
	if err != nil || !report.Ready {
		t.Fatalf("recover failed: %+v %v", report, err)
	}
	recoveredSync, err := reopened.ReadSync(testContext)
	if err != nil || !recoveredSync.Paused || len(recoveredSync.State) != 0 {
		t.Fatalf("recovered sync with pruned baseline was not cleanly paused: paused=%v stateLen=%d err=%v", recoveredSync.Paused, len(recoveredSync.State), err)
	}
}

func TestBackupDigestCoversAllTables(t *testing.T) {
	s := openTest(t, t.TempDir())
	baseDigest, err := backupDigest(testContext, s.db)
	if err != nil || len(baseDigest) != 64 {
		t.Fatalf("base digest failed: %q %v", baseDigest, err)
	}
	identity := workspace.SealedIdentity{
		Identity: workspace.Identity{ID: "id-digest", Name: "Digest ID", Username: "root", Authentication: "password"},
		Cipher:   []byte("secret-v1"),
	}
	if err := s.SaveIdentity(testContext, identity, false); err != nil {
		t.Fatal(err)
	}
	afterIdentity, err := backupDigest(testContext, s.db)
	if err != nil || afterIdentity == baseDigest {
		t.Fatal("backupDigest did not reflect saved identity")
	}
	if _, err := s.db.ExecContext(testContext, "UPDATE identities SET cipher=? WHERE id=?", []byte("secret-v2"), identity.ID); err != nil {
		t.Fatal(err)
	}
	afterTamper, err := backupDigest(testContext, s.db)
	if err != nil || afterTamper == afterIdentity {
		t.Fatal("backupDigest did not detect ciphertext change in identities")
	}
}
