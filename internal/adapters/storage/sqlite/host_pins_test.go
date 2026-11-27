package sqlite

import (
	"reflect"
	"testing"
	"yaare/internal/domain/workspace"
)

func TestPinCompatibilityKeepsStoredOrderAcrossRestartAndBatch(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	for _, id := range []string{"a", "b", "c"} {
		h := hostForTest()
		h.ID = id
		h.Name = id
		h.Pinned = id == "b"
		applyTest(t, s, workspace.Change{Kind: "saveHost", Host: h})
	}
	applyTest(t, s, workspace.Change{Kind: "pinHost", ID: "c", Pinned: true})
	s.Close()
	s = openTest(t, dir)
	snapshot := s.Load(testContext).Snapshot
	ids := []string{}
	for _, h := range snapshot.Hosts {
		ids = append(ids, h.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a", "b", "c"}) || !snapshot.Hosts[1].Pinned || !snapshot.Hosts[2].Pinned {
		t.Fatal("pinning changed stored order or failed to preserve old pinned flags")
	}
	for i := range snapshot.Hosts {
		snapshot.Hosts[i].Pinned = false
	}
	applyTest(t, s, workspace.Change{Kind: "saveHosts", Hosts: snapshot.Hosts})
	s.Close()
	s = openTest(t, dir)
	actual := s.Load(testContext).Snapshot.Hosts
	if len(actual) != len(ids) {
		t.Fatal("batch unpin changed the host count")
	}
	for i, h := range actual {
		if h.Pinned || h.ID != ids[i] {
			t.Fatal("batch unpin failed to persist without reordering")
		}
	}
}
