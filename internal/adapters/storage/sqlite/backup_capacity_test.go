package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yaare/internal/domain/preferences"
	"yaare/internal/domain/workspace"
)

func TestLargeWorkspaceReopenAndRecover(t *testing.T) {
	dir := t.TempDir()
	store := openTest(t, dir)
	blob := make([]byte, 2<<20)
	for n := range 33 {
		id := fmt.Sprintf("identity-%02d", n)
		identity := workspace.SealedIdentity{
			Identity: workspace.Identity{ID: id, Name: id, Username: "tester", Authentication: "password"},
			Cipher:   blob,
		}
		if err := store.SaveIdentity(t.Context(), identity, false); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	primary := filepath.Join(dir, "workspace.sqlite3")
	original, err := os.Stat(primary)
	if err != nil || original.Size() <= 64<<20 {
		t.Fatalf("expected a working database larger than the old limit: %v, %v", original, err)
	}
	store = openTest(t, dir)
	if identities, err := store.Identities(t.Context()); err != nil || len(identities) != 33 {
		t.Fatalf("reopened identities: %d, %v", len(identities), err)
	}
	store.Close()
	corrupt, err := os.OpenFile(primary, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = corrupt.WriteAt([]byte("bad header"), 0)
	closeErr := corrupt.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("damage fixture: %v %v", err, closeErr)
	}
	store = New(func() (string, error) { return dir, nil })
	t.Cleanup(func() { store.Close() })
	if err := store.Initialize(t.Context()); err == nil || !store.Load(t.Context()).CanRecover {
		t.Fatal("large snapshot unavailable for recovery")
	}
	if report, err := store.Recover(t.Context()); err != nil || !report.Ready {
		t.Fatalf("recover large database: %v, %s", err, report.Code)
	}
	if identities, err := store.Identities(t.Context()); err != nil || len(identities) != 33 {
		t.Fatalf("restored identities: %d, %v", len(identities), err)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "recovery", "*", "workspace.sqlite3"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("preserved original: %v, %v", archives, err)
	}
	if info, err := os.Stat(archives[0]); err != nil || info.Size() != original.Size() {
		t.Fatal("the complete damaged original was not preserved", err)
	}
}

func TestDatabasePageLimitOnEveryWriterConnection(t *testing.T) {
	for _, size := range []int{512, 4096, 65536} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.sqlite3")
			db, err := dbOpen(path, "rwc")
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(fmt.Sprintf("PRAGMA page_size=%d; CREATE TABLE example(id INTEGER PRIMARY KEY)", size))
			db.Close()
			if err != nil {
				t.Fatal(err)
			}
			db, err = dbOpen(path, "rw")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxIdleConns(0)
			for range 2 {
				var pages int
				if err = db.QueryRow("PRAGMA max_page_count").Scan(&pages); err != nil || pages != maxWorkspaceBytes/size {
					t.Fatalf("page limit: %d, %v", pages, err)
				}
			}
		})
	}
}

func TestBackupCacheDetectsReplacementWithSameSizeAndTimestamp(t *testing.T) {
	store := openTest(t, t.TempDir())
	verified := store.lastRoutine
	path := verified.path
	if err := os.Rename(path, path+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, verified.info.Size()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, verified.info.ModTime(), verified.info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if verified.unchanged() {
		t.Fatal("cached identity matched the replacement")
	}
	if _, ok := store.verifyBackupCached(t.Context(), path, true, false); ok {
		t.Fatal("cached verification accepted the replacement")
	}
}

func TestPageLimitRejectsWriteWithoutChangingWorkspace(t *testing.T) {
	store := openTest(t, t.TempDir())
	var pages int
	if err := store.db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(fmt.Sprintf("PRAGMA max_page_count=%d", pages+1)); err != nil {
		t.Fatal(err)
	}
	_, err := store.RegisterKey(t.Context(), workspace.SavedKey{
		ID: "key", Name: "key", Format: "OpenSSH", Cipher: make([]byte, 256<<10),
	})
	assertStorageError(t, err, "STORAGE_FULL")
	if keys, err := store.Keys(t.Context()); err != nil || len(keys) != 0 {
		t.Fatalf("failed transaction changed keys: %d, %v", len(keys), err)
	}
	if report := store.Load(t.Context()); !report.Ready || report.Snapshot.Revision != 0 {
		t.Fatalf("failed transaction changed the workspace: %+v", report)
	}
}

func TestCloseBacksUpIndependentSettingsWrites(t *testing.T) {
	for _, name := range []string{"preferences", "layout", "sync"} {
		t.Run(name, func(t *testing.T) {
			store := openTest(t, t.TempDir())
			var err error
			switch name {
			case "preferences":
				device := preferences.Empty().Device
				device.AutoReconnect = !device.AutoReconnect
				_, err = store.SavePreferences(t.Context(), preferences.SaveRequest{Version: preferences.Version, Device: &device})
			case "layout":
				_, err = store.SaveSessionLayout(t.Context(), layoutFixture())
			case "sync":
				err = store.SaveSync(t.Context(), SyncRecord{Settings: []byte("fixture")})
			}
			if err != nil {
				t.Fatal(err)
			}
			want, err := backupDigest(t.Context(), store.db)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			_, got, err := checkFileWithDigest(t.Context(), store.lastRoutine.path, true)
			if err != nil || got != want {
				t.Fatal("latest snapshot does not contain the settings change", err)
			}
		})
	}
}

func TestLastEditBackedUpWithoutAnotherWrite(t *testing.T) {
	creation := time.Now().UTC()
	writeTime := creation.Add(backupInterval - 200*time.Millisecond)
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart=%v", restart), func(t *testing.T) {
			dir := t.TempDir()
			store := openTestClock(t, dir, func() time.Time { return creation })
			if restart {
				store.Close()
				db, err := dbOpen(filepath.Join(dir, "workspace.sqlite3"), "rw")
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.Exec("INSERT INTO groups(name,position) VALUES('trailing-edit',1); UPDATE meta SET revision=1")
				db.Close()
				if err != nil {
					t.Fatal(err)
				}
				store = openTestClock(t, dir, func() time.Time { return writeTime })
			} else {
				store.now = func() time.Time { return writeTime }
				applyTest(t, store, workspace.Change{Kind: "addGroup", Name: "trailing-edit"})
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				store.cacheMu.Lock()
				latest := store.lastRoutine
				store.cacheMu.Unlock()
				if latest.created.After(creation) {
					got, _, err := checkFile(t.Context(), latest.path, true)
					if err != nil || got.Revision != 1 || len(got.Groups) != 2 || got.Groups[1] != "trailing-edit" {
						t.Fatalf("trailing snapshot: %+v, %v", got.Groups, err)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("no snapshot after the cooldown")
		})
	}
}
