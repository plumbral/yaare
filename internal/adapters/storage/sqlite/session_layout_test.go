package sqlite

import (
	"testing"
	"yaare/internal/domain/workspace"
)

func layoutFixture() workspace.SessionLayout {
	s := workspace.EmptySessionLayout()
	s.Panes = []workspace.LayoutPane{{ID: "pane1", HostID: "test-host", Path: "/srv", FilesOpen: true}, {ID: "pane2", HostID: "missing-host", Path: ".", Pending: true}}
	s.Groups = []workspace.LayoutGroup{{ID: "group1", Name: "工作标签", ActiveID: "pane1", Pinned: true, Root: workspace.LayoutNode{Kind: "split", ID: "split1", Axis: "horizontal", Ratio: .35, First: &workspace.LayoutNode{Kind: "leaf", ID: "pane1"}, Second: &workspace.LayoutNode{Kind: "leaf", ID: "pane2"}}}}
	s.ActiveID = "pane1"
	s.View = "terminal"
	return s
}
func TestSessionLayoutTransactionsRestartClearAndIndependentRevision(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	applyTest(t, s, workspace.Change{Kind: "saveHost", Host: hostForTest()})
	configRevision := s.Load(testContext).Snapshot.Revision
	initial, e := s.SessionLayout(testContext)
	if e != nil || !initial.Enabled || initial.Version != 0 || initial.StartOpen {
		t.Fatal("default", e)
	}
	next := layoutFixture()
	next.Panes[0].Label = "Logs"
	next.Groups[0].WorkspaceID = "workspace1"
	next.Groups[0].WorkspaceName = "Po0"
	next.Groups[0].WorkspaceHostID = "test-host"
	next.Groups[0].WorkspacePinned = true
	next.StartOpen = true
	saved, e := s.SaveSessionLayout(testContext, next)
	if e != nil || saved.Version != 1 {
		t.Fatal("save", e)
	}
	if _, e = s.SaveSessionLayout(testContext, next); e == nil {
		t.Fatal("stale layout accepted")
	}
	if s.Load(testContext).Snapshot.Revision != configRevision {
		t.Fatal("layout invalidated open host editor")
	}
	s.Close()
	s = openTest(t, dir)
	restored, e := s.SessionLayout(testContext)
	if e != nil || restored.Groups[0].WorkspaceID != "workspace1" || restored.Groups[0].WorkspaceHostID != "test-host" || restored.Groups[0].WorkspaceName != "Po0" || !restored.Groups[0].WorkspacePinned || restored.Panes[0].Label != "Logs" || restored.Panes[0].Path != "/srv" || restored.Groups[0].Root.Ratio != .35 || !restored.Groups[0].Pinned || !restored.StartOpen {
		t.Fatal("restart", e)
	}
	restored.Enabled = false
	saved, e = s.SaveSessionLayout(testContext, restored)
	if e != nil || len(saved.Panes) != 0 || len(saved.Groups) != 0 || saved.StartOpen {
		t.Fatal("disable retained recoverable panes", e)
	}
	invalid := layoutFixture()
	invalid.Version = saved.Version
	invalid.Groups[0].Root.Ratio = 2
	if _, e = s.SaveSessionLayout(testContext, invalid); e == nil {
		t.Fatal("invalid ratio")
	}
	invalid = layoutFixture()
	invalid.Version = saved.Version
	invalid.Groups[0].Root.Second.ID = "pane1"
	if _, e = s.SaveSessionLayout(testContext, invalid); e == nil {
		t.Fatal("duplicate pane")
	}
}
