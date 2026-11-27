package sqlite

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func installV1Fixture(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "workspace-v1.sqlite3.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMigrateAndRecoverFrozenV1(t *testing.T) {
	for _, asBackup := range []bool{false, true} {
		name := "existing workspace"
		if asBackup {
			name = "only surviving backup"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "workspace.sqlite3")
			if asBackup {
				backupDir := filepath.Join(dir, "backups")
				if err := os.Mkdir(backupDir, 0700); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(backupDir, "backup-20261120T120000.000000000Z-0123456789abcdef0123456789abcdef-protected.sqlite3")
			}
			original := installV1Fixture(t, path)
			s := New(func() (string, error) { return dir, nil })
			defer s.Close()
			err := s.Initialize(t.Context())
			if asBackup {
				if err == nil || !s.Load(t.Context()).CanRecover {
					t.Fatal("v1 snapshot must be offered for recovery", err)
				}
				if _, err = s.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
				still, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(original, still) {
					t.Fatal("recovery modified the source snapshot", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			files, err := s.backupFiles()
			if err != nil {
				t.Fatal(err)
			}
			hasOld := false
			for _, file := range files {
				db, err := openBackup(file.path)
				if err != nil {
					t.Fatal(err)
				}
				version, err := databaseVersion(t.Context(), db)
				db.Close()
				if err != nil {
					t.Fatal(err)
				}
				hasOld = hasOld || (file.protected && version == 1)
			}
			if !hasOld {
				t.Fatal("missing verified, pre-upgrade snapshot")
			}
			want := s.Load(t.Context())
			if !want.Ready || len(want.Snapshot.Hosts) != 1 || want.Snapshot.Hosts[0].Notes != "v1 fixture: preserved across upgrades" {
				t.Fatalf("lost v1 data: %+v", want)
			}
			s.Close()
			reopened := openTest(t, dir)
			if version, err := databaseVersion(t.Context(), reopened.db); err != nil || version != schemaVersion {
				t.Fatal(version, err)
			}
			if got := reopened.Load(t.Context()); got.Snapshot.Hosts[0].ID != want.Snapshot.Hosts[0].ID || got.Snapshot.Revision != want.Snapshot.Revision {
				t.Fatal("reopening changed the migrated host or revision")
			}
		})
	}
}

func TestMigrationFailureRollsBackDDLAndVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.sqlite3")
	installV1Fixture(t, path)
	db, err := dbOpen(path, "rw")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TEMP TRIGGER fail_upgrade BEFORE UPDATE ON schema_info BEGIN SELECT RAISE(ABORT, 'injected failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = applyMigrations(t.Context(), db, 1); err == nil {
		t.Fatal("expected rollback")
	}
	if version, err := databaseVersion(t.Context(), db); err != nil || version != 1 {
		t.Fatal("old version lost", version, err)
	}
	if err = validateCatalog(t.Context(), db, 1); err != nil {
		t.Fatal("old schema changed", err)
	}
	if _, err = db.Exec("DROP TRIGGER temp.fail_upgrade"); err != nil {
		t.Fatal(err)
	}
	if err = applyMigrations(t.Context(), db, 1); err != nil {
		t.Fatal("retry failed", err)
	}
}
