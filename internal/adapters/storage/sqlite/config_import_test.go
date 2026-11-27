package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"yaare/internal/domain/workspace"
)

func TestImportKeysUsesLocalFilesystemCaseSemantics(t *testing.T) {
	dir := t.TempDir()
	key := workspace.SavedKey{ID: "key1", Name: "id_test", Path: filepath.Join(dir, "id_test"), Format: "OpenSSH"}
	other := key
	other.ID, other.Path = "key2", filepath.Join(dir, "ID_TEST")
	if err := os.WriteFile(key.Path, []byte("fixture A"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(other.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	same := errors.Is(err, os.ErrExist)
	if err == nil {
		f.Close()
	} else if !same {
		t.Fatal(err)
	}
	store := openTest(t, t.TempDir())
	if _, err = store.RegisterKey(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	h := importHost("a", "alpha", other.ID)
	res, err := store.ImportHosts(t.Context(), store.Load(t.Context()).Snapshot.Revision, []workspace.Host{h}, []workspace.SavedKey{other})
	if err != nil {
		t.Fatal(err)
	}
	want := other.ID
	if same {
		want = key.ID
	}
	if got := res.Snapshot.Hosts[0].KeyID; got != want {
		t.Fatalf("import bound host to %q, want %q", got, want)
	}
	if k, err := store.RegisterKey(t.Context(), other); err != nil || k.ID != want {
		t.Fatal("register and import disagree", k.ID, err)
	}
}

func importHost(id, name, key string) workspace.Host {
	return workspace.NormalizeHost(workspace.Host{ID: id, Name: name, Address: "127.0.0.1", Username: "tester", Port: 22, Group: workspace.Ungrouped, Authentication: "key", KeyID: key})
}
func TestConfigImportAtomicReferencesConflictsAndRestart(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	old := s.Load(testContext).Snapshot
	key1 := workspace.SavedKey{ID: "key1", Name: "id_test", Path: filepath.Join(dir, "id_test"), Format: "OpenSSH"}
	key2 := key1
	key2.ID = "key2"
	a, b := importHost("a", "alpha", key1.ID), importHost("b", "beta", key2.ID)
	b.JumpHostIDs = []string{a.ID}
	result, err := s.ImportHosts(testContext, old.Revision, []workspace.Host{a, b}, []workspace.SavedKey{key1, key2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Hosts) != 2 || result.Snapshot.Hosts[0].KeyID != result.Snapshot.Hosts[1].KeyID || result.Snapshot.Revision != old.Revision+1 {
		t.Fatal(result)
	}
	keys, err := s.Keys(testContext)
	if err != nil || len(keys) != 1 {
		t.Fatal(keys, err)
	}
	if _, err = s.ImportHosts(testContext, old.Revision, []workspace.Host{importHost("c", "third", "key1")}, nil); err == nil {
		t.Fatal("stale import accepted")
	}
	key3 := key1
	key3.ID = "key3"
	key3.Path = filepath.Join(dir, "other_key")
	if _, err = s.ImportHosts(testContext, result.Snapshot.Revision, []workspace.Host{importHost("c", "alpha", "key3")}, []workspace.SavedKey{key3}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	keys, _ = s.Keys(testContext)
	if len(keys) != 1 || len(s.Load(testContext).Snapshot.Hosts) != 2 {
		t.Fatal("rejected import leaked changes")
	}
	s.Close()
	s = openTest(t, dir)
	after := s.Load(testContext)
	if len(after.Snapshot.Hosts) != 2 || after.Snapshot.Hosts[1].JumpHostIDs[0] != "a" {
		t.Fatal(after)
	}
}
