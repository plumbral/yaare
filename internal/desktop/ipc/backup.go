package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"

	"github.com/egoist/mygo"
	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

type BackupExportRequest struct {
	Password           string `json:"password"`
	IncludeCredentials bool   `json:"includeCredentials"`
}
type BackupFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int    `json:"size"`
}
type BackupPreview struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Created            string       `json:"created"`
	IncludeCredentials bool         `json:"includeCredentials"`
	Current            CloudSummary `json:"current"`
	Incoming           CloudSummary `json:"incoming"`
	Added              []string     `json:"added"`
	Removed            []string     `json:"removed"`
	Changed            []string     `json:"changed"`
}
type BackupResult struct {
	Message      string `json:"message"`
	RestorePoint string `json:"restorePoint"`
}
type backupEnvelope struct {
	Format             string            `json:"format"`
	Version            int               `json:"version"`
	MinReaderVersion   int               `json:"minReaderVersion"`
	Created            string            `json:"created"`
	IncludeCredentials bool              `json:"includeCredentials"`
	Workspace          syncmodel.Payload `json:"workspace"`
}
type backupPlan struct {
	id       string
	revision int64
	data     syncmodel.LocalData
	rollback []byte
	pause    sqlite.IntegrationPause
}
type backupSelection struct {
	file    BackupFile
	blob    []byte
	expires time.Time
	plan    *backupPlan
	latest  string
}
type BackupService struct {
	cloud       *CloudService
	mu          sync.Mutex
	selections  map[*callerScope]*backupSelection
	closedHooks map[*callerScope]bool
	operation   chan struct{}
}

func NewBackupService(cloud *CloudService) *BackupService {
	return &BackupService{cloud: cloud, selections: map[*callerScope]*backupSelection{}, closedHooks: map[*callerScope]bool{}, operation: make(chan struct{}, 1)}
}
func nativeBackupCaller(ctx context.Context) error {
	if mygo.CallerWindow(ctx) == nil || mygo.CallerPage(ctx) == nil {
		return errors.New("本地配置导出和导入需要桌面客户端。")
	}
	return nil
}
func (s *BackupService) begin() error {
	select {
	case s.operation <- struct{}{}:
		return nil
	default:
		return errors.New("已有配置操作正在进行，请稍候。")
	}
}
func (s *BackupService) end() { <-s.operation }
func clearBackupPlan(p *backupPlan) {
	if p == nil {
		return
	}
	clear(p.rollback)
	for _, c := range p.data.HostCredentials {
		clear(c.Cipher)
	}
	for _, c := range p.data.ProxyCredentials {
		clear(c.Cipher)
	}
	for _, k := range p.data.Keys {
		clear(k.Cipher)
	}
	for _, i := range p.data.Identities {
		clear(i.Cipher)
	}
}
func (s *BackupService) release(page *callerScope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.closedHooks, page)
	if p := s.selections[page]; p != nil {
		clear(p.blob)
		clearBackupPlan(p.plan)
		p.blob = nil
		p.plan = nil
		delete(s.selections, page)
	}
}
func sealBackup[P ~string | ~[]byte](payload syncmodel.Payload, password P, include bool) ([]byte, error) {
	payload = syncmodel.PackIcons(payload)
	raw, err := json.Marshal(backupEnvelope{Format: "yaare-config-backup", Version: syncmodel.Version, MinReaderVersion: syncmodel.Version, Created: time.Now().UTC().Format(time.RFC3339), IncludeCredentials: include, Workspace: payload})
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	return cloudsync.Seal(raw, password)
}
func (s *BackupService) exportBytes(ctx context.Context, r BackupExportRequest) ([]byte, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	password := []byte(r.Password)
	r.Password = ""
	defer clear(password)
	if !cloudsync.ValidPassword(password) {
		return nil, errors.New("备份密码至少 12 个字符，最多 1 KiB。")
	}
	payload, _, err := (syncapp.NewPortableWorkspace(s.cloud.store, s.cloud.protector)).Capture(ctx, r.IncludeCredentials)
	if err != nil {
		return nil, err
	}
	defer syncmodel.ClearPayload(payload)
	return sealBackup(payload, password, r.IncludeCredentials)
}
func (s *BackupService) Export(ctx context.Context, r BackupExportRequest) (bool, error) {
	if err := nativeBackupCaller(ctx); err != nil {
		return false, err
	}
	if err := s.begin(); err != nil {
		return false, err
	}
	defer s.end()
	blob, err := s.exportBytes(ctx, r)
	r.Password = ""
	if err != nil {
		return false, err
	}
	defer clear(blob)
	dest, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: mygo.CallerWindow(ctx), Title: uiText(callerPage(ctx), "导出加密配置"), DefaultPath: "Yaare-" + time.Now().Format("20060102-150405") + ".yaare", Filters: []mygo.FileFilter{{Name: uiText(callerPage(ctx), "Yaare 加密配置"), Extensions: []string{"yaare"}}}})
	if err != nil {
		return false, errors.New("无法选择备份位置。")
	}
	if dest == "" {
		return false, nil
	}
	if err = cloudsync.WriteArchive(ctx, dest, blob); err != nil {
		return false, err
	}
	return true, nil
}

func (s *BackupService) stageImport(ctx context.Context, name string, blob []byte) (BackupFile, error) {
	if err := workspaceCaller(ctx); err != nil {
		return BackupFile{}, err
	}
	if !cloudsync.ValidEnvelope(blob) {
		return BackupFile{}, errors.New("请选择有效的 Yaare 加密配置文件。")
	}
	page := callerPage(ctx)
	s.mu.Lock()
	if len(s.selections) >= 4 && s.selections[page] == nil {
		s.mu.Unlock()
		return BackupFile{}, errors.New("打开的配置预览过多，请关闭其他预览。")
	}
	s.mu.Unlock()
	s.release(page)
	item := &backupSelection{file: BackupFile{workspace.NewID(), filepath.Base(name), len(blob)}, blob: bytes.Clone(blob), expires: time.Now().Add(10 * time.Minute)}
	registerClose := false
	s.mu.Lock()
	s.selections[page] = item
	if window := mygo.CallerWindow(ctx); window != nil && page != nil {
		if s.closedHooks == nil {
			s.closedHooks = map[*callerScope]bool{}
		}
		if !s.closedHooks[page] {
			s.closedHooks[page] = true
			registerClose = true
		}
	}
	s.mu.Unlock()
	if registerClose {
		mygo.CallerWindow(ctx).OnClosed(func() { s.release(page) })
	}
	time.AfterFunc(10*time.Minute, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.selections[page] == item {
			clear(item.blob)
			clearBackupPlan(item.plan)
			item.blob = nil
			item.plan = nil
		}
	})
	return item.file, nil
}
func (s *BackupService) ChooseImport(ctx context.Context) (BackupFile, error) {
	if err := nativeBackupCaller(ctx); err != nil {
		return BackupFile{}, err
	}
	if err := s.begin(); err != nil {
		return BackupFile{}, err
	}
	defer s.end()
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: mygo.CallerWindow(ctx), Title: uiText(callerPage(ctx), "选择 Yaare 加密配置"), Filters: []mygo.FileFilter{{Name: uiText(callerPage(ctx), "Yaare 加密配置"), Extensions: []string{"yaare"}}}})
	if err != nil {
		return BackupFile{}, errors.New("无法选择配置文件。")
	}
	if len(paths) == 0 {
		return BackupFile{}, nil
	}
	blob, err := cloudsync.ReadArchive(paths[0])
	if err != nil {
		return BackupFile{}, err
	}
	defer clear(blob)
	return s.stageImport(ctx, paths[0], blob)
}
func (s *BackupService) unlockSelection(ctx context.Context, fileID string, pass []byte) (result *backupEnvelope, selection *backupSelection, err error) {
	page := callerPage(ctx)
	s.mu.Lock()
	selected := s.selections[page]
	if selected == nil || selected.file.ID != fileID || time.Now().After(selected.expires) || len(selected.blob) == 0 {
		s.mu.Unlock()
		return nil, nil, errors.New("所选配置已失效，请重新选择。")
	}
	blob := bytes.Clone(selected.blob)
	s.mu.Unlock()
	defer clear(blob)
	raw, err := cloudsync.Open(blob, pass)
	if err != nil {
		return nil, nil, errors.New("备份密码不正确或配置文件已损坏；本机配置未修改。")
	}
	defer clear(raw)
	var envelope backupEnvelope
	defer func() {
		if result == nil {
			syncmodel.ClearPayload(envelope.Workspace)
		}
	}()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || len(bytes.Trim(raw[decoder.InputOffset():], " \t\r\n")) != 0 || envelope.Format != "yaare-config-backup" || !syncmodel.SupportsVersion(envelope.Version, envelope.MinReaderVersion) || envelope.Workspace.Version != syncmodel.Version {
		return nil, nil, errors.New("配置包格式或版本不受支持，请使用相应版本的 Yaare。")
	}
	if err = syncmodel.UnpackIcons(&envelope.Workspace); err != nil {
		return nil, nil, err
	}
	if _, err = time.Parse(time.RFC3339, envelope.Created); err != nil {
		return nil, nil, errors.New("配置包创建信息无效。")
	}

	if !envelope.IncludeCredentials && (len(envelope.Workspace.Keys) > 0 || len(envelope.Workspace.Identities) > 0 || len(envelope.Workspace.HostCredentials) > 0 || len(envelope.Workspace.ProxyCredentials) > 0) {
		return nil, nil, errors.New("配置包的认证范围与内容不一致。")
	}
	if !envelope.IncludeCredentials {
		for _, host := range envelope.Workspace.Snapshot.Hosts {
			if host.IdentityID != "" || host.KeyID != "" {
				return nil, nil, errors.New("未包含认证的配置包不应关联身份或密钥。")
			}
		}
	}
	return &envelope, selected, nil
}

func (s *BackupService) Preview(ctx context.Context, fileID, password string) (BackupPreview, error) {
	if err := workspaceCaller(ctx); err != nil {
		return BackupPreview{}, err
	}
	if err := s.begin(); err != nil {
		return BackupPreview{}, err
	}
	defer s.end()
	page := callerPage(ctx)
	pass := []byte(password)
	password = ""
	defer clear(pass)
	envelope, selected, err := s.unlockSelection(ctx, fileID, pass)
	if err != nil {
		return BackupPreview{}, err
	}
	defer syncmodel.ClearPayload(envelope.Workspace)
	portable := syncapp.NewPortableWorkspace(s.cloud.store, s.cloud.protector)
	local, revision, err := portable.Capture(ctx, true)
	if err != nil {
		return BackupPreview{}, err
	}
	defer syncmodel.ClearPayload(local)
	data, err := portable.Materialize(ctx, envelope.Workspace)
	if err != nil {
		return BackupPreview{}, err
	}
	if !envelope.IncludeCredentials {
		current, e := s.cloud.store.CloudRead(ctx)
		if e != nil {
			return BackupPreview{}, e
		}
		data.Keys = current.Keys
		data.Identities = current.Identities
		preserveHostCredentials(&data, current)
		preserveProxyCredentials(&data, current)
	}
	if err = sqlite.ValidateCloudData(data); err != nil {
		return BackupPreview{}, err
	}
	rollback, err := sealBackup(local, pass, true)
	if err != nil {
		return BackupPreview{}, err
	}
	syncVersion, err := s.cloud.store.SyncVersion(ctx)
	if err != nil {
		clear(rollback)
		return BackupPreview{}, err
	}
	plan := &backupPlan{id: workspace.NewID(), revision: revision, data: data, rollback: rollback, pause: sqlite.IntegrationPause{Version: syncVersion}}
	s.mu.Lock()
	if s.selections[page] != selected || time.Now().After(selected.expires) {
		s.mu.Unlock()
		clearBackupPlan(plan)
		return BackupPreview{}, errors.New("预览已取消，请重新选择。")
	}
	clearBackupPlan(selected.plan)
	selected.plan = plan
	s.mu.Unlock()
	added, removed, changed := syncmodel.Differences(local, envelope.Workspace)
	return BackupPreview{plan.id, selected.file.Name, envelope.Created, envelope.IncludeCredentials, summary(local), summary(envelope.Workspace), added, removed, changed}, nil
}
func (s *BackupService) Apply(ctx context.Context, previewID string) (BackupResult, error) {
	if err := workspaceCaller(ctx); err != nil {
		return BackupResult{}, err
	}
	if err := s.begin(); err != nil {
		return BackupResult{}, err
	}
	defer s.end()
	page := callerPage(ctx)
	s.mu.Lock()
	selected := s.selections[page]
	if selected == nil || selected.plan == nil || selected.plan.id != previewID || time.Now().After(selected.expires) {
		s.mu.Unlock()
		return BackupResult{}, errors.New("导入预览已失效，请重新检查。")
	}
	plan := selected.plan
	selected.plan = nil
	s.mu.Unlock()
	defer clearBackupPlan(plan)
	point, err := s.cloud.store.PreserveBeforeImport(ctx, plan.rollback)
	if err != nil {
		return BackupResult{}, err
	}
	if err = s.cloud.store.RestoreCloud(ctx, plan.revision, plan.data, plan.pause); err != nil {
		return BackupResult{}, err
	}
	s.mu.Lock()
	if s.selections[page] == selected {
		clear(selected.blob)
		selected.blob = nil
		selected.latest = point
	}
	s.mu.Unlock()
	return BackupResult{Message: "配置已导入，自动云同步已暂停。导入前副本使用本次输入的备份密码。", RestorePoint: point}, nil
}
func (s *BackupService) Discard(ctx context.Context) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.selections[callerPage(ctx)]; p != nil {
		clear(p.blob)
		p.blob = nil
		clearBackupPlan(p.plan)
		p.plan = nil
	}
	return nil
}
func (s *BackupService) SaveRestorePoint(ctx context.Context, id string) (bool, error) {
	if err := nativeBackupCaller(ctx); err != nil {
		return false, err
	}
	if err := s.begin(); err != nil {
		return false, err
	}
	defer s.end()
	blob, err := s.restorePointBytes(ctx, id)
	if err != nil {
		return false, err
	}
	defer clear(blob)
	dest, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: mygo.CallerWindow(ctx), Title: uiText(callerPage(ctx), "另存导入前备份"), DefaultPath: "Yaare-before-import.yaare", Filters: []mygo.FileFilter{{Name: uiText(callerPage(ctx), "Yaare 加密配置"), Extensions: []string{"yaare"}}}})
	if err != nil {
		return false, errors.New("无法选择保存位置。")
	}
	if dest == "" {
		return false, nil
	}
	if err = cloudsync.WriteArchive(ctx, dest, blob); err != nil {
		return false, err
	}
	return true, nil
}

func (s *BackupService) restorePointBytes(ctx context.Context, id string) ([]byte, error) {
	s.mu.Lock()
	item := s.selections[callerPage(ctx)]
	allowed := item != nil && item.latest == id && id != ""
	s.mu.Unlock()
	if !allowed {
		return nil, errors.New("该导入前副本不属于当前窗口。")
	}
	blob, err := s.cloud.store.ReadBeforeImport(id)
	if err != nil {
		return nil, errors.New("导入前副本不可读，请检查本机数据目录。")
	}
	return blob, nil
}
