package ipc

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/domain/workspace"
)

type TerminalFileScope struct {
	SessionID string `json:"sessionId"`
	Attempt   int    `json:"attempt"`
	Lease     string `json:"lease"`
}
type TerminalReceiveRequest struct {
	Scope       TerminalFileScope `json:"scope"`
	FolderToken string            `json:"folderToken"`
	Name        string            `json:"name"`
	Size        int64             `json:"size"`
}
type TerminalReceiveView struct {
	ID   string              `json:"id"`
	File filefs.IncomingInfo `json:"file"`
}
type TerminalUploadFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}
type TerminalUploadSelection struct {
	ID    string               `json:"id"`
	Files []TerminalUploadFile `json:"files"`
}
type TerminalRead struct {
	Data  []byte `json:"data"`
	Final bool   `json:"final"`
}
type terminalReceive struct {
	page  *callerScope
	scope TerminalFileScope
	file  *filefs.IncomingFile
}
type terminalDirectory struct {
	page  *callerScope
	scope TerminalFileScope
	root  *os.Root
}

func (s *FileService) TerminalReceiveChooseFolder(ctx context.Context, scope TerminalFileScope) (*LocalFolder, error) {
	if e := s.terminalScope(ctx, scope); e != nil {
		return nil, e
	}
	page, window := callerPage(ctx), mygo.CallerWindow(ctx)
	if window == nil {
		return nil, errors.New("请在桌面窗口中选择接收目录。")
	}
	paths, e := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: uiText(callerPage(ctx), "选择 ZMODEM 接收目录"), ButtonLabel: uiText(callerPage(ctx), "接收到此目录"), Directory: true, CreateDirectories: true})
	if e != nil {
		return nil, errors.New("接收目录选择框未打开，请重试。")
	}
	if len(paths) == 0 {
		return nil, nil
	}
	if e = s.terminalScope(ctx, scope); e != nil {
		return nil, e
	}
	root, e := os.OpenRoot(paths[0])
	if e != nil {
		return nil, fileProblem(e)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.terminalReceives {
		if r.page == page && r.scope == scope {
			root.Close()
			return nil, errors.New("请先完成或取消当前文件，再更换接收目录。")
		}
	}
	for id, d := range s.terminalDirectories {
		if d.page == page && d.scope == scope {
			d.root.Close()
			delete(s.terminalDirectories, id)
		}
	}
	if len(s.terminalDirectories) >= 16 {
		root.Close()
		return nil, errors.New("接收目录授权较多，请先结束其他终端传输。")
	}
	id := workspace.NewID()
	s.terminalDirectories[id] = &terminalDirectory{page, scope, root}
	return &LocalFolder{Token: id, Name: filepath.Base(paths[0])}, nil
}

type terminalUpload struct {
	mu     sync.Mutex
	page   *callerScope
	scope  TerminalFileScope
	fs     *filefs.Dropped
	files  map[string]*terminalUploadSource
	closed bool
}
type terminalUploadSource struct {
	meta   TerminalUploadFile
	reader io.ReadCloser
	offset int64
	done   bool
}

func (s *FileService) terminalScope(ctx context.Context, scope TerminalFileScope) error {
	if e := workspaceCaller(ctx); e != nil {
		return e
	}
	if s.ssh == nil {
		return errors.New("SSH 不可用。")
	}
	run := s.ssh.owned(ctx, scope.SessionID, scope.Attempt)
	if run == nil || !run.OwnsTerminalTransfer(scope.Lease) {
		return errors.New("终端传输已结束或连接已变化。")
	}
	return nil
}
func (s *FileService) TerminalReceivePrepare(ctx context.Context, request TerminalReceiveRequest) (TerminalReceiveView, error) {
	if e := s.terminalScope(ctx, request.Scope); e != nil {
		return TerminalReceiveView{}, e
	}
	if runtime.GOOS == "windows" && !filefs.WindowsRelativeName(request.Name) {
		return TerminalReceiveView{}, errors.New("请使用本地授权目录和有效的文件名。")
	}
	page := callerPage(ctx)
	s.mu.Lock()
	directory := s.terminalDirectories[request.FolderToken]
	s.mu.Unlock()
	if directory == nil || directory.page != page || directory.scope != request.Scope {
		return TerminalReceiveView{}, errors.New("请重新选择本批文件的接收目录。")
	}
	f := filefs.Local{Root: directory.root}
	id := workspace.NewID()
	incoming, e := filefs.NewIncomingFile(f, "/", request.Name, request.Size, id)
	if e != nil {
		return TerminalReceiveView{}, fileProblem(e)
	}
	s.mu.Lock()
	if s.terminalDirectories[request.FolderToken] != directory {
		s.mu.Unlock()
		return TerminalReceiveView{}, errors.New("本地授权目录已变化，未开始接收。")
	}
	if len(s.terminalReceives) >= 16 {
		s.mu.Unlock()
		return TerminalReceiveView{}, errors.New("正在接收的文件较多，请完成当前文件后重试。")
	}
	s.terminalReceives[id] = &terminalReceive{page, request.Scope, incoming}
	s.mu.Unlock()
	e = incoming.Begin("ask")
	view := TerminalReceiveView{id, incoming.Info()}
	view.File.Conflict = errors.Is(e, filefs.ErrConflict)
	if e != nil && !view.File.Conflict {
		_ = s.cancelTerminalReceive(page, id)
		return TerminalReceiveView{}, fileProblem(e)
	}
	return view, nil
}
func (s *FileService) receiveFor(ctx context.Context, id string) (*terminalReceive, error) {
	if e := workspaceCaller(ctx); e != nil {
		return nil, e
	}
	s.mu.Lock()
	r := s.terminalReceives[id]
	s.mu.Unlock()
	if r == nil || r.page != callerPage(ctx) {
		return nil, errors.New("接收文件授权已结束。")
	}
	if e := s.terminalScope(ctx, r.scope); e != nil {
		return nil, e
	}
	return r, nil
}
func (s *FileService) TerminalReceiveResolve(ctx context.Context, id, policy string) (TerminalReceiveView, error) {
	r, e := s.receiveFor(ctx, id)
	if e != nil {
		return TerminalReceiveView{}, e
	}
	if e = r.file.Begin(policy); e != nil {
		return TerminalReceiveView{}, fileProblem(e)
	}
	info := r.file.Info()
	info.Conflict = false
	return TerminalReceiveView{id, info}, nil
}
func (s *FileService) TerminalReceiveWrite(ctx context.Context, id string, offset int64, data []byte) error {
	defer clear(data)
	r, e := s.receiveFor(ctx, id)
	if e != nil {
		return e
	}
	return wrapFileError(r.file.Write(offset, data))
}
func (s *FileService) TerminalReceiveCommit(ctx context.Context, id string) error {
	r, e := s.receiveFor(ctx, id)
	if e != nil {
		return e
	}
	if e = r.file.Commit(); e != nil {
		return fileProblem(e)
	}
	s.mu.Lock()
	delete(s.terminalReceives, id)
	s.mu.Unlock()
	return nil
}
func (s *FileService) cancelTerminalReceive(page *callerScope, id string) error {
	s.mu.Lock()
	r := s.terminalReceives[id]
	if r == nil || r.page != page {
		s.mu.Unlock()
		return nil
	}
	delete(s.terminalReceives, id)
	s.mu.Unlock()
	return wrapFileError(r.file.Cancel())
}
func (s *FileService) TerminalReceiveCancel(ctx context.Context, id string) error {
	if e := workspaceCaller(ctx); e != nil {
		return e
	}
	return s.cancelTerminalReceive(callerPage(ctx), id)
}
func (s *FileService) TerminalFilesCancel(ctx context.Context, scope TerminalFileScope) error {
	if e := workspaceCaller(ctx); e != nil {
		return e
	}
	page := callerPage(ctx)
	s.mu.Lock()
	var receives []*terminalReceive
	var uploads []*terminalUpload
	var directories []*terminalDirectory
	for id, r := range s.terminalReceives {
		if r.page == page && r.scope == scope {
			receives = append(receives, r)
			delete(s.terminalReceives, id)
		}
	}
	for id, u := range s.terminalUploads {
		if u.page == page && u.scope == scope {
			uploads = append(uploads, u)
			delete(s.terminalUploads, id)
		}
	}
	for id, d := range s.terminalDirectories {
		if d.page == page && d.scope == scope {
			directories = append(directories, d)
			delete(s.terminalDirectories, id)
		}
	}
	s.mu.Unlock()
	var result error
	for _, r := range receives {
		if e := r.file.Cancel(); e != nil {
			result = fileProblem(e)
		}
	}
	for _, u := range uploads {
		u.close()
	}
	for _, d := range directories {
		_ = d.root.Close()
	}
	return result
}
func (s *FileService) TerminalUploadsChoose(ctx context.Context, scope TerminalFileScope) (*TerminalUploadSelection, error) {
	if e := s.terminalScope(ctx, scope); e != nil {
		return nil, e
	}
	window := mygo.CallerWindow(ctx)
	if window == nil {
		return nil, errors.New("请在桌面窗口中选择上传文件。")
	}
	selected, e := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: uiText(callerPage(ctx), "选择 ZMODEM 上传文件"), ButtonLabel: uiText(callerPage(ctx), "选择文件"), Multiple: true})
	if e != nil {
		return nil, errors.New("文件选择框未打开，请重试。")
	}
	if len(selected) == 0 {
		return nil, nil
	}
	if e = s.terminalScope(ctx, scope); e != nil {
		return nil, e
	}
	return s.terminalUploadsFromPaths(callerPage(ctx), scope, selected)
}
func (s *FileService) terminalUploadsFromPaths(page *callerScope, scope TerminalFileScope, paths []string) (*TerminalUploadSelection, error) {
	dropped, e := filefs.OpenDropped(paths)
	if e != nil {
		return nil, fileProblem(e)
	}
	u := &terminalUpload{page: page, scope: scope, fs: dropped, files: map[string]*terminalUploadSource{}}
	view := &TerminalUploadSelection{ID: workspace.NewID(), Files: []TerminalUploadFile{}}
	for _, name := range dropped.Names() {
		info, e := dropped.Lstat("/" + name)
		if e != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > 0xffffffff {
			u.close()
			return nil, errors.New("ZMODEM 仅上传不超过 4 GiB 的普通文件，不发送目录或链接。")
		}
		meta := TerminalUploadFile{name, info.Size(), info.ModTime().UnixNano()}
		u.files[name] = &terminalUploadSource{meta: meta}
		display := meta
		display.Modified = info.ModTime().UnixMilli()
		view.Files = append(view.Files, display)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.terminalUploads) >= 16 {
		u.close()
		return nil, errors.New("文件选择较多，请先结束其他终端传输。")
	}
	s.terminalUploads[view.ID] = u
	return view, nil
}
func (u *terminalUpload) read(name string) (TerminalRead, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	f := u.files[name]
	if u.closed || f == nil || f.done {
		return TerminalRead{}, errors.New("上传文件授权已结束。")
	}
	check := func() error {
		info, e := u.fs.Lstat("/" + name)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Size() != f.meta.Size || info.ModTime().UnixNano() != f.meta.Modified {
			return errors.New("上传来源在选择后发生变化，请重新选择。")
		}
		return nil
	}
	if f.reader == nil {
		if e := check(); e != nil {
			return TerminalRead{}, e
		}
		var e error
		f.reader, e = u.fs.Open(path.Join("/", name))
		if e != nil {
			return TerminalRead{}, e
		}
	}
	data := make([]byte, min(int64(32768), f.meta.Size-f.offset))
	_, e := io.ReadFull(f.reader, data)
	if e != nil {
		clear(data)
		return TerminalRead{}, errors.New("上传来源未能完整读取，请检查文件后重新选择。")
	}
	f.offset += int64(len(data))
	final := f.offset == f.meta.Size
	if final {
		if e = check(); e != nil {
			clear(data)
			return TerminalRead{}, e
		}
		if e = f.reader.Close(); e != nil {
			clear(data)
			return TerminalRead{}, e
		}
		f.reader = nil
		f.done = true
	}
	return TerminalRead{data, final}, nil
}
func (u *terminalUpload) close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return
	}
	u.closed = true
	for _, f := range u.files {
		if f.reader != nil {
			_ = f.reader.Close()
			f.reader = nil
		}
	}
	u.fs.Close()
}
func (s *FileService) TerminalUploadRead(ctx context.Context, id, name string) (TerminalRead, error) {
	if e := workspaceCaller(ctx); e != nil {
		return TerminalRead{}, e
	}
	s.mu.Lock()
	u := s.terminalUploads[id]
	s.mu.Unlock()
	if u == nil || u.page != callerPage(ctx) {
		return TerminalRead{}, errors.New("上传文件授权已结束。")
	}
	if e := s.terminalScope(ctx, u.scope); e != nil {
		return TerminalRead{}, e
	}
	result, e := u.read(name)
	return result, wrapFileError(e)
}
func (s *FileService) closeTerminalFilesLocked(page *callerScope, all bool) {
	for id, r := range s.terminalReceives {
		if all || r.page == page {
			_ = r.file.Cancel()
			delete(s.terminalReceives, id)
		}
	}
	for id, u := range s.terminalUploads {
		if all || u.page == page {
			u.close()
			delete(s.terminalUploads, id)
		}
	}
	for id, d := range s.terminalDirectories {
		if all || d.page == page {
			_ = d.root.Close()
			delete(s.terminalDirectories, id)
		}
	}
}
