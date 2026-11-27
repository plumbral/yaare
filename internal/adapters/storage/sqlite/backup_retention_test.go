package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

func TestRoutineBackupCooldownAndActivityWrites(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := openTestClock(t, dir, clock)
	host := hostForTest()
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: host})
	now = now.Add(backupInterval)
	applyTest(t, store, workspace.Change{Kind: "pinHost", ID: host.ID, Pinned: true})
	applyTest(t, store, workspace.Change{Kind: "markHostUsed", ID: host.ID})
	if files, err := store.backupFiles(); err != nil || len(files) != 1 {
		t.Fatalf("activity created backups: %d, %v", len(files), err)
	}
	want := applyTest(t, store, workspace.Change{Kind: "addGroup", Name: "committed"}).Snapshot
	store.waitBackups()
	files, err := store.backupFiles()
	if err != nil || len(files) != 2 || files[0].protected {
		t.Fatalf("routine snapshot not created after interval: %d, %v", len(files), err)
	}
	latest, _, err := checkFile(testContext, files[0].path, true)
	if err != nil || !reflect.DeepEqual(want, latest) {
		t.Fatal("routine snapshot did not preserve committed state", err)
	}
	store.Close()
	now = now.Add(backupInterval - time.Second)
	store = openTestClock(t, dir, clock)
	applyTest(t, store, workspace.Change{Kind: "addGroup", Name: "after restart"})
	store.waitBackups()
	if files, err = store.backupFiles(); err != nil || len(files) != 2 {
		t.Fatalf("restart bypassed cooldown: %d, %v", len(files), err)
	}
	if err := os.Rename(filepath.Join(dir, "backups"), filepath.Join(dir, "saved-backups")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backups"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	before := store.Load(testContext).Snapshot.Revision
	applyTest(t, store, workspace.Change{Kind: "pinHost", ID: host.ID})
	after := applyTest(t, store, workspace.Change{Kind: "markHostUsed", ID: host.ID}).Snapshot
	if after.Revision != before+1 || after.Hosts[0].Pinned || after.Hosts[0].LastUsed == 0 {
		t.Fatal("backup failure blocked an activity write or markHostUsed bumped revision")
	}
	_, err = app.NewWorkspace(store).Apply(testContext, workspace.Change{Kind: "deleteHost", ID: host.ID, Revision: after.Revision})
	assertStorageError(t, err, "BACKUP_FAILED")
	if !reflect.DeepEqual(store.Load(testContext).Snapshot, after) {
		t.Fatal("deletion proceeded without a checkpoint")
	}
}

func TestBackupDailyWeeklyAndCheckpointRetention(t *testing.T) {
	now := time.Date(2027, 1, 10, 12, 0, 0, 0, time.UTC)
	store := openTestClock(t, t.TempDir(), func() time.Time { return now.AddDate(0, 0, -60) })
	seed := store.lastRoutine.path
	var index int
	want := map[string]bool{}
	add := func(created time.Time, suffix string, keep bool) string {
		t.Helper()
		index++
		name := fmt.Sprintf("backup-%s-%032x%s.sqlite3", created.Format(backupTimeLayout), index, suffix)
		path := filepath.Join(store.dir, "backups", name)
		if err := copyFile(seed, path); err != nil {
			t.Fatal(err)
		}
		want[name] = keep
		return path
	}
	for minutes := 1; minutes <= 7; minutes++ {
		add(now.Add(-time.Duration(minutes)*time.Minute), "", minutes <= 5)
	}
	for days := 1; days <= 6; days++ {
		day := now.AddDate(0, 0, -days)
		add(day, "", true)
		add(day.Add(-time.Minute), "", false)
	}
	for _, days := range []int{7, 14, 21, 28} {
		day := now.AddDate(0, 0, -days)
		add(day, "", days < 28)
		add(day.Add(-time.Minute), "", false)
	}
	add(now.AddDate(0, 0, -3).Add(-time.Hour), "-protected", true)
	add(now.AddDate(0, 0, -8), "-protected", false)
	corrupt := add(now, "", true)
	if err := os.WriteFile(corrupt, []byte("invalid snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	store.pruneBackups(testContext, now)
	files, err := store.backupFiles()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, file := range files {
		got[filepath.Base(file.path)] = true
	}
	if got[filepath.Base(seed)] {
		t.Fatal("expired seed backup was retained")
	}
	for name, keep := range want {
		if got[name] != keep {
			t.Errorf("backup %s: retained = %v, want %v", name, got[name], keep)
		}
	}
}

func TestImportAndReplaceUsePreChangeCheckpointsDuringCooldown(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	store := openTestClock(t, t.TempDir(), func() time.Time { return now })
	host := hostForTest()
	beforeImport := applyTest(t, store, workspace.Change{Kind: "saveHost", Host: host}).Snapshot
	imported := host
	imported.ID, imported.Name = "imported", "Imported host"
	if _, err := store.ImportHosts(testContext, beforeImport.Revision, []workspace.Host{imported}, nil); err != nil {
		t.Fatal(err)
	}
	cloud, err := store.CloudRead(testContext)
	if err != nil {
		t.Fatal(err)
	}
	beforeReplace := cloud.Snapshot
	cloud.Snapshot.Hosts = []workspace.Host{}
	if err := store.CloudReplace(testContext, beforeReplace.Revision, cloud); err != nil {
		t.Fatal(err)
	}
	files, err := store.backupFiles()
	if err != nil || len(files) != 3 || !files[0].protected || !files[1].protected || files[2].protected {
		t.Fatalf("expected two protected checkpoints and one routine backup: %+v, %v", files, err)
	}
	for index, want := range []workspace.Snapshot{beforeReplace, beforeImport} {
		got, _, err := checkFile(testContext, files[index].path, true)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("checkpoint %d did not capture exact pre-change state: %v", index, err)
		}
	}
}
