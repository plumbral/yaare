package sqlite

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
)

func TestProtectedDatabaseIsNeverChanged(t *testing.T) {
	for _, tc := range []struct {
		name        string
		id, version int
		code        string
	}{
		{"foreign", 12345, 1, "UNSUPPORTED_FORMAT"},
		{"foreign higher version", 12345, 99, "UNSUPPORTED_FORMAT"},
		{"Yaare higher version", appID, schemaVersion + 1, "NEWER_VERSION"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			s := openTest(t, root)
			files, err := s.backupFiles()
			if err != nil || len(files) == 0 {
				t.Fatal("expected a valid backup", err)
			}
			s.Close()
			primary := filepath.Join(root, "workspace.sqlite3")
			writer, err := dbOpen(primary, "rw")
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			if _, err := writer.Exec(fmt.Sprintf("PRAGMA wal_autocheckpoint=0; PRAGMA application_id=%d; PRAGMA user_version=%d", tc.id, tc.version)); err != nil {
				t.Fatal(err)
			}

			read := func(path string) []byte {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			before := map[string][]byte{}
			for _, path := range []string{primary, primary + "-wal", files[0].path} {
				before[path] = read(path)
			}

			store := New(func() (string, error) { return root, nil })
			defer store.Close()
			if err := store.Initialize(t.Context()); err == nil {
				t.Fatal("opened unsupported database")
			}
			report := store.Load(t.Context())
			if report.Code != tc.code || report.CanRecover || report.Ready {
				t.Fatal(report)
			}
			if _, err := store.Recover(t.Context()); err == nil {
				t.Fatal("replaced unsupported database")
			}
			for path, data := range before {
				if !bytes.Equal(read(path), data) {
					t.Errorf("%s changed", filepath.Base(path))
				}
			}
		})
	}
}

func TestRecoveryRechecksReplacedPrimary(t *testing.T) {
	root := t.TempDir()
	s := openTest(t, root)
	s.Close()
	path := filepath.Join(root, "workspace.sqlite3")
	if err := os.WriteFile(path, []byte("unreadable"), 0600); err != nil {
		t.Fatal(err)
	}
	store := New(func() (string, error) { return root, nil })
	defer store.Close()
	if err := store.Initialize(t.Context()); err == nil || !store.Load(t.Context()).CanRecover {
		t.Fatal("expected recovery")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	db, err := dbOpen(path, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("PRAGMA application_id=12345; PRAGMA user_version=1")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if report, err := store.Recover(t.Context()); err == nil || report.Code != "UNSUPPORTED_FORMAT" {
		t.Fatal(report, err)
	}
}

func TestStoredSchemaChecksAllIndexes(t *testing.T) {
	root := t.TempDir()
	s := openTest(t, root)
	if _, err := s.db.Exec("DROP INDEX hosts_identity"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	store := New(func() (string, error) { return root, nil })
	defer store.Close()
	if err := store.Initialize(t.Context()); err == nil || store.Load(t.Context()).Code != "CONFIG_CORRUPT" {
		t.Fatal("opened an incomplete database")
	}
}

func TestCurrentSchemaFreshReopenAndAtomicSyncPause(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	var version, id int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatal("fresh schema version", version, err)
	}
	if err := s.db.QueryRow("PRAGMA application_id").Scan(&id); err != nil || id != 0x59414152 {
		t.Fatal("unexpected initial file header", id, version, err)
	}
	if report := s.Load(testContext); !report.Ready || len(report.Snapshot.Commands) != 0 || len(report.Snapshot.CommandGroups) != 1 {
		t.Fatal("fresh database has seeded commands", report)
	}
	settings := []byte("current encrypted settings")
	if err := s.SaveSync(testContext, SyncRecord{Settings: settings}); err != nil {
		t.Fatal(err)
	}
	stale, err := s.ReadSync(testContext)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSync(testContext, stale); err != nil {
		t.Fatal(err)
	}
	before := s.Load(testContext).Snapshot
	next := syncmodel.LocalData{Snapshot: workspace.Empty()}
	if err = s.RestoreCloud(testContext, before.Revision, next, IntegrationPause{Version: stale.Version}); err == nil {
		t.Fatal("stale sync preview restored data")
	}
	current, err := s.ReadSync(testContext)
	if err != nil || current.Paused || s.Load(testContext).Snapshot.Revision != before.Revision {
		t.Fatal("rejected restore changed state", err)
	}
	if err = s.RestoreCloud(testContext, before.Revision, next, IntegrationPause{Version: current.Version}); err != nil {
		t.Fatal(err)
	}
	paused, err := s.ReadSync(testContext)
	if err != nil || !paused.Paused || !bytes.Equal(paused.Settings, settings) {
		t.Fatal("current runner not paused atomically", err)
	}
	s.Close()
	s = openTest(t, dir)
	reopened, err := s.ReadSync(testContext)
	if err != nil || !reopened.Paused {
		t.Fatal("current state did not survive restart", err)
	}
}
