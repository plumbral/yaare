package workspace

import (
	"math"
	"path"
	"strings"
)

type LayoutNode struct {
	Kind   string      `json:"kind"`
	ID     string      `json:"id"`
	Axis   string      `json:"axis,omitempty"`
	Ratio  float64     `json:"ratio,omitempty"`
	First  *LayoutNode `json:"first,omitempty"`
	Second *LayoutNode `json:"second,omitempty"`
}
type LayoutPane struct {
	Label     string `json:"label,omitempty"`
	ID        string `json:"id"`
	HostID    string `json:"hostId"`
	Pending   bool   `json:"pending"`
	Path      string `json:"path"`
	FilesOpen bool   `json:"filesOpen"`
}
type LayoutGroup struct {
	WorkspacePinned bool       `json:"workspacePinned,omitempty"`
	WorkspaceID     string     `json:"workspaceId,omitempty"`
	WorkspaceHostID string     `json:"workspaceHostId,omitempty"`
	WorkspaceName   string     `json:"workspaceName,omitempty"`
	CustomName      bool       `json:"customName,omitempty"`
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Root            LayoutNode `json:"root"`
	ActiveID        string     `json:"activeId"`
	Maximized       bool       `json:"maximized"`
	Pinned          bool       `json:"pinned"`
}
type SessionLayout struct {
	Version    int64         `json:"version"`
	Format     int           `json:"format"`
	Enabled    bool          `json:"enabled"`
	Groups     []LayoutGroup `json:"groups"`
	Panes      []LayoutPane  `json:"panes"`
	ActiveID   string        `json:"activeId"`
	View       string        `json:"view"`
	StartOpen  bool          `json:"startOpen"`
	StartWidth int           `json:"startWidth"`
	EndWidth   int           `json:"endWidth"`
}

func EmptySessionLayout() SessionLayout {
	return SessionLayout{Format: 1, Enabled: true, Groups: []LayoutGroup{}, Panes: []LayoutPane{}, View: "hosts", StartWidth: 256, EndWidth: 320}
}
func ValidSessionLayout(s SessionLayout) bool {
	if s.Version < 0 || s.Version > 1<<53 || s.Format != 1 || len(s.Groups) > 16 || len(s.Panes) > 64 || s.View != "hosts" && s.View != "terminal" || s.StartWidth < 200 || s.StartWidth > 800 || s.EndWidth < 280 || s.EndWidth > 1000 {
		return false
	}
	panes := map[string]bool{}
	for _, p := range s.Panes {
		if p.Label != "" && !cleanText(p.Label, 64) || !idPattern.MatchString(p.ID) || panes[p.ID] || p.HostID != "" && !idPattern.MatchString(p.HostID) || len(p.Path) > 32768 || strings.ContainsAny(p.Path, "\x00\r\n") || p.Path != "." && (!strings.HasPrefix(p.Path, "/") || path.Clean(p.Path) != p.Path) {
			return false
		}
		panes[p.ID] = true
	}
	seen, nodes, groups := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var visit func(LayoutNode, int) ([]string, bool)
	visit = func(n LayoutNode, depth int) ([]string, bool) {
		if depth > 8 || !idPattern.MatchString(n.ID) || nodes[n.ID] {
			return nil, false
		}
		nodes[n.ID] = true
		if n.Kind == "leaf" {
			if n.First != nil || n.Second != nil || n.Axis != "" || n.Ratio != 0 || !panes[n.ID] || seen[n.ID] {
				return nil, false
			}
			seen[n.ID] = true
			return []string{n.ID}, true
		}
		if n.Kind != "split" || n.First == nil || n.Second == nil || n.Axis != "horizontal" && n.Axis != "vertical" || math.IsNaN(n.Ratio) || n.Ratio < .05 || n.Ratio > .95 {
			return nil, false
		}
		a, ok := visit(*n.First, depth+1)
		if !ok {
			return nil, false
		}
		b, ok := visit(*n.Second, depth+1)
		return append(a, b...), ok
	}
	for _, g := range s.Groups {
		if g.WorkspaceID != "" && !idPattern.MatchString(g.WorkspaceID) || g.WorkspaceHostID != "" && !idPattern.MatchString(g.WorkspaceHostID) || g.WorkspaceName != "" && !cleanText(g.WorkspaceName, 64) {
			return false
		}
		if !idPattern.MatchString(g.ID) || groups[g.ID] || !cleanText(g.Name, 64) {
			return false
		}
		groups[g.ID] = true
		ids, ok := visit(g.Root, 0)
		if !ok || len(ids) > 8 {
			return false
		}
		active := false
		for _, id := range ids {
			if id == g.ActiveID {
				active = true
			}
		}
		if !active {
			return false
		}
	}
	return len(seen) == len(panes) && (len(panes) == 0 && s.ActiveID == "" || seen[s.ActiveID]) && (!s.Enabled || len(s.Panes) <= 64)
}
