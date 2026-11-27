package ipc

import (
	"context"
	"encoding/json"
	"errors"

	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/adapters/recovery"
	"yaare/internal/adapters/secrets"
)

type transferManifest struct {
	Version    int
	ID         string
	Request    TransferRequest
	Bindings   [2]string
	LocalRoots [2]string
	Items      []filefs.Item
	Total      int64
	Nonce      string
	DropPaths  []string
	Review     json.RawMessage `json:"review,omitempty"`
}

func transferManifestVersion(protocol string) (int, error) {
	switch protocol {
	case "":
		return 1, nil
	case "scp":
		return 2, nil
	default:
		return 0, errors.New("请选择 SFTP 或 SCP 传输。")
	}
}

type transferState struct {
	View       FileTask
	Checkpoint json.RawMessage
}

type pendingStateWrite struct {
	data []byte
	gen  uint64
	job  *fileJob
}

type transferRecord struct {
	Request        TransferRequest
	View           FileTask
	Bindings       [2]string
	LocalRoots     [2]string
	Items          []filefs.Item
	Total          int64
	Checkpoint     json.RawMessage
	Nonce          string
	DropPaths      []string
	Review         json.RawMessage `json:"review,omitempty"`
	dropPage       *callerScope
	authorizedDrop []string
}
type RecoverableTransfer struct {
	Protocol               string   `json:"protocol,omitempty"`
	DropSource             bool     `json:"dropSource,omitempty"`
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	Direction              string   `json:"direction"`
	Done                   int64    `json:"done"`
	Total                  int64    `json:"total"`
	Created                int64    `json:"created"`
	Source                 string   `json:"source"`
	Destination            string   `json:"destination"`
	SourceConnections      []string `json:"sourceConnections"`
	DestinationConnections []string `json:"destinationConnections"`
}
type RecoveryConnection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type TransferRecoveryView struct {
	Items       []RecoverableTransfer `json:"items"`
	Connections []RecoveryConnection  `json:"connections"`
	Message     string                `json:"message"`
}
type RestoreTransferRequest struct {
	ID                   string `json:"id"`
	SourceSessionID      string `json:"sourceSessionId"`
	DestinationSessionID string `json:"destinationSessionId"`
}

func ConfigureTransferRecovery(s *FileService, root func() (string, error)) {
	var p secrets.Protector = secrets.CurrentUser{}
	if s.ssh != nil {
		p = s.ssh.credentialProtector
	}
	s.recovery = recovery.New(root, p)
}

func InitializeTransferRecovery(s *FileService) {
	if s.recovery == nil {
		return
	}
	entries, err := s.recovery.Initialize()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoverable = map[string]transferRecord{}
	if err != nil {
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			s.recoveryProblem = "中断任务记录未能读取，原记录已保留。请检查配置目录权限后重试。"
		} else {
			s.recoveryProblem = err.Error()
		}
	}
	for _, entry := range entries {
		var manifest transferManifest
		err = json.Unmarshal(entry.Manifest, &manifest)
		clear(entry.Manifest)
		var state transferState
		if err == nil && len(entry.State) > 0 {
			err = json.Unmarshal(entry.State, &state)
		}
		clear(entry.State)
		version, formatErr := transferManifestVersion(manifest.Request.Protocol)
		if err != nil || formatErr != nil || manifest.Version != version || manifest.ID != entry.ID || state.View.ID != entry.ID ||
			len(manifest.Items) > 50000 || !sessionIDPattern.MatchString(manifest.Nonce) {
			s.recoveryProblem = "部分续传记录无效，原记录已保留。"
			continue
		}
		record := transferRecord{
			Request:    manifest.Request,
			View:       state.View,
			Bindings:   manifest.Bindings,
			LocalRoots: manifest.LocalRoots,
			Items:      manifest.Items,
			Total:      manifest.Total,
			Checkpoint: state.Checkpoint,
			Nonce:      manifest.Nonce,
			DropPaths:  manifest.DropPaths,
			Review:     manifest.Review,
		}
		cp, e := filefs.DecodeCheckpoint(record.Checkpoint)
		if e != nil || len(record.Items) == 0 && (cp.Index != 0 || cp.File != nil || cp.Progress.Done != 0) || cp.Index > len(record.Items) || cp.File != nil && (cp.Index >= len(record.Items) || cp.File.Offset > record.Items[cp.Index].Size) {
			s.recoveryProblem = "部分续传记录无效，原记录已保留。"
			continue
		}
		if len(record.DropPaths) > 256 {
			s.recoveryProblem = "部分拖入记录无效，原记录已保留。"
			continue
		}
		if len(record.Review) > 0 {
			if _, e = filefs.RestoreReviewedSync(record.Review); e != nil {
				s.recoveryProblem = e.Error()
				continue
			}
		}
		s.recoverable[entry.ID] = record
	}
}
func recoveryLabel(loc FileLocation, root, binding string) string {
	if loc.Side == "drop" {
		return "拖入的本机文件"
	}
	if loc.Side == "local" {
		return "本地 · " + filepath.Base(root)
	}
	return transferEndpoint("", binding)
}

func transferEndpoint(name, binding string) string {
	address, rest, ok := strings.Cut(binding, "\x00")
	username, _, _ := strings.Cut(rest, "\x00")
	if !ok || address == "" {
		if name == "" {
			return "远程服务器"
		}
		return name
	}
	endpoint := username + "@" + address
	if name == "" || name == endpoint {
		return endpoint
	}
	return name + " · " + endpoint
}
func recoveryPathKey(value string) string {
	value = filepath.Clean(value)
	if runtime.GOOS == "windows" {
		value = strings.ToLower(value)
	}
	return value
}
func (s *FileService) RecoverableTransfers(ctx context.Context) (TransferRecoveryView, error) {
	if err := workspaceCaller(ctx); err != nil {
		return TransferRecoveryView{}, err
	}
	return s.recoveryView(callerPage(ctx)), nil
}
func (s *FileService) recoveryView(page *callerScope) TransferRecoveryView {
	s.mu.Lock()
	result := TransferRecoveryView{Items: []RecoverableTransfer{}, Connections: []RecoveryConnection{}, Message: s.recoveryProblem}
	records := []transferRecord{}
	for _, r := range s.recoverable {
		records = append(records, r)
	}
	s.mu.Unlock()
	identities := map[string]string{}
	if s.ssh != nil {
		s.ssh.mu.Lock()
		for id, run := range s.ssh.runs {
			if run.page == page {
				if identity, err := run.session.FileIdentity(); err == nil {
					identities[id] = identity
					result.Connections = append(result.Connections, RecoveryConnection{id, transferEndpoint(run.name, identity)})
				}
			}
		}
		s.ssh.mu.Unlock()
	}
	for _, r := range records {
		item := RecoverableTransfer{Protocol: r.Request.Protocol, DropSource: len(r.DropPaths) > 0, ID: r.View.ID, Name: r.View.Name, Direction: r.View.Direction, Done: r.View.Done, Total: r.Total, Created: r.View.Created, Source: recoveryLabel(r.Request.Source, r.LocalRoots[0], r.Bindings[0]), Destination: recoveryLabel(r.Request.Destination, r.LocalRoots[1], r.Bindings[1]), SourceConnections: []string{}, DestinationConnections: []string{}}
		if r.View.SourceLabel != "" {
			item.Source = r.View.SourceLabel
		}
		if r.View.DestinationLabel != "" {
			item.Destination = r.View.DestinationLabel
		}
		for id, identity := range identities {
			if identity == r.Bindings[0] {
				item.SourceConnections = append(item.SourceConnections, id)
			}
			if identity == r.Bindings[1] {
				item.DestinationConnections = append(item.DestinationConnections, id)
			}
		}
		result.Items = append(result.Items, item)
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Created < result.Items[j].Created })
	sort.Slice(result.Connections, func(i, j int) bool { return result.Connections[i].ID < result.Connections[j].ID })
	return result
}
func (s *FileService) startRecoveryStateWorkerLocked() {
	if s.stateWorkerStarted || s.stateClosed {
		return
	}
	if s.stateCond == nil {
		s.stateCond = sync.NewCond(&s.recoveryMu)
	}
	if s.pendingStates == nil {
		s.pendingStates = map[string]pendingStateWrite{}
	}
	if s.stateGen == nil {
		s.stateGen = map[string]uint64{}
	}
	s.stateWorkerStarted = true
	s.stateWorkerDone = make(chan struct{})
	go s.runRecoveryStateWorker()
}

func (s *FileService) stopRecoveryStateWorker() {
	s.recoveryMu.Lock()
	s.stateClosed = true
	if s.stateCond != nil {
		s.stateCond.Broadcast()
	}
	done := s.stateWorkerDone
	s.recoveryMu.Unlock()
	if done != nil {
		<-done
	}
}

func (s *FileService) runRecoveryStateWorker() {
	defer close(s.stateWorkerDone)
	for {
		s.recoveryMu.Lock()
		for len(s.pendingStates) == 0 && !s.stateClosed {
			s.stateCond.Wait()
		}
		if len(s.pendingStates) == 0 && s.stateClosed {
			s.recoveryMu.Unlock()
			return
		}
		var nextID string
		var next pendingStateWrite
		for id, item := range s.pendingStates {
			nextID, next = id, item
			delete(s.pendingStates, id)
			break
		}
		s.recoveryMu.Unlock()

		s.recoveryIOMu.Lock()
		s.recoveryMu.Lock()
		currentGen := s.stateGen[nextID]
		s.recoveryMu.Unlock()
		var err error
		if currentGen == next.gen && s.recovery != nil {
			err = s.recovery.SaveState(nextID, next.data)
		}
		s.recoveryIOMu.Unlock()
		clear(next.data)

		if next.job != nil {
			s.mu.Lock()
			if err != nil {
				next.job.view.RecoveryMessage = "未能保存重启恢复记录；当前传输继续，退出后可能需要重新传输。"
			} else {
				next.job.view.RecoveryMessage = ""
			}
			s.mu.Unlock()
		}
	}
}

func (s *FileService) removeRecovery(id string) error {
	if s.recovery == nil {
		return nil
	}
	s.recoveryIOMu.Lock()
	defer s.recoveryIOMu.Unlock()
	s.recoveryMu.Lock()
	if s.stateGen == nil {
		s.stateGen = map[string]uint64{}
	}
	s.stateGen[id]++
	if pending, ok := s.pendingStates[id]; ok {
		clear(pending.data)
		delete(s.pendingStates, id)
	}
	s.recoveryMu.Unlock()
	return s.recovery.Remove(id)
}

func (s *FileService) persistJob(job *fileJob, force bool) {
	if s.recovery == nil {
		return
	}
	if !force && time.Since(job.lastCheckpoint) < 2*time.Second {
		return
	}
	job.lastCheckpoint = time.Now()
	s.mu.Lock()
	view := job.view
	roots := [2]string{}
	for i, loc := range []FileLocation{job.request.Source, job.request.Destination} {
		if loc.Side == "local" {
			if folder := s.roots[job.page]; folder != nil {
				roots[i] = folder.root.Name()
			}
		}
	}
	s.mu.Unlock()
	if view.State == "completed" || view.State == "cancelled" || view.State == "failed" && !view.CanResume {
		_ = s.removeRecovery(view.ID)
		return
	}
	cp, err := filefs.EncodeCheckpoint(job.checkpoint)
	var stateData []byte
	if err == nil {
		stateData, err = json.Marshal(transferState{View: view, Checkpoint: cp})
	}
	needsManifest := !job.manifestSaved || (len(job.items) > 0 && !job.manifestPlanned)
	if !force && !needsManifest && err == nil {
		s.recoveryMu.Lock()
		if !s.stateClosed {
			s.startRecoveryStateWorkerLocked()
			if old, ok := s.pendingStates[view.ID]; ok {
				clear(old.data)
			}
			s.stateGen[view.ID]++
			s.pendingStates[view.ID] = pendingStateWrite{data: stateData, gen: s.stateGen[view.ID], job: job}
			s.stateCond.Signal()
			s.recoveryMu.Unlock()
			return
		}
		s.recoveryMu.Unlock()
	}

	var manifestData []byte
	if err == nil && needsManifest {
		manifest := transferManifest{
			ID:         view.ID,
			Request:    job.request,
			Bindings:   job.bindings,
			LocalRoots: roots,
			Items:      job.items,
			Total:      job.total,
			Nonce:      job.nonce,
			DropPaths:  job.dropPaths,
		}
		manifest.Version, err = transferManifestVersion(job.request.Protocol)
		if err == nil && job.review != nil {
			manifest.Review, err = json.Marshal(job.review)
		}
		if err == nil {
			manifestData, err = json.Marshal(manifest)
		}
	}

	if err == nil {
		s.recoveryIOMu.Lock()
		s.recoveryMu.Lock()
		if s.stateGen == nil {
			s.stateGen = map[string]uint64{}
		}
		s.stateGen[view.ID]++
		if pending, ok := s.pendingStates[view.ID]; ok {
			clear(pending.data)
			delete(s.pendingStates, view.ID)
		}
		s.recoveryMu.Unlock()
		if needsManifest {
			err = s.recovery.SaveManifest(view.ID, manifestData)
			if err == nil {
				job.manifestSaved = true
				if len(job.items) > 0 {
					job.manifestPlanned = true
				}
			}
		}
		if err == nil {
			err = s.recovery.SaveState(view.ID, stateData)
		}
		s.recoveryIOMu.Unlock()
	}
	clear(manifestData)
	clear(stateData)
	s.mu.Lock()
	if err != nil {
		job.view.RecoveryMessage = "未能保存重启恢复记录；当前传输继续，退出后可能需要重新传输。"
	} else {
		job.view.RecoveryMessage = ""
	}
	s.mu.Unlock()
}
func (s *FileService) RestoreTransfer(ctx context.Context, request RestoreTransferRequest) (string, error) {
	if err := workspaceCaller(ctx); err != nil {
		return "", err
	}
	return s.restoreTransfer(ctx, callerPage(ctx), request)
}
func (s *FileService) restoreTransfer(ctx context.Context, page *callerScope, request RestoreTransferRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.recoverable[request.ID]
	if !ok || s.jobs[request.ID] != nil {
		return "", errors.New("续传记录已处理，请刷新列表。")
	}
	active := 0
	for _, job := range s.jobs {
		if !terminalJob(job.view.State) || job.view.CanResume {
			active++
		}
	}
	if active >= 64 || len(s.jobs) >= 128 || s.plannedItems+len(record.Items) > 200000 {
		return "", errors.New("文件队列已满，请完成或清理任务后重试。")
	}
	cp, err := filefs.DecodeCheckpoint(record.Checkpoint)
	if err != nil {
		return "", err
	}
	transfer := record.Request
	transfer.Policy = "ask"
	for i, loc := range []*FileLocation{&transfer.Source, &transfer.Destination} {
		if loc.Side == "drop" && i == 0 {
			if record.dropPage != page || len(record.authorizedDrop) == 0 {
				return "", errors.New("请在恢复窗口重新拖入原来的文件或文件夹，以重新授权源文件。")
			}
		} else if loc.Side == "local" {
			folder := s.roots[page]
			if folder == nil || recoveryPathKey(folder.root.Name()) != recoveryPathKey(record.LocalRoots[i]) {
				return "", errors.New("请先重新选择原本的本地目录：" + filepath.Base(record.LocalRoots[i]))
			}
			loc.Token = folder.token
		} else if loc.Side == "remote" {
			loc.SessionID = request.SourceSessionID
			if i == 1 {
				loc.SessionID = request.DestinationSessionID
			}
			if loc.SessionID == "" {
				return "", errors.New("请连接并选择原服务器后继续。")
			}
		} else {
			return "", errors.New("续传记录的文件位置无效。")
		}
	}
	taskCtx, cancel := context.WithCancel(context.Background())
	job := &fileJob{page: page, caller: ctx, request: transfer, view: record.View, ctx: taskCtx, cancel: cancel, bindings: record.Bindings, items: record.Items, plannedCount: len(record.Items), total: record.Total, checkpoint: cp, nonce: record.Nonce, manifestSaved: true, manifestPlanned: len(record.Items) > 0}
	job.dropPaths = append([]string(nil), record.authorizedDrop...)
	job.view.setRoute(transfer)
	if len(record.Review) > 0 {
		job.review, err = filefs.RestoreReviewedSync(record.Review)
		if err != nil {
			cancel()
			return "", err
		}
	}
	if err = s.bindRequest(job, &job.request, &job.view); err != nil {
		cancel()
		return "", err
	}
	job.view.State = "queued"
	job.view.CanResume = false
	job.view.Code = ""
	job.view.Message = "已确认恢复，将先校验已传内容。"
	job.view.Speed = 0
	job.view.ETASeconds = -1
	job.view.SessionID = transfer.Destination.SessionID
	if transfer.Source.Side == "remote" {
		job.view.SessionID = transfer.Source.SessionID
	}
	s.sequence++
	job.sequence = s.sequence
	s.jobs[job.view.ID] = job
	s.plannedItems += len(job.items)
	delete(s.recoverable, request.ID)
	s.notifyLocked(page)
	s.once.Do(func() { go s.scheduler() })
	s.signal()
	return job.view.ID, nil
}
func (s *FileService) DiscardRecoveredTransfer(ctx context.Context, id string) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	_, ok := s.recoverable[id]
	s.mu.Unlock()
	if !ok {
		return errors.New("续传记录已处理，请刷新列表。")
	}
	if err := s.removeRecovery(id); err != nil {
		return errors.New("恢复记录未移除，请重试。")
	}
	s.mu.Lock()
	delete(s.recoverable, id)
	s.mu.Unlock()
	return nil
}
