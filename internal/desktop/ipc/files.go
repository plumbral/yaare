package ipc

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/pkg/sftp"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/adapters/recovery"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/domain/workspace"
)

type FileLocation struct {
	Side      string `json:"side"`
	Token     string `json:"token"`
	Path      string `json:"path"`
	SessionID string `json:"sessionId"`
	Attempt   int    `json:"attempt"`
}
type LocalFolder struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}
type FileDirectory struct {
	Path    string         `json:"path"`
	Entries []filefs.Entry `json:"entries"`
}
type FileOperation struct {
	Location    FileLocation `json:"location"`
	Kind        string       `json:"kind"`
	Names       []string     `json:"names"`
	Name        string       `json:"name"`
	Destination string       `json:"destination"`
	Permissions string       `json:"permissions"`
	Confirmed   bool         `json:"confirmed"`
}
type localFolder struct {
	root        *os.Root
	token, name string
}

type FileService struct {
	recovery            *recovery.Journal
	recoverable         map[string]transferRecord
	recoveryProblem     string
	drops               map[string]nativeDrop
	comparisons         map[string]ownedComparison
	syncIntents         map[string]ownedSync
	directorySyncs      map[string]*directorySyncRun
	terminalReceives    map[string]*terminalReceive
	terminalUploads     map[string]*terminalUpload
	terminalDirectories map[string]*terminalDirectory
	ssh                 *SSHService
	mu                  sync.Mutex
	roots               map[*callerScope]*localFolder
	jobs                map[string]*fileJob
	listeners           map[*callerScope]map[chan struct{}]bool
	wake                chan struct{}
	limits              map[*callerScope]int
	sequence            uint64
	plannedItems        int
	stop                chan struct{}
	once                sync.Once
	closeOnce           sync.Once
	textWrites          chan struct{}
	searches            chan struct{}
	textProtection      map[*callerScope]textProtection

	recoveryIOMu       sync.Mutex
	recoveryMu         sync.Mutex
	stateCond          *sync.Cond
	pendingStates      map[string]pendingStateWrite
	stateGen           map[string]uint64
	stateClosed        bool
	stateWorkerStarted bool
	stateWorkerDone    chan struct{}
}

func NewFileService(ssh *SSHService) (*FileService, func()) {
	s := &FileService{ssh: ssh, drops: map[string]nativeDrop{}, roots: map[*callerScope]*localFolder{}, jobs: map[string]*fileJob{}, listeners: map[*callerScope]map[chan struct{}]bool{}, wake: make(chan struct{}, 1), limits: map[*callerScope]int{}, stop: make(chan struct{}), textWrites: make(chan struct{}, 1), searches: make(chan struct{}, 2), textProtection: map[*callerScope]textProtection{}}
	s.comparisons = map[string]ownedComparison{}
	s.syncIntents = map[string]ownedSync{}
	s.directorySyncs = map[string]*directorySyncRun{}
	s.terminalReceives = map[string]*terminalReceive{}
	s.terminalUploads = map[string]*terminalUpload{}
	s.terminalDirectories = map[string]*terminalDirectory{}
	return s, s.shutdown
}
func (s *FileService) shutdown() {
	s.closeOnce.Do(func() {
		close(s.stop)
		s.stopRecoveryStateWorker()
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, job := range s.jobs {
			if job.cancel != nil {
				job.cancel()
			}
		}
		for _, run := range s.directorySyncs {
			if run.cancel != nil {
				run.cancel()
			}
		}
		s.closeTerminalFilesLocked(nil, true)
		for _, folder := range s.roots {
			_ = folder.root.Close()
		}
	})
}

type fileFailure struct{ code, message string }

func (f *fileFailure) Error() string { return f.code + "：" + f.message }
func fileProblem(err error) error {
	code, message := fileError(err)
	return &fileFailure{code, message}
}
func fileError(err error) (string, string) {
	var resumeProblem *filefs.ResumeProblem
	if errors.As(err, &resumeProblem) {
		return "FILE_RESUME_CHANGED", resumeProblem.Message
	}
	var textProblem *filefs.TextProblem
	if errors.As(err, &textProblem) {
		return textProblem.Code, textProblem.Message
	}
	var applicationProblem *filefs.Problem
	if errors.As(err, &applicationProblem) {
		return "FILE_VALIDATION", applicationProblem.Message
	}
	var connectionProblem *sshclient.Fault
	if errors.As(err, &connectionProblem) {
		return connectionProblem.Code, connectionProblem.Message
	}
	var known *fileFailure
	if errors.As(err, &known) {
		return known.code, known.message
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, sftp.ErrSSHFxConnectionLost) {
		return "SFTP_DISCONNECTED", "SFTP 连接已中断，请重新连接后重试。"
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELLED", "操作已取消。"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "FILE_TIMEOUT", "文件操作超时，请检查连接后重试。"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "FILE_PERMISSION", "权限不足，请检查目录或文件权限。"
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "FILE_NOT_FOUND", "文件或目录已不存在，请刷新。"
	}
	if errors.Is(err, fs.ErrExist) {
		return "FILE_EXISTS", "目标已存在，请选择覆盖、跳过或保留两者。"
	}
	var errno syscall.Errno
	if errors.Is(err, syscall.ENOSPC) || runtime.GOOS == "windows" && errors.As(err, &errno) && (errno == 112 || errno == 39) {
		return "DISK_FULL", "磁盘空间不足，原文件已保留。"
	}
	var status *sftp.StatusError
	if errors.As(err, &status) {
		switch status.Code {
		case 2:
			return "FILE_NOT_FOUND", "文件或目录已不存在，请刷新。"
		case 3:
			return "FILE_PERMISSION", "服务器拒绝访问，请检查权限。"
		case 6, 7:
			return "SFTP_DISCONNECTED", "SFTP 连接已中断，请重新连接后重试。"
		}
	}
	return "FILE_FAILED", "文件操作未完成；请检查连接、文件权限、可用空间及服务器能力后重试。"
}
func localFolderDisplayName(dir string) string {
	cleaned := filepath.Clean(dir)
	base := filepath.Base(cleaned)
	if base == "" || base == "." || base == string(filepath.Separator) || base == "/" {
		return cleaned
	}
	return base
}

func (s *FileService) releasePage(page *callerScope) {
	if page == nil {
		return
	}
	s.mu.Lock()
	for id, p := range s.comparisons {
		if p.page == page {
			delete(s.comparisons, id)
		}
	}
	for id, p := range s.syncIntents {
		if p.page == page {
			delete(s.syncIntents, id)
		}
	}
	for id, d := range s.drops {
		if d.page == page {
			delete(s.drops, id)
		}
	}
	for id, j := range s.jobs {
		if j.page == page {
			if j.cancel != nil {
				j.cancel()
			}
			if !j.executing && terminalJob(j.view.State) {
				s.plannedItems -= j.plannedCount
				j.plannedCount = 0
				delete(s.jobs, id)
			}
		}
	}
	for _, run := range s.directorySyncs {
		if run.page == page && run.cancel != nil {
			run.cancel()
		}
	}
	for id, record := range s.recoverable {
		if record.dropPage == page {
			record.dropPage = nil
			record.authorizedDrop = nil
			s.recoverable[id] = record
		}
	}
	s.closeTerminalFilesLocked(page, false)
	delete(s.limits, page)
	delete(s.listeners, page)
	delete(s.textProtection, page)
	folder := s.roots[page]
	delete(s.roots, page)
	s.mu.Unlock()
	if folder != nil {
		_ = folder.root.Close()
	}
}

func (s *FileService) OpenLocalHome(ctx context.Context) (*LocalFolder, error) {
	page := callerPage(ctx)
	if page == nil || mygo.CallerWindow(ctx) == nil {
		return nil, errors.New("请在桌面窗口中选择本地目录。")
	}
	s.mu.Lock()
	if folder := s.roots[page]; folder != nil {
		res := &LocalFolder{folder.token, folder.name}
		s.mu.Unlock()
		return res, nil
	}
	s.mu.Unlock()
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return nil, errors.New("无法打开本机用户目录。")
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, fileProblem(err)
	}
	candidate := &localFolder{root: root, token: workspace.NewID(), name: localFolderDisplayName(home)}
	s.mu.Lock()
	if existing := s.roots[page]; existing != nil {
		res := &LocalFolder{existing.token, existing.name}
		s.mu.Unlock()
		_ = root.Close()
		return res, nil
	}
	s.roots[page] = candidate
	s.mu.Unlock()
	return &LocalFolder{candidate.token, candidate.name}, nil
}

func (s *FileService) SelectLocal(ctx context.Context) (*LocalFolder, error) {
	page, window := callerPage(ctx), mygo.CallerWindow(ctx)
	if page == nil || window == nil {
		return nil, errors.New("请在桌面窗口中选择本地目录。")
	}
	s.mu.Lock()
	for _, run := range s.directorySyncs {
		if run.page == page {
			s.mu.Unlock()
			return nil, errors.New("请先停止自动同步，再更换本地目录。")
		}
	}
	for _, job := range s.jobs {
		if job.page == page && job.view.State != "cancelled" && job.view.State != "completed" {
			s.mu.Unlock()
			return nil, errors.New("请先完成或取消当前文件任务，再更换本地目录。")
		}
	}
	s.mu.Unlock()
	selected, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: uiText(callerPage(ctx), "选择本地文件目录"), ButtonLabel: uiText(callerPage(ctx), "使用此目录"), Directory: true, CreateDirectories: true})
	if err != nil {
		return nil, errors.New("无法打开目录选择框。")
	}
	if len(selected) == 0 {
		return nil, nil
	}
	root, err := os.OpenRoot(selected[0])
	if err != nil {
		return nil, fileProblem(err)
	}
	folder := &localFolder{root: root, token: workspace.NewID(), name: localFolderDisplayName(selected[0])}
	s.mu.Lock()
	for _, job := range s.jobs {
		if job.page == page && job.view.State != "cancelled" && job.view.State != "completed" {
			s.mu.Unlock()
			root.Close()
			return nil, errors.New("文件任务尚未结束，未切换本地目录。")
		}
	}
	old := s.roots[page]
	for _, run := range s.directorySyncs {
		if run.page == page {
			s.mu.Unlock()
			root.Close()
			return nil, errors.New("自动同步已开启，未更换本地目录。")
		}
	}
	s.roots[page] = folder
	s.mu.Unlock()
	if old != nil {
		_ = old.root.Close()
	}
	return &LocalFolder{folder.token, folder.name}, nil
}
func (s *FileService) filesystem(ctx context.Context, page *callerScope, location FileLocation) (filefs.FS, func(), string, error) {
	if page == nil {
		return nil, nil, "", errors.New("请在桌面窗口中操作文件。")
	}
	if location.Side == "local" {
		s.mu.Lock()
		folder := s.roots[page]
		s.mu.Unlock()
		if folder == nil || folder.token != location.Token || location.Token == "" {
			return nil, nil, "", errors.New("请重新选择本地目录。")
		}
		if len(location.Path) > 32768 || strings.ContainsRune(location.Path, 0) {
			return nil, nil, "", errors.New("本地路径无效。")
		}
		return filefs.Local{Root: folder.root}, func() {}, path.Clean("/" + location.Path), nil
	}
	if location.Side != "remote" {
		return nil, nil, "", errors.New("文件位置无效。")
	}
	run := s.ssh.owned(ctx, location.SessionID, location.Attempt)
	if run == nil {
		return nil, nil, "", errors.New("SSH 会话不可用，请重新连接。")
	}
	client, closeClient, err := run.OpenFiles(ctx)
	if err != nil {
		return nil, nil, "", fileProblem(err)
	}
	p, err := filefs.RemotePath(location.Path)
	if err == nil && p == "." {
		p, err = client.Getwd()
	}
	if err != nil {
		closeClient()
		return nil, nil, "", fileProblem(err)
	}
	return filefs.Remote{Client: client}, closeClient, p, nil
}
func (s *FileService) List(ctx context.Context, location FileLocation) (FileDirectory, error) {
	if err := workspaceCaller(ctx); err != nil {
		return FileDirectory{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var p string
	var rows []fs.FileInfo
	var err error
	if location.Side == "remote" {
		run := s.ssh.owned(ctx, location.SessionID, location.Attempt)
		if run == nil {
			return FileDirectory{}, errors.New("SSH 会话不可用，请重新连接。")
		}
		remotePath, pathErr := filefs.RemotePath(location.Path)
		if pathErr != nil {
			return FileDirectory{}, fileProblem(pathErr)
		}
		p, rows, err = run.BrowseDirectory(ctx, remotePath)
	} else {
		filesystem, closeFS, localPath, openErr := s.filesystem(ctx, callerPage(ctx), location)
		if openErr != nil {
			return FileDirectory{}, openErr
		}
		defer closeFS()
		p = localPath
		rows, err = filesystem.ReadDir(p)
	}
	if err != nil {
		return FileDirectory{}, fileProblem(err)
	}
	if len(rows) > 50000 {
		return FileDirectory{}, errors.New("目录超过 50,000 项，请输入更具体的路径。")
	}
	result := FileDirectory{Path: p, Entries: []filefs.Entry{}}
	for _, row := range rows {
		result.Entries = append(result.Entries, filefs.Metadata(row))
	}
	return result, nil
}
func (s *FileService) Operate(ctx context.Context, request FileOperation) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	filesystem, closeFS, p, err := s.filesystem(ctx, callerPage(ctx), request.Location)
	if err != nil {
		return err
	}
	defer closeFS()
	if request.Kind == "create" {
		if request.Location.Side != "remote" {
			return errors.New("新建文件仅用于当前远程连接。")
		}
		return wrapFileError(filefs.CreateEmpty(ctx, filesystem, p, request.Name))
	}
	if request.Kind == "mkdir" {
		if !filefs.Leaf(request.Name) {
			return errors.New("请输入有效的文件夹名称。")
		}
		return wrapFileError(filesystem.Mkdir(path.Join(p, request.Name)))
	}
	if len(request.Names) == 0 || len(request.Names) > 256 {
		return errors.New("请选择 1–256 项。")
	}
	for _, name := range request.Names {
		if !filefs.Leaf(name) {
			return errors.New("文件名无效。")
		}
	}
	switch request.Kind {
	case "delete":
		if !request.Confirmed {
			return errors.New("请先确认删除所选文件。")
		}
		budget := 50000
		for _, name := range request.Names {
			if err = filefs.RemoveTree(ctx, filesystem, path.Join(p, name), &budget); err != nil {
				return fileProblem(err)
			}
		}
	case "rename", "move":
		if len(request.Names) != 1 || !filefs.Leaf(request.Name) {
			return errors.New("请选择一个文件并输入新名称。")
		}
		destination := p
		if request.Kind == "move" {
			destination, err = filefs.RemotePath(request.Destination)
			if err != nil || destination == "." {
				return errors.New("请输入有效的目标目录。")
			}
		}
		from, to := path.Join(p, request.Names[0]), path.Join(destination, request.Name)
		if from == to {
			return nil
		}
		if strings.HasPrefix(to, from+"/") {
			return errors.New("不能移动到自身的子目录。")
		}
		if _, err = filesystem.Lstat(to); err == nil {
			return errors.New("目标已存在，请先重命名或使用复制任务处理冲突。")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fileProblem(err)
		}
		return wrapFileError(filesystem.Rename(from, to, false))
	case "chmod":
		if len(request.Permissions) != 3 && len(request.Permissions) != 4 {
			return errors.New("权限应为 3–4 位八进制数。")
		}
		mode, e := strconv.ParseUint(request.Permissions, 8, 12)
		if e != nil || mode > 0777 {
			return errors.New("仅支持 000–777 的普通访问权限。")
		}
		for _, name := range request.Names {
			if err = filesystem.Chmod(path.Join(p, name), fs.FileMode(mode)); err != nil {
				return fileProblem(err)
			}
		}
	default:
		return errors.New("不支持的文件操作。")
	}
	return nil
}
func wrapFileError(err error) error {
	if err == nil {
		return nil
	}
	return fileProblem(err)
}
