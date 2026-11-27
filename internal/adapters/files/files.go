package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/sftp"
)

type Problem struct{ Message string }

func (p *Problem) Error() string   { return p.Message }
func problem(message string) error { return &Problem{message} }

type FS interface {
	ReadDir(string) ([]fs.FileInfo, error)
	Lstat(string) (fs.FileInfo, error)
	Open(string) (io.ReadCloser, error)
	Create(string) (io.WriteCloser, error)
	Mkdir(string) error
	Remove(string, bool) error
	Rename(string, string, bool) error
	Chmod(string, fs.FileMode) error
	privateDir(string, bool) error
	commitCopy(context.Context, string, string, FileVersion, fs.FileMode) error
}
type Entry struct {
	Name        string  `json:"name"`
	Size        int64   `json:"size"`
	Modified    int64   `json:"modified"`
	Mode        string  `json:"mode"`
	Permissions string  `json:"permissions"`
	Directory   bool    `json:"directory"`
	Link        bool    `json:"link"`
	OwnerUID    *uint32 `json:"ownerUid,omitempty"`
}

func Metadata(info fs.FileInfo) Entry {
	entry := Entry{Name: info.Name(), Size: info.Size(), Modified: info.ModTime().UnixMilli(), Mode: info.Mode().String(), Permissions: fmt.Sprintf("%03o", info.Mode().Perm()), Directory: info.IsDir(), Link: info.Mode()&fs.ModeSymlink != 0}
	if stat, ok := info.Sys().(*sftp.FileStat); ok && (stat.UID != 0 || stat.GID != 0) {
		entry.OwnerUID = &stat.UID
	}
	return entry
}
func Leaf(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 1024 && !strings.ContainsAny(name, "/\\\x00")
}
func RemotePath(p string) (string, error) {
	if p == "" || p == "." {
		return ".", nil
	}
	if len(p) > 32768 || strings.ContainsRune(p, 0) || !strings.HasPrefix(p, "/") {
		return "", problem("请输入有效的绝对路径。")
	}
	return path.Clean(p), nil
}

type Local struct{ Root *os.Root }

func localPath(p string) string {
	if p == "/" || p == "." || p == "" {
		return "."
	}
	return filepath.FromSlash(strings.TrimPrefix(path.Clean("/"+p), "/"))
}
func (f Local) ReadDir(p string) ([]fs.FileInfo, error) {
	d, e := f.Root.Open(localPath(p))
	if e != nil {
		return nil, e
	}
	defer d.Close()
	rows, e := d.Readdir(50001)
	if e == io.EOF {
		e = nil
	}
	if len(rows) > 50000 {
		return nil, problem("目录超过 50,000 项，请输入更具体的路径。")
	}
	return rows, e
}
func (f Local) Lstat(p string) (fs.FileInfo, error)  { return f.Root.Lstat(localPath(p)) }
func (f Local) Open(p string) (io.ReadCloser, error) { return f.Root.Open(localPath(p)) }
func (f Local) Create(p string) (io.WriteCloser, error) {
	if copyStageLayout(p) {
		return f.createPrivate(p)
	}
	return f.Root.OpenFile(localPath(p), os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateCopyMode)
}
func (f Local) Mkdir(p string) error            { return f.Root.Mkdir(localPath(p), 0755) }
func (f Local) Remove(p string, dir bool) error { return f.Root.Remove(localPath(p)) }
func (f Local) Rename(a, b string, overwrite bool) error {
	if !overwrite {
		if _, e := f.Lstat(b); e == nil {
			return fs.ErrExist
		} else if !errors.Is(e, fs.ErrNotExist) {
			return e
		}
		if info, err := f.Lstat(a); err == nil && info.Mode().IsRegular() {
			if err = f.Root.Link(localPath(a), localPath(b)); err != nil {
				return err
			}
			_ = f.Root.Remove(localPath(a))
			return nil
		}
	}
	return f.Root.Rename(localPath(a), localPath(b))
}
func (f Local) Chmod(p string, m fs.FileMode) error { return f.Root.Chmod(localPath(p), m) }

type Remote struct{ Client *sftp.Client }

func (f Remote) ReadDir(p string) ([]fs.FileInfo, error) { return f.Client.ReadDir(p) }
func (f Remote) Lstat(p string) (fs.FileInfo, error)     { return f.Client.Lstat(p) }
func (f Remote) Open(p string) (io.ReadCloser, error)    { return f.Client.Open(p) }
func (f Remote) Create(p string) (io.WriteCloser, error) {
	handle, err := f.Client.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return nil, f.spaceError(p, err)
	}
	if copyStageLayout(p) {
		info, statErr := handle.Stat()
		if statErr == nil && info.Mode() != privateCopyMode {
			_ = handle.Chmod(privateCopyMode)
			info, statErr = handle.Stat()
		}
		if statErr != nil || !transferPayloadMode(info.Mode()) ||
			info.Mode() != privateCopyMode && verifyPrivateDir(f, path.Dir(p)) != nil {
			_ = handle.Close()
			_ = f.Remove(p, false)
			return nil, problem(privateStageError)
		}
	}
	return remoteWriter{File: handle, remote: f, path: p}, nil
}

type remoteWriter struct {
	*sftp.File
	remote Remote
	path   string
}

func (w remoteWriter) Write(bytes []byte) (int, error) {
	n, e := w.File.Write(bytes)
	if e != nil {
		e = w.remote.spaceError(w.path, e)
	}
	return n, e
}
func (w remoteWriter) WriteAt(bytes []byte, off int64) (int, error) {
	n, e := w.File.WriteAt(bytes, off)
	if e != nil {
		e = w.remote.spaceError(w.path, e)
	}
	return n, e
}
func (w remoteWriter) Close() error {
	e := w.File.Close()
	if e != nil {
		e = w.remote.spaceError(w.path, e)
	}
	return e
}
func (f Remote) spaceError(p string, err error) error {
	var status *sftp.StatusError
	if errors.As(err, &status) && status.Code == 4 {
		if _, ok := f.Client.HasExtension("statvfs@openssh.com"); ok {
			if v, e := f.Client.StatVFS(path.Dir(p)); e == nil && (v.Bavail == 0 || v.Favail == 0) {
				return syscall.ENOSPC
			}
		}
	}
	return err
}
func (f Remote) Mkdir(p string) error { return f.Client.Mkdir(p) }
func (f Remote) Remove(p string, dir bool) error {
	if dir {
		return f.Client.RemoveDirectory(p)
	}
	return f.Client.Remove(p)
}
func (f Remote) Rename(a, b string, overwrite bool) error {
	if overwrite {
		if _, ok := f.Client.HasExtension("posix-rename@openssh.com"); !ok {
			return problem("服务器不支持安全覆盖；原文件已保留。")
		}
		return f.Client.PosixRename(a, b)
	}
	return f.Client.Rename(a, b)
}
func (f Remote) Chmod(p string, m fs.FileMode) error { return f.Client.Chmod(p, m) }

type Item struct {
	Source, Relative string
	Directory        bool
	Size             int64
	Modified         time.Time
}

func Plan(ctx context.Context, f FS, base string, names []string) ([]Item, int64, error) {
	return PlanWithProgress(ctx, f, base, names, nil)
}

func PlanWithProgress(ctx context.Context, f FS, base string, names []string, pulse func()) ([]Item, int64, error) {
	if len(names) == 0 || len(names) > 256 {
		return nil, 0, problem("请选择 1–256 个文件或文件夹。")
	}
	items := []Item{}
	var total int64
	var walk func(string, string, int, fs.FileInfo) error
	walk = func(source, relative string, depth int, info fs.FileInfo) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if depth > 64 || len(items) >= 50000 {
			return problem("目录层级或文件数量超过支持范围。")
		}
		if info == nil {
			var e error
			info, e = f.Lstat(source)
			if e != nil {
				return e
			}
		}
		if pulse != nil {
			pulse()
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return problem("为避免越界，批量复制不跟随符号链接。")
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return problem("仅支持普通文件和文件夹。")
		}
		items = append(items, Item{source, relative, info.IsDir(), info.Size(), info.ModTime()})
		if !info.IsDir() {
			total += info.Size()
			if total > 1<<53 {
				return problem("传输总量超过支持范围。")
			}
			return nil
		}
		children, e := f.ReadDir(source)
		if e != nil {
			return e
		}
		if pulse != nil {
			pulse()
		}
		slices.SortFunc(children, func(left, right fs.FileInfo) int { return strings.Compare(left.Name(), right.Name()) })
		for _, child := range children {
			if !Leaf(child.Name()) {
				return problem("目录含无效文件名。")
			}
			if e = walk(path.Join(source, child.Name()), path.Join(relative, child.Name()), depth+1, child); e != nil {
				return e
			}
		}
		return nil
	}
	seen := map[string]bool{}
	for _, name := range names {
		if !Leaf(name) || seen[name] {
			return nil, 0, problem("文件选择无效或重复。")
		}
		seen[name] = true
		if e := walk(path.Join(base, name), name, 0, nil); e != nil {
			return nil, 0, e
		}
	}
	return items, total, nil
}

type Progress struct {
	Done, Total    int64
	Files, Skipped int
	Current        string
}
type Conflict func(name string, directory bool) (string, error)

func Copy(ctx context.Context, source, dest FS, items []Item, target, nonce string, total int64, conflict Conflict, progress func(Progress)) error {
	p := Progress{Total: total}
	renamed := map[string]string{}
	skipped := []string{}
	for index, item := range items {
		if e := ctx.Err(); e != nil {
			return e
		}
		ignore := false
		for _, prefix := range skipped {
			if item.Relative == prefix || strings.HasPrefix(item.Relative, prefix+"/") {
				ignore = true
				break
			}
		}
		if ignore {
			p.Skipped++
			continue
		}
		relative := item.Relative
		longest := ""
		for prefix := range renamed {
			if (relative == prefix || strings.HasPrefix(relative, prefix+"/")) && len(prefix) > len(longest) {
				longest = prefix
			}
		}
		if longest != "" {
			relative = renamed[longest] + strings.TrimPrefix(relative, longest)
		}
		destination := path.Join(target, relative)
		p.Current = item.Relative
		existing, err := dest.Lstat(destination)
		overwrite := false
		if err == nil {
			choice, e := conflict(item.Relative, item.Directory)
			if e != nil {
				return e
			}
			switch choice {
			case "skip":
				if item.Directory {
					skipped = append(skipped, item.Relative)
				}
				p.Skipped++
				progress(p)
				continue
			case "keepBoth":
				base, ext := path.Base(destination), path.Ext(destination)
				if item.Directory {
					ext = ""
				}
				stem := strings.TrimSuffix(base, ext)
				found := false
				for number := 2; number < 10000; number++ {
					candidate := path.Join(path.Dir(destination), fmt.Sprintf("%s (%d)%s", stem, number, ext))
					if _, e = dest.Lstat(candidate); errors.Is(e, fs.ErrNotExist) {
						destination = candidate
						found = true
						break
					} else if e != nil {
						return e
					}
				}
				if !found {
					return problem("无法找到可用的副本名称。")
				}
				if item.Directory {
					renamed[item.Relative] = strings.TrimPrefix(strings.TrimPrefix(destination, target), "/")
				}
			case "overwrite":
				if (item.Directory && !existing.IsDir()) || (!item.Directory && !regular(existing)) || existing.Mode()&fs.ModeSymlink != 0 {
					return problem("目标类型不同或为符号链接，请选择保留两者。")
				}
				overwrite = true
			default:
				return problem("请选择覆盖、跳过或保留两者。")
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if item.Directory {
			if !overwrite {
				if e := dest.Mkdir(destination); e != nil {
					return e
				}
			}
			progress(p)
			continue
		}
		var baseline FileVersion
		if overwrite {
			baseline, _ = version(existing, nil)
		}
		if e := copyFile(ctx, source, dest, item, copyStagePath(destination, nonce, index), destination, baseline, &p, progress); e != nil {
			return e
		}
		p.Files++
		progress(p)
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	progress(p)
	return nil
}
func copyFile(ctx context.Context, source, dest FS, item Item, temporary, destination string, baseline FileVersion, p *Progress, progress func(Progress)) error {
	input, e := source.Open(item.Source)
	if e != nil {
		return e
	}
	defer input.Close()
	output, e := createCopyStage(dest, temporary, item.Size)
	if e != nil {
		return e
	}
	defer func() {
		_ = output.Close()
		_ = removeCopyStage(dest, temporary)
	}()
	if e = streamWrite(ctx, input, output, dest, 0, item.Size, true, func(data []byte) {
		p.Done += int64(len(data))
		progress(*p)
	}); e != nil {
		return e
	}
	if e = input.Close(); e != nil {
		return e
	}
	if local, ok := output.(*os.File); ok {
		if e = local.Sync(); e != nil {
			return e
		}
	}
	if e = output.Close(); e != nil {
		return e
	}
	after, e := source.Lstat(item.Source)
	if e != nil {
		return e
	}
	if !regular(after) || after.Size() != item.Size || !after.ModTime().Equal(item.Modified) {
		return problem(sourceChanged)
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return dest.commitCopy(ctx, temporary, destination, baseline, after.Mode())
}

func RemoveTree(ctx context.Context, f FS, p string, budget *int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	*budget--
	if *budget < 0 {
		return problem("批量删除超过支持范围。")
	}
	if p == "/" || p == "." || p == "" {
		return problem("不能删除根目录。")
	}
	info, e := f.Lstat(p)
	if e != nil {
		return e
	}
	if info.IsDir() && info.Mode()&fs.ModeSymlink == 0 {
		entries, e := f.ReadDir(p)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if !Leaf(entry.Name()) {
				return problem("目录含无效文件名。")
			}
			if e = RemoveTree(ctx, f, path.Join(p, entry.Name()), budget); e != nil {
				return e
			}
		}
	}
	return f.Remove(p, info.IsDir())
}
