package files

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Dropped struct {
	entries map[string]dropEntry
	names   []string
}
type dropEntry struct {
	root *os.Root
	file string
	info fs.FileInfo
}
type namedDropInfo struct {
	fs.FileInfo
	name string
}

func (i namedDropInfo) Name() string { return i.name }

func OpenDropped(paths []string) (*Dropped, error) {
	if len(paths) == 0 || len(paths) > 256 {
		return nil, problem("一次最多拖入 256 个文件或文件夹。")
	}
	d := &Dropped{entries: map[string]dropEntry{}}
	fail := func(err error) (*Dropped, error) { d.Close(); return nil, err }
	selected := []string{}
	for _, p := range paths {
		if !filepath.IsAbs(p) || len(p) > 32768 || strings.ContainsRune(p, 0) {
			return fail(problem("拖入的本地路径无效。"))
		}
		p = filepath.Clean(p)
		duplicate := false
		for _, q := range paths {
			if strings.EqualFold(p, filepath.Clean(q)) {
				continue
			}
			rel, e := filepath.Rel(filepath.Clean(q), p)
			if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			found := false
			for _, q := range selected {
				if strings.EqualFold(p, q) {
					found = true
					break
				}
			}
			if !found {
				selected = append(selected, p)
			}
		}
	}
	used := map[string]bool{}
	for _, p := range selected {
		info, e := os.Lstat(p)
		if e != nil {
			return fail(e)
		}
		name := filepath.Base(p)
		if !Leaf(name) || used[strings.ToLower(name)] {
			return fail(problem("拖入项含重名或无效名称，请分批上传。"))
		}
		used[strings.ToLower(name)] = true
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return fail(problem("拖入上传仅支持普通文件和文件夹，不跟随链接。"))
		}
		rootPath, file := p, "."
		if !info.IsDir() {
			rootPath, file = filepath.Dir(p), name
		}
		root, e := os.OpenRoot(rootPath)
		if e != nil {
			return fail(e)
		}
		pinned, e := root.Lstat(file)
		if e != nil || !os.SameFile(info, pinned) {
			root.Close()
			return fail(problem("拖入项已经变化，请重新拖入。"))
		}
		d.entries[name] = dropEntry{root, file, info}
		d.names = append(d.names, name)
	}
	return d, nil
}
func (d *Dropped) Names() []string { return append([]string(nil), d.names...) }
func (d *Dropped) Close() {
	for _, e := range d.entries {
		_ = e.root.Close()
	}
}
func (d *Dropped) resolve(p string) (dropEntry, string, error) {
	if p != path.Clean(p) || !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") {
		return dropEntry{}, "", fs.ErrPermission
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	entry, ok := d.entries[parts[0]]
	if !ok {
		return entry, "", fs.ErrPermission
	}
	if len(parts) == 1 {
		return entry, entry.file, nil
	}
	if !entry.info.IsDir() {
		return entry, "", fs.ErrPermission
	}
	relative := ""
	for _, part := range parts[1:] {
		if !Leaf(part) {
			return entry, "", fs.ErrPermission
		}
		relative = filepath.Join(relative, part)
		i, e := entry.root.Lstat(relative)
		if e != nil {
			return entry, "", e
		}
		if i.Mode()&fs.ModeSymlink != 0 {
			return entry, "", problem("拖入上传不跟随目录中的链接。")
		}
	}
	return entry, relative, nil
}
func (d *Dropped) Lstat(p string) (fs.FileInfo, error) {
	e, r, err := d.resolve(p)
	if err != nil {
		return nil, err
	}
	i, err := e.root.Lstat(r)
	if err == nil && r == e.file {
		return namedDropInfo{i, path.Base(p)}, nil
	}
	return i, err
}
func (d *Dropped) ReadDir(p string) ([]fs.FileInfo, error) {
	if p == "/" {
		items := []fs.FileInfo{}
		for _, n := range d.names {
			i, e := d.Lstat("/" + n)
			if e != nil {
				return nil, e
			}
			items = append(items, i)
		}
		return items, nil
	}
	e, r, err := d.resolve(p)
	if err != nil {
		return nil, err
	}
	return (Local{e.root}).ReadDir(filepath.ToSlash(r))
}
func (d *Dropped) Open(p string) (io.ReadCloser, error) {
	e, r, err := d.resolve(p)
	if err != nil {
		return nil, err
	}
	before, err := e.root.Lstat(r)
	if err != nil {
		return nil, err
	}
	if !regular(before) {
		return nil, fs.ErrPermission
	}
	f, err := e.root.Open(r)
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	after, ae := e.root.Lstat(r)
	if err != nil || ae != nil || !regular(after) || !os.SameFile(before, actual) || !os.SameFile(actual, after) {
		f.Close()
		return nil, problem("源文件已变化，请重新拖入。")
	}
	return f, nil
}
func (d *Dropped) Create(string) (io.WriteCloser, error) { return nil, fs.ErrPermission }
func (d *Dropped) privateDir(string, bool) error         { return fs.ErrPermission }
func (d *Dropped) commitCopy(context.Context, string, string, FileVersion, fs.FileMode) error {
	return fs.ErrPermission
}
func (d *Dropped) Mkdir(string) error                { return fs.ErrPermission }
func (d *Dropped) Remove(string, bool) error         { return fs.ErrPermission }
func (d *Dropped) Rename(string, string, bool) error { return fs.ErrPermission }
func (d *Dropped) Chmod(string, fs.FileMode) error   { return fs.ErrPermission }

var _ FS = (*Dropped)(nil)
