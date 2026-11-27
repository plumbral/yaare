package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

var testContext = context.Background()

func openTest(t *testing.T, dir string) *Store {
	t.Helper()
	return openTestClock(t, dir, time.Now)
}

func openTestClock(t *testing.T, dir string, clock func() time.Time) *Store {
	t.Helper()
	s := New(func() (string, error) { return dir, nil })
	s.now = clock
	t.Cleanup(func() { s.Close() })
	if err := s.Initialize(testContext); err != nil {
		t.Fatal(err)
	}
	if report := s.Load(testContext); !report.Ready || report.Warning != "" {
		t.Fatalf("initial report: %+v", report)
	}
	return s
}
func hostForTest() workspace.Host {
	return workspace.NormalizeHost(workspace.Host{ID: "test-host", Name: "测试主机", Address: "127.0.0.1", Port: 22, Username: "tester", Group: "生产环境"})
}
func applyTest(t *testing.T, s *Store, c workspace.Change) workspace.Result {
	t.Helper()
	c.Revision = s.Load(testContext).Snapshot.Revision
	result, err := app.NewWorkspace(s).Apply(testContext, c)
	if err != nil {
		t.Fatal(err)
	}
	if result.Warning != "" {
		t.Fatal(result.Warning)
	}
	return result
}
func TestRestartTransactionsAndBackups(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "中文 & configuration")
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	s := openTestClock(t, dir, clock)
	applyTest(t, s, workspace.Change{Kind: "saveHost", Host: hostForTest()})
	copy := applyTest(t, s, workspace.Change{Kind: "duplicateHost", ID: "test-host"})
	if copy.EntityID == "test-host" || copy.EntityID == "" {
		t.Fatal("invalid copied id")
	}
	applyTest(t, s, workspace.Change{Kind: "renameGroup", Name: "生产环境", NewName: "重命名"})
	applyTest(t, s, workspace.Change{Kind: "addGroup", Name: "空分组"})
	applyTest(t, s, workspace.Change{Kind: "saveCommands", Commands: []workspace.Command{{ID: "command", Name: "检查", Command: "printf ok"}}})
	now = now.Add(backupInterval)
	applyTest(t, s, workspace.Change{Kind: "deleteHost", ID: copy.EntityID})
	before := s.Load(testContext).Snapshot
	if before.Hosts[0].Group != "重命名" {
		t.Fatal("rename did not update host")
	}
	if _, err := app.NewWorkspace(s).Apply(testContext, workspace.Change{Kind: "deleteHost", ID: "test-host", Revision: 0}); err == nil {
		t.Fatal("stale write accepted")
	}
	if !reflect.DeepEqual(before, s.Load(testContext).Snapshot) {
		t.Fatal("rejected write changed data")
	}
	s.Close()
	s = openTestClock(t, dir, clock)
	after := s.Load(testContext).Snapshot
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restart changed configuration")
	}
	files, err := s.backupFiles()
	if err != nil || len(files) != 3 {
		t.Fatalf("retention: %d %v", len(files), err)
	}
	latest, _, err := checkFile(testContext, s.lastRoutine.path, true)
	if err != nil || !reflect.DeepEqual(after, latest) {
		t.Fatal("latest backup not committed state", err)
	}
	applyTest(t, s, workspace.Change{Kind: "deleteGroup", Name: "重命名"})
	if s.Load(testContext).Snapshot.Hosts[0].Group != workspace.Ungrouped {
		t.Fatal("group deletion lost host")
	}
}
func TestCorruptionRecoveryPreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	s := openTestClock(t, dir, func() time.Time { return now })
	now = now.Add(backupInterval)
	want := applyTest(t, s, workspace.Change{Kind: "saveHost", Host: hostForTest()}).Snapshot
	s.Close()
	primary := filepath.Join(dir, "workspace.sqlite3")
	damaged := []byte("damaged database, keep original")
	if err := os.WriteFile(primary, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(dir, "backups", "backup-"+now.UTC().Add(time.Second).Format(backupTimeLayout)+"-00000000000000000000000000000000.sqlite3")
	os.WriteFile(invalid, []byte("bad"), 0600)
	s = New(func() (string, error) { return dir, nil })
	defer s.Close()
	if s.Initialize(testContext) == nil {
		t.Fatal("corrupt accepted")
	}
	report := s.Load(testContext)
	if report.Ready || !report.CanRecover {
		t.Fatalf("report: %+v", report)
	}
	if _, err := app.NewWorkspace(s).Apply(testContext, workspace.Change{Kind: "addGroup", Name: "unsafe"}); err == nil {
		t.Fatal("corrupt database writable")
	}
	if data, _ := os.ReadFile(primary); !bytes.Equal(data, damaged) {
		t.Fatal("corruption was silently replaced")
	}
	report, err := s.Recover(testContext)
	if err != nil || !report.Ready || !reflect.DeepEqual(want, report.Snapshot) {
		t.Fatal("recovery failed", err, report)
	}
	originals, _ := filepath.Glob(filepath.Join(dir, "recovery", "original-*", "workspace.sqlite3"))
	if len(originals) == 0 {
		t.Fatal("original not archived")
	}
	if data, _ := os.ReadFile(originals[0]); !bytes.Equal(data, damaged) {
		t.Fatal("archive changed")
	}
}
func TestLockMissingAndNewerVersionFailClosed(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	other := New(func() (string, error) { return dir, nil })
	defer other.Close()
	if other.Initialize(testContext) == nil || other.Load(testContext).Code != "STORAGE_IN_USE" {
		t.Fatal("second writer acquired store")
	}
	s.Close()
	if err := other.Initialize(testContext); err != nil {
		t.Fatal("OS lease not released", err)
	}
	other.Close()
	primary := filepath.Join(dir, "workspace.sqlite3")
	os.Remove(primary)
	missing := New(func() (string, error) { return dir, nil })
	if missing.Initialize(testContext) == nil || missing.Load(testContext).Code != "DATABASE_MISSING" {
		t.Fatal("missing database reset")
	}
	if report, err := missing.Recover(testContext); err != nil || !report.Ready {
		t.Fatal("missing recovery", err)
	}
	_, err := missing.db.Exec("PRAGMA user_version=99")
	if err != nil {
		t.Fatal(err)
	}
	missing.Close()
	newer := New(func() (string, error) { return dir, nil })
	defer newer.Close()
	if newer.Initialize(testContext) == nil || newer.Load(testContext).Code != "NEWER_VERSION" {
		t.Fatal("future schema accepted")
	}
	if _, err := newer.Recover(testContext); err == nil {
		t.Fatal("future schema overwritten")
	}
}
func TestKeyReferencesNoSecretsAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	path := filepath.Join(dir, "identity")
	secret := "NEVER_COPY_PRIVATE_KEY_OR_PASSWORD"
	os.WriteFile(path, []byte(secret), 0600)
	key := workspace.SavedKey{ID: "key", Name: "identity", Format: "OpenSSH", Path: path}
	registered, err := s.RegisterKey(testContext, key)
	if err != nil {
		t.Fatal(err)
	}
	h := hostForTest()
	h.Authentication = "key"
	h.KeyID = registered.ID
	applyTest(t, s, workspace.Change{Kind: "saveHost", Host: h})
	if err := s.ForgetKey(testContext, key.ID); err == nil {
		t.Fatal("associated key forgotten")
	}
	before := s.Load(testContext).Snapshot
	s.Close()
	os.Remove(path)
	s = openTest(t, dir)
	if !reflect.DeepEqual(before, s.Load(testContext).Snapshot) {
		t.Fatal("missing key erased host configuration")
	}
	keys, err := s.Keys(testContext)
	if err != nil || keys[0].Path != path {
		t.Fatal("key reference not restored")
	}
	public, _ := json.Marshal(s.Load(testContext))
	if strings.Contains(string(public), path) || strings.Contains(string(public), "password\"") {
		t.Fatal("sensitive fields in public snapshot")
	}
	s.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "backups", "*.sqlite3"))
	files = append(files, filepath.Join(dir, "workspace.sqlite3"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("key material in database")
		}
	}
}
func TestInterruptedRecoveryAndBackupFailure(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	applyTest(t, s, workspace.Change{Kind: "saveHost", Host: hostForTest()})
	s.Close()
	os.WriteFile(filepath.Join(dir, "recovery.pending"), []byte("interrupted"), 0600)
	s = New(func() (string, error) { return dir, nil })
	defer s.Close()
	if s.Initialize(testContext) == nil || s.Load(testContext).Code != "RECOVERY_INTERRUPTED" {
		t.Fatal("interrupted restore silently opened")
	}
	if report, err := s.Recover(testContext); err != nil || !report.Ready {
		t.Fatal("interrupted restore could not resume", err)
	}
	backupDir := filepath.Join(dir, "backups")
	archived := filepath.Join(dir, "saved-backups")
	if err := os.Rename(backupDir, archived); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupDir, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	before := s.Load(testContext).Snapshot
	_, err := app.NewWorkspace(s).Apply(testContext, workspace.Change{Kind: "addGroup", Name: "rejected", Revision: before.Revision})
	if err == nil || !reflect.DeepEqual(before, s.Load(testContext).Snapshot) {
		t.Fatal("backup failure accepted write")
	}
}
