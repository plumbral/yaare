package workspace

import (
	"reflect"
	"testing"
)

func orderFixture() Snapshot {
	s := Empty()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		s.Hosts = append(s.Hosts, NormalizeHost(Host{ID: id, Name: id, Address: "127.0.0.1", Port: 22, Username: "user", Group: Ungrouped, System: "Linux", Icon: "linux", Authentication: "password", Pinned: id == "b" || id == "d"}))
	}
	return s
}
func hostIDs(s Snapshot) []string {
	ids := []string{}
	for _, h := range s.Hosts {
		ids = append(ids, h.ID)
	}
	return ids
}
func TestIndependentHostOrdersAndUnpinReturn(t *testing.T) {
	s := orderFixture()
	pin, _, e := Next(s, Change{Kind: "reorderPinnedHosts", IDs: []string{"d", "b"}}, nil)
	if e != nil || !reflect.DeepEqual(pin.PinOrder, []string{"d", "b"}) || !reflect.DeepEqual(hostIDs(pin), hostIDs(s)) {
		t.Fatal(pin, e)
	}
	moved, _, e := Next(pin, Change{Kind: "reorderHosts", IDs: []string{"e", "a", "c"}}, nil)
	if e != nil || !reflect.DeepEqual(hostIDs(moved), []string{"e", "b", "a", "d", "c"}) || !reflect.DeepEqual(moved.PinOrder, pin.PinOrder) {
		t.Fatal(moved, e)
	}
	unpinned, _, e := Next(moved, Change{Kind: "pinHost", ID: "d", Pinned: false}, nil)
	if e != nil || !reflect.DeepEqual(hostIDs(unpinned), hostIDs(moved)) || !reflect.DeepEqual(unpinned.PinOrder, []string{"b"}) {
		t.Fatal(unpinned, e)
	}
	if !reflect.DeepEqual(hostIDs(s), []string{"a", "b", "c", "d", "e"}) {
		t.Fatal("mutated original snapshot")
	}
}
func TestHostReorderSubsetAndRejections(t *testing.T) {
	s := orderFixture()
	next, _, e := Next(s, Change{Kind: "reorderHosts", IDs: []string{"e", "a"}}, nil)
	if e != nil || !reflect.DeepEqual(hostIDs(next), []string{"e", "b", "c", "d", "a"}) {
		t.Fatal(next, e)
	}
	for _, change := range []Change{{Kind: "reorderHosts", IDs: []string{}}, {Kind: "reorderHosts", IDs: []string{"a", "a"}}, {Kind: "reorderHosts", IDs: []string{"a", "b"}}, {Kind: "reorderPinnedHosts", IDs: []string{"a"}}, {Kind: "reorderHosts", IDs: []string{"missing"}}, {Kind: "reorderHosts", Revision: 1, IDs: []string{"e", "a"}}} {
		got, _, err := Next(s, change, nil)
		if err == nil || !reflect.DeepEqual(got, s) {
			t.Fatal("invalid or stale move altered configuration", change, err)
		}
	}
}

func TestHostGroupOrderPreservesNamesAndMembership(t *testing.T) {
	s := orderFixture()
	s.Groups = []string{Ungrouped, "Test 1", "Test 2", "Test 10", "Test 1/Subgroup"}
	s.Hosts[0].Group = "Test 1/Subgroup"
	order := []string{Ungrouped, "Test 10", "Test 1", "Test 2", "Test 1/Subgroup"}
	next, _, err := Next(s, Change{Kind: "reorderGroups", Groups: order}, nil)
	if err != nil || !reflect.DeepEqual(next.Groups, order) || !reflect.DeepEqual(next.Hosts, s.Hosts) {
		t.Fatalf("reorder changed group data or lost its order: %v, %v", next.Groups, err)
	}
	added, _, err := Next(next, Change{Revision: next.Revision, Kind: "addGroup", Name: "Appended"}, nil)
	if err != nil || !reflect.DeepEqual(added.Groups, append(order, "Appended")) {
		t.Fatalf("new group did not append: %v, %v", added.Groups, err)
	}
	for _, bad := range [][]string{
		order[1:],
		{"Test 10", Ungrouped, "Test 1", "Test 2", "Test 1/Subgroup"},
		{Ungrouped, "Test 1", "Test 1", "Test 2", "Test 1/Subgroup"},
		{Ungrouped, "Missing", "Test 1", "Test 2", "Test 1/Subgroup"},
	} {
		got, _, err := Next(s, Change{Kind: "reorderGroups", Groups: bad}, nil)
		if err == nil || !reflect.DeepEqual(got, s) {
			t.Fatalf("invalid group permutation changed the snapshot: %v, %v", bad, err)
		}
	}
}
