package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sync"
)

type IncomingFile struct {
	mu                         sync.Mutex
	f                          FS
	target, temporary          string
	expected                   FileVersion
	output                     io.WriteCloser
	size, written              int64
	created, committed, closed bool
}
type IncomingInfo struct {
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	Conflict     bool   `json:"conflict"`
	ExistingSize int64  `json:"existingSize"`
}

func NewIncomingFile(f FS, dir, name string, size int64, nonce string) (*IncomingFile, error) {
	if !Leaf(name) || size < 0 || size > 0xffffffff || !Leaf(nonce) {
		return nil, problem("ZMODEM 仅接收普通文件，名称必须有效，大小不能超过 4 GiB。")
	}
	target := path.Join(dir, name)
	if e := checkParent(f, target); e != nil {
		return nil, e
	}
	info, e := f.Lstat(target)
	baseline, e := version(info, e)
	if e != nil {
		return nil, e
	}
	return &IncomingFile{f: f, target: target, temporary: path.Join(dir, ".yaare-zmodem-"+nonce+".part"), expected: baseline, size: size}, nil
}
func (i *IncomingFile) Info() IncomingInfo {
	i.mu.Lock()
	defer i.mu.Unlock()
	return IncomingInfo{path.Base(i.target), i.size, i.expected.Exists, i.expected.Size}
}
func (i *IncomingFile) Begin(policy string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.created {
		return problem("接收任务状态已变化。")
	}
	if policy != "ask" && policy != "overwrite" && policy != "keepBoth" {
		return problem("请选择覆盖或保留两者。")
	}
	if i.expected.Exists && policy == "ask" {
		return ErrConflict
	}
	current, e := version(i.f.Lstat(i.target))
	if e != nil {
		return e
	}
	if !i.expected.Matches(current) {
		return resumeProblem("目标在确认期间变化，未开始接收。")
	}
	if current.Exists && policy == "keepBoth" {
		original := i.target
		found := false
		for n := 2; n <= 9999; n++ {
			ext := path.Ext(original)
			i.target = fmt.Sprintf("%s (%d)%s", original[:len(original)-len(ext)], n, ext)
			if _, e := i.f.Lstat(i.target); errors.Is(e, fs.ErrNotExist) {
				found = true
				break
			} else if e != nil {
				return e
			}
		}
		if !found {
			return problem("无法找到可用的副本名称。")
		}
		i.expected = FileVersion{}
	} else if current.Exists && !current.Mode.IsRegular() {
		return problem("目标类型不是普通文件，请保留两者或跳过。")
	}
	if e = checkParent(i.f, i.target); e != nil {
		return e
	}
	i.output, e = i.f.Create(i.temporary)
	if e != nil {
		return e
	}
	i.created = true
	return nil
}
func (i *IncomingFile) Write(offset int64, data []byte) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || !i.created || offset != i.written || len(data) > 65536 || int64(len(data)) > i.size-i.written {
		return problem("文件数据顺序或大小无效，未提交目标。")
	}
	n, e := i.output.Write(data)
	i.written += int64(n)
	if e != nil {
		return e
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
func (i *IncomingFile) Commit() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || !i.created || i.written != i.size {
		return problem("文件尚未完整接收，未提交目标。")
	}
	if syncer, ok := i.output.(interface{ Sync() error }); ok {
		if e := syncer.Sync(); e != nil {
			return e
		}
	}
	e := i.output.Close()
	i.output = nil
	i.closed = true
	if e != nil {
		return e
	}
	if e = checkParent(i.f, i.target); e != nil {
		return e
	}
	current, e := version(i.f.Lstat(i.target))
	if e != nil {
		return e
	}
	if !i.expected.Matches(current) {
		return resumeProblem("目标在接收期间变化，原文件已保留。")
	}
	mode := fs.FileMode(0600)
	if i.expected.Exists {
		mode = i.expected.Mode.Perm()
	}
	if e = i.f.Chmod(i.temporary, mode); e != nil {
		return e
	}
	if e = i.f.Rename(i.temporary, i.target, i.expected.Exists); e != nil {
		return e
	}
	i.committed = true
	return nil
}
func (i *IncomingFile) Cancel() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.output != nil {
		_ = i.output.Close()
		i.output = nil
	}
	i.closed = true
	if i.created && !i.committed {
		e := i.f.Remove(i.temporary, false)
		if !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	return nil
}
