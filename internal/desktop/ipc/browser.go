package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"yaare/internal/adapters/configsync"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/sessionlog"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/adapters/sshconfig"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app"
)

type BrowserServices struct {
	services             map[string]any
	SSH                  *SSHService
	Files                *FileService
	desktop              *DesktopService
	config               *SSHConfigService
	backup               *BackupService
	configurationSync    *SyncService
	stopSync             func()
	store                *sqlite.Store
	logs                 *sessionlog.Store
	closeSSH, closeFiles func()
	root                 func() (string, error)
	localMu              sync.Mutex
	localRoot            *os.Root
	local                map[*callerScope]*browserLocalFolder
	uploads              map[string]*browserUpload
}

func NewBrowserServices(root func() (string, error), protector secrets.Protector) (*BrowserServices, error) {
	if protector == nil {
		return nil, errors.New("请先配置服务端凭证密钥。")
	}
	store := sqlite.New(root)
	application := app.NewWorkspace(store)
	keys := NewPersistentKeyService(store)
	identities := NewIdentityService(store, keys)
	ssh, closeSSH := NewSSHService(keys, root)
	ConfigureCredentialProtection(keys, identities, ssh, protector)
	ConfigureSSHIdentities(ssh, identities)
	ConfigureSSHWorkspace(ssh, application)
	logs := sessionlog.New(root)
	ConfigureSessionLogs(ssh, logs)
	files, closeFiles := NewFileService(ssh)
	b := &BrowserServices{SSH: ssh, Files: files, store: store, logs: logs, closeSSH: closeSSH, closeFiles: closeFiles, root: root, local: map[*callerScope]*browserLocalFolder{}, uploads: map[string]*browserUpload{}}
	b.desktop = NewDesktopService(ssh)
	b.config = NewSSHConfigService(store)
	workspaceService := NewWorkspaceService(application, protector)
	ConfigureWorkspaceKeys(workspaceService, keys)
	ConfigureTransferRecovery(files, root)
	b.services = map[string]any{"ApplicationService": NewApplicationService(app.New()), "WorkspaceService": workspaceService, "SessionLayoutService": NewSessionLayoutService(store), "KeyService": keys, "IdentityService": identities, "SSHService": ssh, "FileService": files, "DesktopService": b.desktop, "SSHConfigService": b.config}
	cloud := NewCloudService(store, protector)
	b.backup = NewBackupService(cloud)
	b.services["BackupService"] = b.backup
	b.configurationSync = NewSyncService(cloud)
	b.services["PreferencesService"] = NewPreferencesService(store)
	b.services["SyncService"] = b.configurationSync
	return b, nil
}
func (b *BrowserServices) Initialize(ctx context.Context) error {
	if e := b.store.Initialize(ctx); e != nil {
		return e
	}
	if e := b.logs.Initialize(); e != nil {
		return e
	}
	root, e := b.root()
	if e != nil {
		return e
	}
	directory := filepath.Join(root, "browser-local")
	if e = os.MkdirAll(directory, 0700); e != nil {
		return e
	}
	b.localRoot, e = os.OpenRoot(directory)
	if e == nil {
		b.stopSync = StartConfigurationSync(b.configurationSync)
	}
	return e
}
func (b *BrowserServices) Close() {
	if b.stopSync != nil {
		b.stopSync()
	}
	b.closeFiles()
	b.closeSSH()
	b.logs.Close()
	b.store.Close()
	if b.localRoot != nil {
		b.localRoot.Close()
	}
}
func (b *BrowserServices) Release(ctx context.Context) {
	page := callerPage(ctx)
	if page == nil || !page.browser {
		return
	}
	defer b.releaseBrowserFiles(page)
	keys := b.services["KeyService"].(*KeyService)
	keys.mu.Lock()
	clear(keys.selections[page].cipher)
	delete(keys.selections, page)
	keys.mu.Unlock()
	b.config.mu.Lock()
	delete(b.config.plans, page)
	b.config.mu.Unlock()
	b.backup.release(page)
	b.configurationSync.release(page)
	b.SSH.mu.Lock()
	for _, run := range b.SSH.runs {
		if run.page == page {
			run.session.Close()
		}
	}
	b.SSH.mu.Unlock()
	b.Files.releasePage(page)
}

var browserStreams = map[string]bool{"SSHService.Connect": true, "DesktopService.VNC": true, "FileService.Watch": true, "FileService.Search": true, "FileService.Compare": true, "FileService.DirectorySync": true, "SyncService.Watch": true}

func BrowserStream(method string) bool {
	return browserStreams[method]
}
func decodeBrowserArgs(args []json.RawMessage, values ...any) error {
	if len(args) != len(values) {
		return errors.New("请求参数数量无效。")
	}
	for i, value := range values {
		if e := json.Unmarshal(args[i], value); e != nil {
			return errors.New("请求参数无效。")
		}
	}
	return nil
}

type browserSink[T any] struct{ send func(any) error }

func (s browserSink[T]) Send(value T) error { return s.send(value) }
func (b *BrowserServices) Stream(ctx context.Context, method string, args []json.RawMessage, send func(any) error) error {
	if e := workspaceCaller(ctx); e != nil {
		return e
	}
	if send == nil {
		return errors.New("数据通道不可用。")
	}
	var session string
	var attempt int
	switch method {
	case "SyncService.Watch":
		if len(args) != 0 {
			return errors.New("请求参数数量无效。")
		}
		return b.configurationSync.watchStatus(ctx, browserSink[SyncStatusEvent]{send})
	case "SSHService.Connect":
		var r SSHConnectRequest
		if e := decodeBrowserArgs(args, &r); e != nil {
			return e
		}
		return b.SSH.connectStream(ctx, r, browserSink[sshclient.Event]{send})
	case "DesktopService.VNC":
		var d DesktopTarget
		if e := decodeBrowserArgs(args, &session, &attempt, &d); e != nil {
			return e
		}
		return b.desktop.vNCStream(ctx, session, attempt, d, browserSink[DesktopPacket]{send})
	case "FileService.Watch":
		if len(args) != 0 {
			return errors.New("请求参数无效。")
		}
		return b.Files.watchStream(ctx, browserSink[FileEvent]{send})
	case "FileService.Search":
		var r FileSearchRequest
		if e := decodeBrowserArgs(args, &r); e != nil {
			return e
		}
		return b.Files.searchStream(ctx, r, browserSink[filefs.SearchProgress]{send})
	case "FileService.Compare":
		var r ComparisonRequest
		if e := decodeBrowserArgs(args, &r); e != nil {
			return e
		}
		return b.Files.compareStream(ctx, r, browserSink[ComparisonEvent]{send})
	case "FileService.DirectorySync":
		var r DirectorySyncRequest
		if e := decodeBrowserArgs(args, &r); e != nil {
			return e
		}
		return b.Files.directorySyncStream(ctx, r, browserSink[DirectorySyncView]{send})
	default:
		return errors.New("浏览器中不支持此通道。")
	}
}

func (b *BrowserServices) Call(ctx context.Context, method string, args []json.RawMessage) (value any, err error) {
	if e := workspaceCaller(ctx); e != nil {
		return nil, e
	}
	defer func() {
		if recover() != nil {
			value = nil
			err = errors.New("请求未完成，请核对状态后再操作。")
		}
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			err = errors.New("文件区操作未完成，请检查名称、空间和权限。")
		}
	}()
	parts := strings.Split(method, ".")
	if method == "BrowserConfig.Preview" {
		var data []byte
		if e := decodeBrowserArgs(args, &data); e != nil {
			return nil, e
		}
		defer clear(data)
		doc, e := sshconfig.ReadUploaded(data)
		if e != nil {
			return nil, e
		}
		return b.config.previewDocument(ctx, callerPage(ctx), doc, "config")
	}
	if method == "BrowserBackup.Import" {
		return b.importBrowserBackup(ctx, args)
	}
	if strings.HasPrefix(method, "BrowserFiles.") {
		return b.browserFilesCall(ctx, method, args)
	}
	if len(parts) != 2 || BrowserStream(method) {
		return nil, errors.New("请求方法无效。")
	}
	service := b.services[parts[0]]
	if service == nil || !browserMethods[method] {
		return nil, errors.New("此服务不可用。")
	}
	switch method {
	case "SSHConfigService.Preview", "ApplicationService.ReadClipboardText", "ApplicationService.WriteClipboardText", "ApplicationService.SetWindowAppearance", "KeyService.Choose", "KeyService.Pick", "FileService.SelectLocal", "FileService.OpenLocalHome", "FileService.SetTextProtection", "SSHService.LogExport", "FileService.TerminalChooseUploads", "FileService.TerminalChooseDirectory", "DesktopService.RDP", "DesktopService.X11":
		return nil, errors.New("此操作需要桌面窗口，请使用浏览器对应的上传或下载入口。")
	}
	f := reflect.ValueOf(service).MethodByName(parts[1])
	if !f.IsValid() {
		return nil, errors.New("请求方法无效。")
	}
	t := f.Type()
	inputs := []reflect.Value{}
	start := 0
	if t.NumIn() > 0 && t.In(0) == reflect.TypeFor[context.Context]() {
		inputs = append(inputs, reflect.ValueOf(ctx))
		start = 1
	}
	if len(args) != t.NumIn()-start {
		return nil, errors.New("请求参数数量无效。")
	}
	for i, raw := range args {
		typ := t.In(i + start)
		if strings.Contains(typ.String(), "mygo.Channel") {
			return nil, errors.New("此通道不可用。")
		}
		p := reflect.New(typ)
		if e := json.Unmarshal(raw, p.Interface()); e != nil {
			return nil, errors.New("请求参数无效。")
		}
		inputs = append(inputs, p.Elem())
	}
	out := f.Call(inputs)
	for _, v := range out {
		if v.Type().Implements(reflect.TypeFor[error]()) {
			if !v.IsNil() {
				return nil, v.Interface().(error)
			}
		} else {
			value = v.Interface()
		}
	}
	return value, nil
}

func (b *BrowserServices) ConfigureBrowserSync(open func(configsync.Config, []byte) (*configsync.HTTP, bool, error)) {
	b.configurationSync.localSource = open
}
