package workspace

import (
	"strings"
	"testing"
)

func TestHostGroupPathsUseTheSameRulesWhenReadAndWritten(t *testing.T) {
	for _, name := range []string{"/Root", "A//B", "..", "A/../B", Ungrouped + "/child"} {
		s := Empty()
		if _, _, err := Next(s, Change{Kind: "addGroup", Name: name}, nil); err == nil {
			t.Errorf("created invalid host group %q", name)
		}
		s.Groups = append(s.Groups, name)
		if err := Validate(s, nil); err == nil {
			t.Errorf("loaded invalid host group %q", name)
		}
	}
	s := Empty()
	s.CommandGroups = append(s.CommandGroups, "Tools/Logs")
	if err := Validate(s, nil); err != nil {
		t.Fatal("command group labels may contain slashes", err)
	}
}

func TestHierarchicalGroupCascadeAndHostMetadata(t *testing.T) {
	s := Empty()
	s.Hosts = []Host{{ID: "h", Name: "Host", Address: "127.0.0.1", Port: 22, Username: "user", Group: Ungrouped, System: "Linux", Icon: "linux", Authentication: "password", Notes: "search note"}}
	next, _, err := Next(s, Change{Kind: "addGroup", Name: "Prod/Databases/Primary"}, nil)
	if err != nil || len(next.Groups) != 4 {
		t.Fatal(next.Groups, err)
	}
	host := next.Hosts[0]
	host.Group = "Prod/Databases/Primary"
	next, _, err = Next(next, Change{Kind: "saveHost", Host: host}, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, _, err = Next(next, Change{Kind: "pinHost", ID: "h", Pinned: true}, nil)
	if err != nil || !next.Hosts[0].Pinned {
		t.Fatal(err)
	}
	next, _, err = Next(next, Change{Kind: "markHostUsed", ID: "h"}, nil)
	if err != nil || next.Hosts[0].LastUsed == 0 {
		t.Fatal(err)
	}
	next, _, err = Next(next, Change{Kind: "renameGroup", Name: "Prod", NewName: "Production"}, nil)
	if err != nil || next.Hosts[0].Group != "Production/Databases/Primary" {
		t.Fatal(err)
	}
	if _, _, err = Next(next, Change{Kind: "renameGroup", Name: "Production", NewName: "Production/Child"}, nil); err == nil {
		t.Fatal("cycle accepted")
	}
	next, _, err = Next(next, Change{Kind: "deleteGroup", Name: "Production"}, nil)
	if err != nil || len(next.Groups) != 1 || next.Hosts[0].Group != Ungrouped || next.Hosts[0].Notes != "search note" || !next.Hosts[0].Pinned || next.Hosts[0].LastUsed == 0 {
		t.Fatal(next, err)
	}
	if ValidHostGroup("A//B") || ValidHostGroup(strings.Repeat("x/", 8)+"x") || ValidHostGroup("未分组/child") {
		t.Fatal("invalid hierarchy accepted")
	}
}
