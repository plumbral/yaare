package files

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
)

type SyncBaseline map[string]CompareStamp
type BilateralAction struct {
	Path        string       `json:"path"`
	Kind        string       `json:"kind"`
	Message     string       `json:"message,omitempty"`
	Directory   bool         `json:"directory"`
	Left, Right CompareStamp `json:"-"`
}

func sameSyncContent(a, b CompareStamp) bool {
	if a.Exists != b.Exists {
		return false
	}
	if !a.Exists {
		return true
	}
	if a.Link || b.Link || a.Problem != "" || b.Problem != "" || a.Directory != b.Directory {
		return false
	}
	if a.Directory {
		return a.Scanned && b.Scanned
	}
	return a.Hash != "" && a.Hash == b.Hash && a.Size == b.Size
}

func AcceptSyncBaseline(base SyncBaseline, c Comparison) {
	seen := map[string]bool{}
	for _, e := range c.Entries {
		seen[e.Relative] = true
		if sameSyncContent(e.Left, e.Right) {
			base[e.Relative] = e.Left
		}
	}
	for p := range base {
		if !seen[p] {
			delete(base, p)
		}
	}
}
func PlanBilateral(c Comparison, base SyncBaseline, deleteEnabled bool) ([]BilateralAction, error) {
	if !c.Complete || !c.Directory || !c.Recursive {
		return nil, problem("自动同步需要完整的子目录比较，请缩小范围后重新扫描。")
	}
	actions := []BilateralAction{}
	for _, e := range c.Entries {
		l, r := e.Left, e.Right
		if sameSyncContent(l, r) {
			continue
		}
		a := BilateralAction{Path: e.Relative, Directory: e.Directory, Left: l, Right: r}
		old, known := base[e.Relative]
		switch {
		case e.Status == "unsupported" || e.Status == "unverified" || e.Status == "name-conflict" || e.Status == "type-conflict":
			a.Kind = "conflict"
			a.Message = e.Message
		case !l.Exists || !r.Exists:
			remaining := l
			a.Kind = "upload"
			if !l.Exists {
				remaining = r
				a.Kind = "download"
			}
			if known {
				if !sameSyncContent(old, remaining) {
					a.Kind = "conflict"
					a.Message = "一端已删除，另一端有修改。"
				} else if !deleteEnabled {
					a.Kind = "retained"
					a.Message = "删除同步未开启，另一端已保留。"
				} else if !l.Exists {
					a.Kind = "delete-remote"
				} else {
					a.Kind = "delete-local"
				}
			}
		case known && sameSyncContent(old, l):
			a.Kind = "download"
		case known && sameSyncContent(old, r):
			a.Kind = "upload"
		default:
			a.Kind = "conflict"
			a.Message = "两端内容不同，请选择保留版本或合并文本。"
		}
		actions = append(actions, a)
	}
	byPath := map[string]*BilateralAction{}
	for i := range actions {
		byPath[actions[i].Path] = &actions[i]
	}
	for i := range actions {
		for parent := path.Dir(actions[i].Path); parent != "." && parent != "/"; parent = path.Dir(parent) {
			if a := byPath[parent]; a != nil && a.Directory && strings.HasPrefix(a.Kind, "delete-") && actions[i].Kind != a.Kind {
				a.Kind = "conflict"
				a.Message = "目录内仍有修改或未处理项，未删除目录。"
			}
		}
	}
	for i := range actions {
		for parent := path.Dir(actions[i].Path); parent != "." && parent != "/"; parent = path.Dir(parent) {
			if a := byPath[parent]; a != nil && a.Directory && a.Kind == "conflict" {
				actions[i].Kind = "conflict"
				actions[i].Message = "请先处理父目录冲突。"
				break
			}
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Path < actions[j].Path })
	return actions, nil
}

func BilateralCopies(actions []BilateralAction, leftRoot, rightRoot, direction string) *ReviewedSync {
	sourceRoot, targetRoot := leftRoot, rightRoot
	if direction == "download" {
		sourceRoot, targetRoot = targetRoot, sourceRoot
	}
	p := &ReviewedSync{Reviews: map[string]CopyReview{}, DestinationRoot: targetRoot, created: map[string]bool{}}
	for _, a := range actions {
		if a.Kind != direction {
			continue
		}
		src, dst := a.Left, a.Right
		if direction == "download" {
			src, dst = dst, src
		}
		from, to := path.Join(sourceRoot, a.Path), path.Join(targetRoot, a.Path)
		p.Reviews[to] = CopyReview{from, to, src, dst}
		p.Items = append(p.Items, Item{Source: from, Relative: a.Path, Directory: src.Directory, Size: src.Size, Modified: timeFromStamp(src)})
		p.Preview = append(p.Preview, SyncPreview{Path: a.Path, Action: "sync", Directory: src.Directory, Size: src.Size})
		if !src.Directory {
			p.Total += src.Size
		}
	}
	sort.Slice(p.Items, func(i, j int) bool {
		a, b := p.Items[i].Relative, p.Items[j].Relative
		if strings.Count(a, "/") != strings.Count(b, "/") {
			return strings.Count(a, "/") < strings.Count(b, "/")
		}
		return a < b
	})
	return p
}
func ApplyBilateralDeletes(ctx context.Context, left, right FS, leftRoot, rightRoot string, actions []BilateralAction) error {
	list := append([]BilateralAction(nil), actions...)
	sort.Slice(list, func(i, j int) bool { return len(list[i].Path) > len(list[j].Path) })
	for _, a := range list {
		if a.Kind != "delete-local" && a.Kind != "delete-remote" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		source, dest, sr, dr, expected := left, right, leftRoot, rightRoot, a.Right
		if a.Kind == "delete-local" {
			source, dest, sr, dr, expected = right, left, rightRoot, leftRoot, a.Left
		}
		from, to := path.Join(sr, a.Path), path.Join(dr, a.Path)
		if err := checkedParents(dest, to); err != nil {
			return err
		}
		for parent := path.Dir(from); ; parent = path.Dir(parent) {
			info, e := source.Lstat(parent)
			if e != nil && !errors.Is(e, fs.ErrNotExist) {
				return e
			}
			if e == nil && (!info.IsDir() || info.Mode()&fs.ModeSymlink != 0) {
				return resumeProblem("删除来源父目录已变化，未执行删除。")
			}
			if parent == "." || parent == "/" {
				break
			}
		}
		missing, e := CompareFingerprint(ctx, source, from)
		if e != nil {
			return e
		}
		if missing.Exists {
			return resumeProblem("删除来源已重新出现，请重新扫描。")
		}
		actual, e := CompareFingerprint(ctx, dest, to)
		if e != nil {
			return e
		}
		if !expected.Matches(actual) {
			return resumeProblem("待删除文件已经变化，未执行删除。")
		}
		if a.Directory {
			rows, e := dest.ReadDir(to)
			if e != nil {
				return e
			}
			if len(rows) != 0 {
				return problem("待删除目录出现新内容，已保留目录。")
			}
		}
		if actual.Mode&fs.ModeSymlink != 0 {
			return fs.ErrPermission
		}
		if e = dest.Remove(to, a.Directory); e != nil {
			return e
		}
	}
	return nil
}
