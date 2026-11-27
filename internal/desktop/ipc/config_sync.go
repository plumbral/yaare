package ipc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/secret"
	"yaare/internal/domain/syncmodel"

	"github.com/egoist/mygo"
	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

type SyncSettings struct {
	Version       int64             `json:"version"`
	Target        configsync.Config `json:"target"`
	Paused        bool              `json:"paused"`
	HasCredential bool              `json:"hasCredential"`
	HasPassword   bool              `json:"hasPassword"`
	LastSync      int64             `json:"lastSync"`
	LastBackup    int64             `json:"lastBackup"`
	Issue         string            `json:"issue"`
	CanSync       bool              `json:"canSync"`
}
type SyncSaveRequest struct {
	Version    int64             `json:"version"`
	Target     configsync.Config `json:"target"`
	Credential string            `json:"credential"`
	Password   string            `json:"password"`
}
type syncSaved struct {
	Target                       configsync.Config
	Credential, Password         secret.Bytes
	Issue                        string
	IncludeCredentials           bool
	LastSync, LastBackup         int64
	LastBackupHash, ReceiptEpoch string
	CanSync                      bool
}

func (c *syncSaved) clearSecrets() {
	c.Password.Clear()
	c.Credential.Clear()
}

type SyncPreview struct {
	ID                 string                   `json:"id"`
	Local              CloudSummary             `json:"local"`
	Remote             CloudSummary             `json:"remote"`
	Conflicts          []syncmodel.SyncConflict `json:"conflicts"`
	Restoring          bool                     `json:"restoring"`
	Added              []string                 `json:"added"`
	Removed            []string                 `json:"removed"`
	Changed            []string                 `json:"changed"`
	IncludeCredentials bool                     `json:"includeCredentials"`
}
type SyncBackup struct {
	Name    string `json:"name"`
	Created string `json:"created"`
	Size    int64  `json:"size"`
}
type syncPlan struct {
	page                *callerScope
	at                  time.Time
	record              sqlite.SyncRecord
	saved               syncSaved
	local, remote, base syncmodel.Payload
	remoteBlob          []byte
	revision            int64
	dbEpoch             uint64
	etag, hash          string
	generation          int64
	baseGeneration      int64
	remoteUnchanged     bool
	restoring           bool
	include             bool
	receipt             *syncapp.Document
}

func clearSyncPlan(p *syncPlan) {
	if p == nil {
		return
	}
	syncmodel.ClearPayload(p.local)
	syncmodel.ClearPayload(p.remote)
	syncmodel.ClearPayload(p.base)
	clear(p.remoteBlob)
	p.remoteBlob = nil
	if p.receipt != nil {
		syncmodel.ClearPayload(p.receipt.Workspace)
	}
}

type syncValidator struct {
	version           int64
	settings          [32]byte
	stateHash         string
	etag              string
	workspaceRevision int64
	dbEpoch           uint64
}

type SyncService struct {
	cloud       *CloudService
	transport   *http.Transport
	localSource func(configsync.Config, []byte) (*configsync.HTTP, bool, error)
	keys        cloudsync.KeyCache
	validator   syncValidator

	runner        *syncapp.Runner
	debounceDelay time.Duration
	mu            sync.Mutex
	plans         map[string]syncPlan
	closedHooks   map[*callerScope]bool
	taskMu        sync.RWMutex
	task          SyncTaskStatus
	taskListeners map[chan struct{}]struct{}
}

func NewSyncService(cloud *CloudService) *SyncService {
	s := &SyncService{cloud: cloud, transport: configsync.NewTransport(), plans: map[string]syncPlan{}, runner: syncapp.NewRunner()}
	return s
}

func (s *SyncService) load(ctx context.Context) (sqlite.SyncRecord, syncSaved, error) {
	r, e := s.cloud.store.ReadSync(ctx)
	if e != nil {
		return r, syncSaved{}, e
	}
	return s.decodeSettings(r)
}
func (s *SyncService) decodeSettings(r sqlite.SyncRecord) (sqlite.SyncRecord, syncSaved, error) {
	if r.Version == 0 {
		return r, syncSaved{IncludeCredentials: true}, nil
	}
	var c syncSaved
	raw, e := s.cloud.protector.Unprotect(r.Settings, "config-sync-v1")
	if e != nil {
		return r, c, errors.New("无法解锁同步设置，请检查本机凭证存储。")
	}
	defer clear(raw)
	if json.Unmarshal(raw, &c) != nil {
		c.clearSecrets()
		return r, c, errors.New("同步设置无效，原配置已保留。")
	}
	if !validSyncProvider(c.Target.Provider) {
		c.clearSecrets()
		return sqlite.SyncRecord{Version: r.Version}, syncSaved{IncludeCredentials: true}, nil
	}
	c.IncludeCredentials = true
	return r, c, nil
}
func (s *SyncService) record(r sqlite.SyncRecord, c syncSaved, baseline *syncapp.Document) (sqlite.SyncRecord, error) {
	var state []byte
	if baseline != nil {
		var e error
		state, e = syncapp.SealDocument(*baseline, c.Password)
		if e != nil {
			return r, e
		}
	}
	return s.recordWithState(r, c, state)
}
func (s *SyncService) recordWithState(r sqlite.SyncRecord, c syncSaved, state []byte) (sqlite.SyncRecord, error) {
	raw, e := json.Marshal(c)
	if e != nil {
		return r, e
	}
	defer clear(raw)
	r.Settings, e = s.cloud.protector.Protect(raw, "config-sync-v1")
	if e != nil {
		return r, e
	}
	if state != nil {
		r.State = bytes.Clone(state)
	}
	return r, nil
}
func syncSettingsView(r sqlite.SyncRecord, c syncSaved) SyncSettings {
	if r.BaselineLost {
		c.Issue = "baseline"
	}
	return SyncSettings{r.Version, c.Target, r.Paused, len(c.Credential) != 0, len(c.Password) != 0, c.LastSync, c.LastBackup, c.Issue, c.CanSync}
}
func (s *SyncService) readSettings(ctx context.Context) (SyncSettings, uint64, error) {
	r, applied, err := s.cloud.store.ReadSyncSettings(ctx)
	if err != nil {
		return SyncSettings{}, applied, err
	}
	r, saved, err := s.decodeSettings(r)
	defer saved.clearSecrets()
	return syncSettingsView(r, saved), applied, err
}
func (s *SyncService) Settings(ctx context.Context) (SyncSettings, error) {
	if e := workspaceCaller(ctx); e != nil {
		return SyncSettings{}, e
	}
	settings, _, err := s.readSettings(ctx)
	return settings, err
}
func (s *SyncService) Save(ctx context.Context, q SyncSaveRequest) (SyncSettings, error) {
	if e := workspaceCaller(ctx); e != nil {
		return SyncSettings{}, e
	}
	if !validSyncProvider(q.Target.Provider) {
		return SyncSettings{}, errors.New("存储类型无效。")
	}
	s.runner.LockInteractive()
	locked := true
	defer func() {
		if locked {
			s.runner.Unlock()
		}
	}()
	r, c, e := s.load(ctx)
	defer c.clearSecrets()
	if e != nil {
		return SyncSettings{}, e
	}
	if r.Version != q.Version {
		return SyncSettings{}, errors.New("同步设置已修改，请重新读取。")
	}
	password, credential := []byte(q.Password), []byte(q.Credential)
	q.Password, q.Credential = "", ""
	defer clear(password)
	defer clear(credential)
	if len(password) != 0 && !cloudsync.ValidPassword(password) {
		return SyncSettings{}, errors.New("同步密码至少 12 个字符，最多 1 KiB。")
	}
	targetChanged := c.Target != q.Target
	credentialChanged := len(credential) != 0 && !bytes.Equal(c.Credential, credential)
	changed := targetChanged || len(password) != 0 && !bytes.Equal(c.Password, password)
	if c.Target.Provider != q.Target.Provider || c.Target.Endpoint != q.Target.Endpoint || c.Target.Username != q.Target.Username {
		c.Credential.Clear()
	}
	c.Target = q.Target
	if len(credential) != 0 {
		c.Credential.Clear()
		c.Credential = credential
	}
	if len(password) != 0 {
		c.Password.Clear()
		c.Password = password
	}
	if changed {
		r.State = nil
		c.LastSync = 0
		c.LastBackup = 0
		c.LastBackupHash = ""
	}
	if changed || r.Paused {
		c.ReceiptEpoch = workspace.NewID()
		if c.ReceiptEpoch == "" {
			return SyncSettings{}, errors.New("无法安全记录待确认的同步写入。")
		}
	}
	c.IncludeCredentials = true
	c.Issue = ""
	c.CanSync = false
	if c.Target.Provider != "" {
		if len(c.Password) == 0 {
			return SyncSettings{}, errors.New("请设置独立的配置加密密码。")
		}
		target, e := s.target(c)
		if e != nil {
			return SyncSettings{}, e
		}
		defer target.Close()
		e = target.Prepare(ctx, "probe-"+workspace.NewID(), true)
		c.CanSync = e == nil
		if e != nil && !errors.Is(e, configsync.ErrConditions) {
			return SyncSettings{}, e
		}
	}
	r.BaselineLost = r.BaselineLost && c.Target.Provider != ""
	r.Paused = r.BaselineLost
	r, e = s.record(r, c, nil)
	if e != nil {
		return SyncSettings{}, e
	}
	if e = s.cloud.store.SaveSync(ctx, r); e != nil {
		return SyncSettings{}, e
	}
	s.validator = syncValidator{}
	if changed || credentialChanged {
		s.keys.Clear()
	}
	if targetChanged || credentialChanged {
		s.transport.CloseIdleConnections()
	}
	r.Version++
	s.setTask("", "", "")
	s.runner.Unlock()
	locked = false
	s.runner.Wake()
	return syncSettingsView(r, c), nil
}

func (s *SyncService) capture(ctx context.Context, c syncSaved) (syncmodel.Payload, int64, error) {
	p, revision, e := (syncapp.NewPortableWorkspace(s.cloud.store, s.cloud.protector)).Capture(ctx, c.IncludeCredentials)
	if e != nil {
		return p, revision, e
	}
	defer syncmodel.ClearPayload(p)
	canonical, e := syncmodel.Canonicalize(p)
	if e != nil {
		syncmodel.ClearPayload(canonical)
		return syncmodel.Payload{}, revision, e
	}
	return canonical, revision, nil
}
func validSyncProvider(provider string) bool {
	return provider == "" || provider == "webdav" || provider == "s3" || provider == "yaare"
}

func (s *SyncService) target(c syncSaved) (*configsync.HTTP, error) {
	cfg := c.Target
	if cfg.Provider == "yaare" && s.localSource != nil {
		if client, ok, e := s.localSource(cfg, c.Credential); ok {
			return client, e
		}
	}
	return configsync.NewHTTPWithTransport(cfg, c.Credential, s.transport)
}

func stateDigestHex(state []byte) string {
	if len(state) == 0 {
		return ""
	}
	sum := sha256.Sum256(state)
	return hex.EncodeToString(sum[:])
}

func (s *SyncService) rememberValidator(r sqlite.SyncRecord, etag string, workspaceRevision int64, dbEpoch uint64) {
	s.validator = syncValidator{
		version:           r.Version,
		settings:          sha256.Sum256(r.Settings),
		stateHash:         stateDigestHex(r.State),
		etag:              etag,
		workspaceRevision: workspaceRevision,
		dbEpoch:           dbEpoch,
	}
}

func (s *SyncService) hasPendingReceipt() (bool, error) {
	path, err := s.cloud.store.SyncReceiptPath()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return true, nil
	}
	return true, nil
}

func (s *SyncService) currentValidator(r sqlite.SyncRecord) (string, error) {
	cache := s.validator
	if r.Paused || len(r.State) == 0 || cache.etag == "" || cache.version != r.Version ||
		cache.settings != sha256.Sum256(r.Settings) || cache.stateHash != stateDigestHex(r.State) {
		return "", nil
	}
	pending, err := s.hasPendingReceipt()
	if err != nil || pending {
		return "", err
	}
	return cache.etag, nil
}

func (s *SyncService) idleValidator(meta sqlite.SyncMeta) (string, error) {
	cache := s.validator
	if meta.Record.Paused || meta.StateHash == "" || cache.etag == "" ||
		cache.version != meta.Record.Version ||
		cache.settings != sha256.Sum256(meta.Record.Settings) ||
		cache.stateHash != meta.StateHash ||
		cache.workspaceRevision != meta.WorkspaceRevision ||
		cache.dbEpoch != meta.DBEpoch {
		return "", nil
	}
	pending, err := s.hasPendingReceipt()
	if err != nil || pending {
		return "", err
	}
	return cache.etag, nil
}

func (s *SyncService) prepare(ctx context.Context, r sqlite.SyncRecord, c syncSaved, restoreName string, background bool) (syncPlan, error) {
	return s.prepareWithPrefetch(ctx, r, c, restoreName, background, nil)
}

func (s *SyncService) prepareWithPrefetch(ctx context.Context, r sqlite.SyncRecord, c syncSaved, restoreName string, background bool, prefetch *syncapp.PrefetchedRemote) (syncPlan, error) {
	p := syncPlan{record: r, saved: c, page: callerPage(ctx), at: time.Now(), include: c.IncludeCredentials, dbEpoch: s.cloud.store.DBEpoch()}
	var e error
	p.local, p.revision, e = s.capture(ctx, c)
	if e != nil {
		return p, e
	}
	p.hash = syncmodel.PayloadHash(p.local)
	remote, err := syncapp.ReadRemote(ctx, syncapp.RemoteRequest{
		Keys: s.keys.Bind(c.ReceiptEpoch), Baseline: r.State, Password: c.Password, IncludeCredentials: c.IncludeCredentials,
		RestoreName: restoreName, Background: background, Prefetch: prefetch,
		Open:      func() (configsync.Storage, error) { return s.target(c) },
		Validator: func() (string, error) { return s.currentValidator(r) },
	})
	if err != nil {
		clearSyncPlan(&p)
		if errors.Is(err, syncapp.ErrInvalidBaseline) {
			if pauseErr := s.cloud.store.PauseLostBaseline(ctx, r.Version); pauseErr != nil {
				return syncPlan{}, pauseErr
			}
		}
		return syncPlan{}, err
	}
	p.base, p.baseGeneration = remote.Base, remote.BaseGeneration
	p.remote, p.remoteBlob = remote.Document.Workspace, remote.BaselineBlob
	p.include, p.etag, p.generation = remote.Document.IncludeCredentials, remote.ETag, remote.Document.Generation
	p.restoring, p.remoteUnchanged = restoreName != "", remote.Unchanged
	if remote.PublishedBlob != nil {
		if err = s.reconcileSyncReceipt(&p, remote.Document, remote.PublishedBlob); err != nil {
			clearSyncPlan(&p)
			return syncPlan{}, err
		}
	}
	return p, nil
}
func (s *SyncService) preview(ctx context.Context, restore string) (SyncPreview, error) {
	r, c, e := s.load(ctx)
	defer c.clearSecrets()
	if e != nil {
		return SyncPreview{}, e
	}
	if c.Target.Provider == "" {
		return SyncPreview{}, errors.New("请先配置同步与备份目标。")
	}
	if restore == "" && !c.CanSync {
		return SyncPreview{}, configsync.ErrConditions
	}
	p, e := s.prepare(ctx, r, c, restore, false)
	if e != nil {
		return SyncPreview{}, e
	}
	stored := false
	defer func() {
		if !stored {
			clearSyncPlan(&p)
		}
	}()
	merged, conflicts, e := syncmodel.Merge(p.base, p.local, p.remote, nil)
	defer syncmodel.ClearPayload(merged)
	if e != nil {
		return SyncPreview{}, e
	}
	if p.restoring {
		conflicts = []syncmodel.SyncConflict{}
		merged = p.remote
	}
	validateTarget := p.remote
	if !p.restoring && len(conflicts) == 0 {
		validateTarget = merged
	}
	if e = s.validatePayload(ctx, validateTarget, p.include); e != nil {
		return SyncPreview{}, e
	}
	added, removed, changed := syncmodel.Differences(p.local, merged)
	id := workspace.NewID()
	s.mu.Lock()
	for key, plan := range s.plans {
		if plan.page == p.page || time.Since(plan.at) > 5*time.Minute {
			clearSyncPlan(&plan)
			delete(s.plans, key)
		}
	}
	if len(s.plans) >= 8 {
		s.mu.Unlock()
		return SyncPreview{}, errors.New("同步预览过多，请关闭其他预览。")
	}
	p.saved.Password, p.saved.Credential = nil, nil
	s.plans[id] = p
	stored = true
	registerClose := false
	if window := mygo.CallerWindow(ctx); window != nil && p.page != nil {
		if s.closedHooks == nil {
			s.closedHooks = map[*callerScope]bool{}
		}
		if !s.closedHooks[p.page] {
			s.closedHooks[p.page] = true
			registerClose = true
		}
	}
	s.mu.Unlock()
	if registerClose {
		page := p.page
		mygo.CallerWindow(ctx).OnClosed(func() { s.release(page) })
	}
	time.AfterFunc(5*time.Minute, func() {
		s.mu.Lock()
		if plan, ok := s.plans[id]; ok {
			clearSyncPlan(&plan)
			delete(s.plans, id)
		}
		s.mu.Unlock()
	})
	return SyncPreview{id, summary(p.local), summary(p.remote), conflicts, p.restoring, added, removed, changed, p.include}, nil
}
func (s *SyncService) Preview(ctx context.Context) (SyncPreview, error) {
	if e := workspaceCaller(ctx); e != nil {
		return SyncPreview{}, e
	}
	s.runner.LockInteractive()
	defer s.runner.Unlock()
	return s.preview(ctx, "")
}
func (s *SyncService) PreviewRestore(ctx context.Context, name string) (SyncPreview, error) {
	if e := workspaceCaller(ctx); e != nil {
		return SyncPreview{}, e
	}
	if !configsync.ValidName(name) || len(name) < 8 || name[:8] != "backups/" {
		return SyncPreview{}, errors.New("备份名称无效。")
	}
	s.runner.LockInteractive()
	defer s.runner.Unlock()
	return s.preview(ctx, name)
}
func (s *SyncService) Apply(ctx context.Context, id string, choices map[string]string) (CloudResult, error) {
	if e := workspaceCaller(ctx); e != nil {
		return CloudResult{}, e
	}
	s.runner.LockInteractive()
	defer s.runner.Unlock()
	s.mu.Lock()
	p, ok := s.plans[id]
	expired := ok && time.Since(p.at) > 5*time.Minute
	if expired {
		clearSyncPlan(&p)
		delete(s.plans, id)
	}
	if !ok || expired || p.page != callerPage(ctx) {
		s.mu.Unlock()
		return CloudResult{}, errors.New("同步预览已失效，请重新检查。")
	}
	delete(s.plans, id)
	s.mu.Unlock()
	defer clearSyncPlan(&p)
	r, c, e := s.load(ctx)
	defer c.clearSecrets()
	if e != nil {
		return CloudResult{}, e
	}
	if r.Version != p.record.Version {
		return CloudResult{}, errors.New("同步设置已改变，请重新检查。")
	}
	p.saved = c
	if !p.restoring {
		s.setTask("sync", "prepare", "")
		defer s.setTask("", "", "")
	}
	local, revision, e := s.capture(ctx, c)
	if e != nil {
		return CloudResult{}, e
	}
	defer syncmodel.ClearPayload(local)
	if revision != p.revision || syncmodel.PayloadHash(local) != p.hash {
		return CloudResult{}, errors.New("本机配置已改变，请重新检查。")
	}
	target, e := s.target(c)
	if e != nil {
		return CloudResult{}, e
	}
	defer target.Close()
	if p.restoring {
		return s.restore(ctx, p, target)
	}
	return s.applyPlan(ctx, p, target, choices)
}
func (s *SyncService) materialize(ctx context.Context, p syncmodel.Payload, include bool) (syncmodel.LocalData, error) {
	return s.prepareCloudData(ctx, p, include, true)
}
func (s *SyncService) validatePayload(ctx context.Context, p syncmodel.Payload, include bool) error {
	_, err := s.prepareCloudData(ctx, p, include, false)
	return err
}
func (s *SyncService) prepareCloudData(ctx context.Context, p syncmodel.Payload, include bool, seal bool) (syncmodel.LocalData, error) {
	var data syncmodel.LocalData
	var e error
	if seal {
		data, e = s.cloud.materialize(ctx, p)
	} else {
		data, e = s.cloud.validate(ctx, p)
	}
	if e != nil {
		return data, e
	}
	if !include {
		current, e := s.cloud.store.CloudRead(ctx)
		if e != nil {
			return data, e
		}
		preserveProxyCredentials(&data, current)
		data.Keys = current.Keys
		data.Identities = current.Identities
		oldHosts := map[string]workspace.Host{}
		for _, h := range current.Snapshot.Hosts {
			oldHosts[h.ID] = h
		}
		identities := map[string]workspace.Identity{}
		for _, i := range current.Identities {
			identities[i.ID] = i.Identity
		}
		for i := range data.Snapshot.Hosts {
			h := &data.Snapshot.Hosts[i]
			old, ok := oldHosts[h.ID]
			if !ok || old.Address != h.Address || old.Port != h.Port {
				continue
			}
			if identity, ok := identities[old.IdentityID]; ok && identity.Username == h.Username && identity.Authentication == h.Authentication {
				h.IdentityID = identity.ID
			} else if old.IdentityID == "" && old.Username == h.Username && old.Authentication == h.Authentication {
				h.KeyID = old.KeyID
			}
		}
		preserveHostCredentials(&data, current)
	}
	return data, sqlite.ValidateCloudData(data)
}
func (s *SyncService) applyPlan(ctx context.Context, p syncPlan, target configsync.Storage, choices map[string]string) (CloudResult, error) {
	plan := syncapp.MergePlan{
		Base: p.base, Local: p.local, Remote: p.remote, RemoteBlob: p.remoteBlob,
		Keys: s.keys.Bind(p.saved.ReceiptEpoch), ETag: p.etag, Hash: p.hash, Issue: p.saved.Issue, Password: p.saved.Password,
		Generation: p.generation, BaseGeneration: p.baseGeneration, RemoteUnchanged: p.remoteUnchanged,
		IncludeCredentials: p.saved.IncludeCredentials, HasBaseline: len(p.record.State) > 0,
	}
	result, err := syncapp.PublishMerge(ctx, plan, target, choices, syncapp.PublicationHooks{
		Reconcile: func(ctx context.Context, plan *syncapp.MergePlan) (bool, error) {
			pulled, err := s.commitSyncReceipt(ctx, &p)
			plan.Hash, plan.BaseGeneration, plan.Issue, plan.HasBaseline = p.hash, p.baseGeneration, p.saved.Issue, len(p.record.State) > 0
			return pulled, err
		},
		Materialize: s.materialize,
		Validate:    s.validatePayload,
		Stage:       s.taskStage,
		SaveReceipt: func(ctx context.Context, blob []byte) error { return s.saveSyncReceipt(ctx, p, blob) },
		AcceptUnchanged: func(etag string) {
			s.rememberValidator(p.record, etag, p.revision, p.dbEpoch)
			s.clearSyncReceipt()
		},
		Commit: func(ctx context.Context, commit syncapp.MergeCommit) error {
			return s.commitMerge(ctx, p, commit)
		},
		ReportHistoryError: s.reportHistoryError,
	})
	return CloudResult{State: result.State, Pulled: result.Pulled}, err
}

func (s *SyncService) commitMerge(ctx context.Context, p syncPlan, commit syncapp.MergeCommit) error {
	s.runner.ClearBackgroundCancel()
	s.taskStage("confirm")
	c := p.saved
	c.LastSync = time.Now().UnixMilli()
	if commit.Publishing {
		c.LastBackup = c.LastSync
	}
	c.Issue = ""
	var e error
	var r sqlite.SyncRecord
	p.record.BaselineLost = false
	if len(commit.State) > 0 {
		r, e = s.recordWithState(p.record, c, commit.State)
	} else if commit.RetainBaseline {
		r, e = s.recordWithState(p.record, c, nil)
	} else {
		r, e = s.record(p.record, c, &commit.Document)
	}
	if e != nil {
		return e
	}
	if commit.Pulled {
		e = s.cloud.store.ApplySync(ctx, p.revision, commit.Data, r)
	} else {
		e = s.cloud.store.SaveSync(ctx, r)
	}
	if e != nil {
		return e
	}
	r.Version++
	nextRevision := p.revision
	if commit.Pulled {
		nextRevision++
	}
	s.rememberValidator(r, commit.ETag, nextRevision, p.dbEpoch)
	s.clearSyncReceipt()
	return nil
}

func (s *SyncService) reportHistoryError(ctx context.Context) {
	current, saved, err := s.load(ctx)
	defer saved.clearSecrets()
	if err != nil {
		return
	}
	saved.Issue = "history"
	if current, err = s.record(current, saved, nil); err == nil {
		_ = s.cloud.store.SaveSync(ctx, current)
	}
}

func (s *SyncService) release(page *callerScope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.closedHooks, page)
	for id, p := range s.plans {
		if p.page == page {
			clearSyncPlan(&p)
			delete(s.plans, id)
		}
	}
}
func (s *SyncService) releaseAllPlans() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.plans {
		clearSyncPlan(&p)
		delete(s.plans, id)
	}
	clear(s.closedHooks)
}
