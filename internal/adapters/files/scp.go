package files

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"sync"
)

type RemoteCommand struct {
	Input  io.WriteCloser
	Output io.Reader
	Wait   func() error
	Close  func()
}
type CommandOpener func(context.Context, string) (RemoteCommand, error)

type SCP struct {
	FS
	Context     context.Context
	OpenCommand CommandOpener
}

func scpPath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\r\n\x00") {
		return "", problem("SCP 需要绝对路径，文件名不能包含换行符。")
	}
	return "'" + strings.ReplaceAll(p, "'", "'\\''") + "'", nil
}
func scpFailure() error {
	return problem("服务器 SCP 传输未确认，请检查服务器是否安装 scp 及目录权限。")
}
func scpAck(r *bufio.Reader) error {
	b, e := r.ReadByte()
	if e != nil {
		return scpFailure()
	}
	if b == 0 {
		return nil
	}
	return scpFailure()
}
func scpWrite(w io.Writer, b []byte) error {
	n, e := w.Write(b)
	if e != nil {
		return e
	}
	if n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}
func (f SCP) Open(p string) (io.ReadCloser, error) {
	quoted, e := scpPath(p)
	if e != nil {
		return nil, e
	}
	info, e := f.FS.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return nil, fs.ErrInvalid
	}
	cmd, e := f.OpenCommand(f.Context, "scp -qf -- "+quoted)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			cmd.Close()
		}
	}()
	reader := bufio.NewReaderSize(cmd.Output, 4096)
	if e = scpWrite(cmd.Input, []byte{0}); e != nil {
		return nil, e
	}
	line, e := reader.ReadSlice('\n')
	if e != nil || len(line) < 9 || line[0] != 'C' {
		return nil, scpFailure()
	}
	fields := strings.SplitN(string(line[1:len(line)-1]), " ", 3)
	if len(fields) != 3 || len(fields[0]) != 4 || fields[2] != path.Base(p) {
		return nil, scpFailure()
	}
	if _, e = strconv.ParseUint(fields[0], 8, 12); e != nil {
		return nil, scpFailure()
	}
	size, e := strconv.ParseInt(fields[1], 10, 64)
	if e != nil || size != info.Size() {
		return nil, scpFailure()
	}
	if e = scpWrite(cmd.Input, []byte{0}); e != nil {
		return nil, e
	}
	ok = true
	return &scpReader{command: cmd, reader: reader, remaining: size}, nil
}
func (f SCP) Create(string) (io.WriteCloser, error) {
	return nil, problem("SCP 写入需要明确的文件大小。")
}
func (f SCP) CreateSized(p string, size int64) (io.WriteCloser, error) {
	quoted, e := scpPath(p)
	if e != nil {
		return nil, e
	}
	if size < 0 || size > 1<<53 {
		return nil, fs.ErrInvalid
	}
	reserved, e := f.FS.Create(p)
	if e != nil {
		return nil, e
	}
	if e = reserved.Close(); e != nil {
		f.FS.Remove(p, false)
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.FS.Remove(p, false)
		}
	}()
	cmd, e := f.OpenCommand(f.Context, "scp -qt -- "+quoted)
	if e != nil {
		return nil, e
	}
	reader := bufio.NewReaderSize(cmd.Output, 4096)
	if e = scpAck(reader); e == nil {
		e = scpWrite(cmd.Input, []byte(fmt.Sprintf("C0600 %d %s\n", size, path.Base(p))))
	}
	if e == nil {
		e = scpAck(reader)
	}
	if e != nil {
		cmd.Close()
		return nil, e
	}
	ok = true
	return &scpWriter{command: cmd, reader: reader, remaining: size}, nil
}

type scpReader struct {
	command   RemoteCommand
	reader    *bufio.Reader
	remaining int64
	once      sync.Once
	result    error
}

func (r *scpReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		if e := r.finish(); e != nil {
			return 0, e
		}
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, e := r.reader.Read(p)
	r.remaining -= int64(n)
	if e == io.EOF && r.remaining > 0 {
		e = io.ErrUnexpectedEOF
	}
	return n, e
}
func (r *scpReader) finish() error {
	r.once.Do(func() {
		defer r.command.Close()
		if r.remaining != 0 {
			r.result = io.ErrUnexpectedEOF
			return
		}
		r.result = scpAck(r.reader)
		if r.result == nil {
			r.result = scpWrite(r.command.Input, []byte{0})
		}
		_ = r.command.Input.Close()
		if r.result == nil && r.command.Wait() != nil {
			r.result = scpFailure()
		}
	})
	return r.result
}
func (r *scpReader) Close() error { return r.finish() }

type scpWriter struct {
	command   RemoteCommand
	reader    *bufio.Reader
	remaining int64
	once      sync.Once
	result    error
}

func (w *scpWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, io.ErrShortWrite
	}
	n, e := w.command.Input.Write(p)
	w.remaining -= int64(n)
	return n, e
}
func (w *scpWriter) Close() error {
	w.once.Do(func() {
		defer w.command.Close()
		if w.remaining != 0 {
			w.result = io.ErrShortWrite
			return
		}
		w.result = scpWrite(w.command.Input, []byte{0})
		if w.result == nil {
			w.result = scpAck(w.reader)
		}
		_ = w.command.Input.Close()
		if w.result == nil && w.command.Wait() != nil {
			w.result = scpFailure()
		}
	})
	return w.result
}

func createSized(f FS, p string, size int64) (io.WriteCloser, error) {
	if sized, ok := f.(interface {
		CreateSized(string, int64) (io.WriteCloser, error)
	}); ok {
		return sized.CreateSized(p, size)
	}
	return f.Create(p)
}
