package workspace

import (
	"strings"
	"testing"
)

func TestCommandLibraryPreservesSourceAndIndependentGroups(t *testing.T) {
	original := Empty()
	next, _, err := Next(original, Change{Kind: "addCommandGroup", Name: "Docker"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := "\n# 容器\n  printf 'hello'\n\n\tprintf 'done'\n"
	next, _, err = Next(next, Change{Kind: "saveCommand", Command: Command{ID: "script", Name: " 容器状态 ", Group: "Docker", Command: strings.ReplaceAll(text, "\n", "\r\n")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c := next.Commands[len(next.Commands)-1]; c.Command != text || c.Name != "容器状态" {
		t.Fatal("source formatting lost")
	}
	next, _, err = Next(next, Change{Kind: "renameCommandGroup", Name: "Docker", NewName: "容器"}, nil)
	if err != nil || next.Commands[len(next.Commands)-1].Group != "容器" {
		t.Fatal("rename did not retarget commands")
	}
	next, _, err = Next(next, Change{Kind: "deleteCommandGroup", Name: "容器"}, nil)
	if err != nil || next.Commands[len(next.Commands)-1].Group != Ungrouped || next.Commands[len(next.Commands)-1].Command != text {
		t.Fatal("delete group lost source")
	}
	if len(original.CommandGroups) != 1 || len(original.Commands) != 0 || len(next.Groups) != 1 {
		t.Fatal("other configuration mutated")
	}
	next, _, err = Next(next, Change{Kind: "deleteCommand", ID: "script"}, nil)
	if err != nil || len(next.Commands) != 0 {
		t.Fatal("command not deleted")
	}
}

func TestCommandBoundsAndGroupOrdering(t *testing.T) {
	for _, text := range []string{"", "  ", "echo\x00x", "echo\x1bx", "echo\rx", "echo\u2028x", strings.Repeat("x", MaxCommandBytes+1), strings.Repeat("中", MaxCommandBytes/3+1)} {
		if ValidCommandText(text) {
			t.Fatal("invalid content accepted")
		}
	}
	if !ValidCommandText("\t# 中文\nprintf 'x'\n") || !ValidCommandText(strings.Repeat("x", MaxCommandBytes)) {
		t.Fatal("valid multiline rejected")
	}
	s := Empty()
	for _, name := range []string{"a", "b"} {
		var err error
		s, _, err = Next(s, Change{Kind: "addCommandGroup", Name: name}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	ordered, _, err := Next(s, Change{Kind: "reorderCommandGroups", CommandGroups: []string{Ungrouped, "b", "a"}}, nil)
	if err != nil || ordered.CommandGroups[1] != "b" {
		t.Fatal("valid order rejected")
	}
	for _, change := range []Change{{Kind: "deleteCommandGroup", Name: Ungrouped}, {Kind: "renameCommandGroup", Name: Ungrouped, NewName: "x"}, {Kind: "reorderCommandGroups", CommandGroups: []string{Ungrouped, "a", "a"}}, {Kind: "saveCommand", Command: Command{ID: "bad", Name: "bad", Group: "missing", Command: "pwd"}}} {
		if _, _, err := Next(s, change, nil); err == nil {
			t.Fatal("invalid group change accepted")
		}
	}
}
