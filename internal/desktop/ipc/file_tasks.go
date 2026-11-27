package ipc

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"path"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/app/transfers"
	"yaare/internal/domain/workspace"
)

type TransferRequest struct {
	Protocol    string       `json:"protocol,omitempty"`
	Source      FileLocation `json:"source"`
	Destination FileLocation `json:"destination"`
	Names       []string     `json:"names"`
	Policy      string       `json:"policy"`
}
type FileTask struct {
	SourceLabel        string `json:"sourceLabel,omitempty"`
	DestinationLabel   string `json:"destinationLabel,omitempty"`
	ConflictSource     string `json:"conflictSource,omitempty"`
	ConflictTarget     string `json:"conflictTarget,omitempty"`
	ConflictID         string `json:"conflictId,omitempty"`
	DestinationPath    string `json:"destinationPath,omitempty"`
	SourceSession      string `json:"sourceSession,omitempty"`
	DestinationSession string `json:"destinationSession,omitempty"`
	Protocol           string `json:"protocol,omitempty"`
	RecoveryMessage    string `json:"recoveryMessage,omitempty"`
	Reviewed           bool   `json:"reviewed,omitempty"`
	ID                 string `json:"id"`
	Created            int64  `json:"created"`
	SessionID          string `json:"sessionId"`
	Name               string `json:"name"`
	Direction          string `json:"direction"`
	State              string `json:"state"`
	Phase              string `json:"phase"`
	Done               int64  `json:"done"`
	Total              int64  `json:"total"`
	Files              int    `json:"files"`
	Skipped            int    `json:"skipped"`
	Speed              int64  `json:"speed"`
	ETASeconds         int64  `json:"etaSeconds"`
	Current            string `json:"current"`
	Code               string `json:"code"`
	Message            string `json:"message"`
	ConflictDirectory  bool   `json:"conflictDirectory"`
	CanResume          bool   `json:"canResume"`
}

func (v *FileTask) setRoute(request TransferRequest) {
	v.DestinationPath = request.Destination.Path
	v.SourceSession = request.Source.SessionID
	v.DestinationSession = request.Destination.SessionID
}

type FileEvent struct {
	Tasks       []FileTask `json:"tasks"`
	Concurrency int        `json:"concurrency"`
}
type fileJob struct {
	browserExports  map[string]browserExportEntry
	review          *filefs.ReviewedSync
	dropPaths       []string
	page            *callerScope
	caller          context.Context
	request         TransferRequest
	view            FileTask
	ctx             context.Context
	cancel          context.CancelFunc
	sequence        uint64
	executing       bool
	intent          string
	decision        conflictDecision
	reset           bool
	bindings        [2]string
	items           []filefs.Item
	plannedCount    int
	total           int64
	checkpoint      filefs.CopyCheckpoint
	nonce           string
	lastCheckpoint  time.Time
	preparing       bool
	manifestSaved   bool
	manifestPlanned bool
}

type conflictDecision struct {
	action, target string
	directory      bool
	version        filefs.FileVersion
}

func (d *conflictDecision) take(target string, directory bool, version filefs.FileVersion) string {
	previous := *d
	*d = conflictDecision{}
	if previous.target == target && previous.directory == directory && previous.version.Matches(version) {
		return previous.action
	}
	return ""
}

func terminalJob(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled"
}
func (s *FileService) notifyLocked(page *callerScope) {
	for ch := range s.listeners[page] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (s *FileService) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *FileService) limitLocked(page *callerScope) int {
	if n := s.limits[page]; n >= 1 && n <= 8 {
		return n
	}
	return 2
}
func (s *FileService) taskSnapshot(page *callerScope) FileEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := FileEvent{Tasks: []FileTask{}, Concurrency: s.limitLocked(page)}
	for _, j := range s.jobs {
		if j.page == page {
			result.Tasks = append(result.Tasks, j.view)
		}
	}
	sort.Slice(result.Tasks, func(i, j int) bool {
		if result.Tasks[i].Created == result.Tasks[j].Created {
			return result.Tasks[i].ID < result.Tasks[j].ID
		}
		return result.Tasks[i].Created < result.Tasks[j].Created
	})
	return result
}

func (s *FileService) Tasks(ctx context.Context) (FileEvent, error) {
	page := callerPage(ctx)
	if page == nil {
		return FileEvent{}, errors.New("请在桌面窗口中查看文件任务。")
	}
	return s.taskSnapshot(page), nil
}
func (s *FileService) Watch(ctx context.Context, events *mygo.Channel[FileEvent]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.watchStream(ctx, events)
}
func (s *FileService) watchStream(ctx context.Context, events eventSink[FileEvent]) error {
	page := callerPage(ctx)
	if page == nil {
		return errors.New("请在桌面窗口中查看文件任务。")
	}
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	if s.listeners[page] == nil {
		s.listeners[page] = map[chan struct{}]bool{}
	}
	s.listeners[page][ch] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if listeners := s.listeners[page]; listeners != nil {
			delete(listeners, ch)
			if len(listeners) == 0 {
				delete(s.listeners, page)
			}
		}
		s.mu.Unlock()
	}()
	if events.Send(s.taskSnapshot(page)) != nil {
		return nil
	}
	const watchThrottle = 100 * time.Millisecond
	lastSent := time.Now()
	var pending *time.Timer
	var pendingC <-chan time.Time
	defer func() {
		if pending != nil {
			pending.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.stop:
			return nil
		case <-ch:
			since := time.Since(lastSent)
			if since >= watchThrottle {
				if pending != nil {
					pending.Stop()
				}
				pendingC = nil
				if events.Send(s.taskSnapshot(page)) != nil {
					return nil
				}
				lastSent = time.Now()
			} else if pendingC == nil {
				wait := watchThrottle - since
				if pending == nil {
					pending = time.NewTimer(wait)
				} else {
					pending.Stop()
					pending.Reset(wait)
				}
				pendingC = pending.C
			}
		case <-pendingC:
			pendingC = nil
			if events.Send(s.taskSnapshot(page)) != nil {
				return nil
			}
			lastSent = time.Now()
		}
	}
}
func (s *FileService) ConfigureTransfers(ctx context.Context, concurrency int) error {
	page := callerPage(ctx)
	if page == nil {
		return errors.New("请在桌面窗口中设置传输任务。")
	}
	if concurrency < 1 || concurrency > 8 {
		return errors.New("并发任务须为 1–8。")
	}
	s.mu.Lock()
	s.limits[page] = concurrency
	s.notifyLocked(page)
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *FileService) bindRequest(job *fileJob, request *TransferRequest, display *FileTask) error {
	for i, loc := range []*FileLocation{&request.Source, &request.Destination} {
		if loc.Side != "remote" {
			continue
		}
		if s.ssh == nil {
			return errors.New("请重新连接原 SSH 会话后继续任务。")
		}
		s.ssh.mu.Lock()
		run := s.ssh.runs[loc.SessionID]
		if run == nil || run.page != job.page {
			s.ssh.mu.Unlock()
			return errors.New("请重新连接原 SSH 会话后继续任务。")
		}
		attempt, session, name := run.attempt, run.session, run.name
		s.ssh.mu.Unlock()
		identity, err := session.FileIdentity()
		if err != nil {
			return err
		}
		if job.bindings[i] == "" && loc.Attempt != attempt {
			return errors.New("SSH 连接已变化，请重新发起文件任务。")
		}
		if job.bindings[i] != "" && job.bindings[i] != identity {
			return errors.New("当前会话的服务器或用户已变化，不能继续原任务。")
		}
		job.bindings[i] = identity
		loc.Attempt = attempt
		if display != nil {
			label := &display.SourceLabel
			if i == 1 {
				label = &display.DestinationLabel
			}
			if *label == "" {
				*label = transferEndpoint(name, identity)
			}
		}
	}
	return nil
}
func (s *FileService) Transfer(ctx context.Context, request TransferRequest) (string, error) {
	if err := workspaceCaller(ctx); err != nil {
		return "", err
	}
	page := callerPage(ctx)
	if request.Protocol != "" && request.Protocol != "scp" {
		return "", errors.New("请选择 SFTP 或 SCP 传输。")
	}
	if request.Protocol == "scp" && request.Source.Side != "remote" && request.Destination.Side != "remote" {
		return "", errors.New("SCP 传输需要远程端点。")
	}
	if len(request.Names) == 0 || len(request.Names) > 256 {
		return "", errors.New("请选择 1–256 项。")
	}
	for _, name := range request.Names {
		if !filefs.Leaf(name) {
			return "", errors.New("文件名无效。")
		}
	}
	if request.Policy != "ask" && request.Policy != "overwrite" && request.Policy != "skip" && request.Policy != "keepBoth" {
		return "", errors.New("请选择有效的冲突处理方式。")
	}
	for _, loc := range []FileLocation{request.Source, request.Destination} {
		if loc.Side != "local" && loc.Side != "remote" {
			return "", errors.New("文件位置无效。")
		}
		if loc.Side == "local" {
			s.mu.Lock()
			root := s.roots[page]
			valid := root != nil && root.token == loc.Token
			s.mu.Unlock()
			if !valid {
				return "", errors.New("请先选择本地目录。")
			}
		} else if s.ssh == nil || s.ssh.owned(ctx, loc.SessionID, loc.Attempt) == nil {
			return "", errors.New("SSH 会话不可用。")
		}
	}
	if request.Source.Side == request.Destination.Side && request.Source.SessionID == request.Destination.SessionID && request.Source.Token == request.Destination.Token {
		for _, name := range request.Names {
			from, to := path.Join(request.Source.Path, name), path.Join(request.Destination.Path, name)
			if (from == to && request.Policy != "keepBoth" && request.Policy != "ask") || strings.HasPrefix(to, from+"/") {
				return "", errors.New("不能复制到原位置或自身子目录。")
			}
		}
	}
	return s.enqueueTransfer(ctx, request, nil)
}
func (s *FileService) enqueueTransfer(ctx context.Context, request TransferRequest, dropPaths []string) (string, error) {
	return s.enqueueTransferWithReview(ctx, request, dropPaths, nil)
}
func (s *FileService) enqueueTransferWithReview(ctx context.Context, request TransferRequest, dropPaths []string, review *filefs.ReviewedSync) (string, error) {
	return s.enqueueOwnedTransfer(callerPage(ctx), ctx, request, dropPaths, review)
}
func (s *FileService) enqueueOwnedTransfer(page *callerScope, ctx context.Context, request TransferRequest, dropPaths []string, review *filefs.ReviewedSync) (string, error) {
	id := workspace.NewID()
	sessionID := request.Source.SessionID
	if sessionID == "" {
		sessionID = request.Destination.SessionID
	}
	taskCtx, cancel := context.WithCancel(context.Background())
	direction := request.Source.Side + "-" + request.Destination.Side
	if len(dropPaths) > 0 {
		direction = "local-remote"
	}
	job := &fileJob{preparing: true, dropPaths: append([]string(nil), dropPaths...), page: page, caller: ctx, request: request, ctx: taskCtx, cancel: cancel, nonce: id, view: FileTask{ID: id, Created: time.Now().UnixMilli(), SessionID: sessionID, Name: request.Names[0], Direction: direction, State: "queued", ETASeconds: -1}}
	job.view.setRoute(request)
	if err := s.bindRequest(job, &job.request, &job.view); err != nil {
		cancel()
		return "", err
	}
	job.review = review
	job.view.Protocol = request.Protocol
	job.view.Reviewed = review != nil

	s.mu.Lock()
	active := 0
	for _, old := range s.jobs {
		if !terminalJob(old.view.State) || old.view.CanResume {
			active++
		}
	}
	if active >= 64 {
		s.mu.Unlock()
		cancel()
		return "", errors.New("文件队列已满，请先完成或取消现有任务。")
	}
	if len(s.jobs) >= 128 {
		for oldID, old := range s.jobs {
			if old.view.State == "completed" || old.view.State == "cancelled" {
				delete(s.jobs, oldID)
			}
		}
	}
	if len(s.jobs) >= 128 {
		s.mu.Unlock()
		cancel()
		return "", errors.New("文件任务过多，请清理已完成任务。")
	}
	s.sequence++
	job.sequence = s.sequence
	s.jobs[id] = job
	s.mu.Unlock()

	s.persistJob(job, true)

	s.mu.Lock()
	job.preparing = false
	s.notifyLocked(page)
	s.once.Do(func() { go s.scheduler() })
	s.signal()
	s.mu.Unlock()
	return id, nil
}

func (s *FileService) scheduler() {
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		}
		s.mu.Lock()
		select {
		case <-s.stop:
			s.mu.Unlock()
			return
		default:
		}
		for _, job := range s.claimLocked() {
			go s.runTask(job)
		}
		s.mu.Unlock()
	}
}
func (s *FileService) claimLocked() []*fileJob {
	candidates := make([]transfers.Candidate[*callerScope, *fileJob], 0, len(s.jobs))
	for _, job := range s.jobs {
		candidates = append(candidates, transfers.Candidate[*callerScope, *fileJob]{
			Owner: job.page, Job: job, Sequence: job.sequence, Active: job.executing,
			Ready:      !job.preparing && (job.view.State == "queued" || job.view.State == "cancelling"),
			Cancelling: job.intent == "cancel",
		})
	}
	claimed := transfers.Claim(candidates, s.limitLocked)
	for _, job := range claimed {
		job.executing = true
		job.ctx, job.cancel = context.WithCancel(context.Background())
	}
	return claimed
}
func (s *FileService) setTask(job *fileJob, update func(*FileTask)) {
	s.mu.Lock()
	update(&job.view)
	s.notifyLocked(job.page)
	s.mu.Unlock()
}
func (s *FileService) cleanupPartial(job *fileJob) error {
	if job.checkpoint.File == nil || !job.checkpoint.File.Created {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := job.request
	request.Source.Side = "local"
	if err := s.bindRequest(job, &request, nil); err != nil {
		return err
	}
	dest, closeDest, _, err := s.filesystem(fileJobContext{ctx, job.caller}, job.page, request.Destination)
	if err != nil {
		return err
	}
	defer closeDest()

	return job.checkpoint.Abort(dest)
}
func (s *FileService) runTask(job *fileJob) {
	var taskErr error
	var timedOut atomic.Bool
	runCancel := job.cancel
	idle := time.AfterFunc(30*time.Second, func() { timedOut.Store(true); runCancel() })
	defer func() {
		idle.Stop()
		if recover() != nil {
			taskErr = errors.New("file task failed")
		}
		s.mu.Lock()
		intent := job.intent
		var cleanupErr error
		if intent == "cancel" {
			s.mu.Unlock()
			cleanupErr = s.cleanupPartial(job)
			s.mu.Lock()
		}
		p := job.checkpoint.Progress
		v := &job.view
		v.Done = p.Done
		v.Total = p.Total
		v.Files = p.Files
		v.Skipped = p.Skipped
		v.Speed = 0
		v.ETASeconds = -1
		outcome := transfers.ResolveOutcome(intent, job.ctx.Err() != nil, timedOut.Load(), taskErr, cleanupErr, fileError)
		v.State, v.Code, v.Message, v.CanResume = outcome.State, outcome.Code, outcome.Message, outcome.CanResume
		if !v.CanResume {
			s.plannedItems -= job.plannedCount
			job.plannedCount = 0
			job.items = nil
			job.checkpoint.Renamed = nil
			job.checkpoint.Skipped = nil
		}
		if v.State == "completed" {
			job.review = nil
			job.dropPaths = nil
		}
		job.intent = ""
		runCancel()
		s.mu.Unlock()
		s.persistJob(job, true)
		s.mu.Lock()
		job.executing = false
		s.notifyLocked(job.page)
		s.mu.Unlock()
		s.signal()
	}()
	taskErr = s.executeTask(job, idle)
}

func (s *FileService) executeTask(job *fileJob, idle *time.Timer) error {
	s.mu.Lock()
	intent := job.intent
	s.mu.Unlock()
	if intent == "cancel" {
		return nil
	}
	if job.ctx.Err() != nil {
		return job.ctx.Err()
	}
	if job.reset {
		if err := s.cleanupPartial(job); err != nil {
			return err
		}
		job.items = nil
		s.mu.Lock()
		s.plannedItems -= job.plannedCount
		job.plannedCount = 0
		s.mu.Unlock()
		job.checkpoint = filefs.CopyCheckpoint{}
		job.total = 0
		job.nonce = workspace.NewID()
		job.reset = false
		job.manifestSaved = false
		job.manifestPlanned = false
	}
	if job.nonce == "" {
		job.nonce = workspace.NewID()
	}
	s.setTask(job, func(v *FileTask) {
		v.State = "running"
		v.Phase = "planning"
		v.ConflictSource = ""
		v.ConflictTarget = ""
		v.ConflictID = ""
		v.Code = ""
		v.Message = ""
		v.CanResume = false
	})
	var source filefs.FS
	var closeSource func()
	var from string
	var err error
	if len(job.dropPaths) > 0 {
		var dropped *filefs.Dropped
		dropped, err = filefs.OpenDropped(job.dropPaths)
		if err == nil {
			source = dropped
			closeSource = dropped.Close
			from = "/"
		}
	} else {
		source, closeSource, from, err = s.filesystem(job.ctxWithCaller(), job.page, job.request.Source)
	}
	if err != nil {
		return err
	}
	defer closeSource()
	dest, closeDest, to, err := s.filesystem(job.ctxWithCaller(), job.page, job.request.Destination)
	if err != nil {
		return err
	}
	defer closeDest()
	if job.request.Protocol == "scp" {
		if partial := job.checkpoint.File; partial != nil {
			if err = job.checkpoint.Abort(dest); err != nil {
				return err
			}
			job.checkpoint.Progress.Done -= partial.Offset
			job.checkpoint.File = nil
			job.nonce = workspace.NewID()
		}
		for _, side := range []struct {
			location FileLocation
			target   *filefs.FS
		}{{job.request.Source, &source}, {job.request.Destination, &dest}} {
			if side.location.Side != "remote" {
				continue
			}
			run := s.ssh.owned(job.ctxWithCaller(), side.location.SessionID, side.location.Attempt)
			if run == nil {
				return errors.New("SSH 已变化，未开始 SCP 传输。")
			}
			*side.target = filefs.SCP{FS: *side.target, Context: job.ctx, OpenCommand: run.OpenCommand}
		}
	}
	if job.review != nil {
		source, dest = job.review.Wrap(job.ctx, source, dest)
	}
	if job.page != nil && job.page.browser && job.request.Destination.Side == "local" {
		if job.browserExports == nil {
			job.browserExports = map[string]browserExportEntry{}
		}
		dest = filefs.RecordCommits(dest, func(p string, dir bool) { job.browserExports[p] = browserExportEntry{Path: p, Directory: dir} })
	}
	if job.items == nil {
		job.request.Source.Path = from
		job.request.Destination.Path = to
		var items []filefs.Item
		if job.review != nil {
			items = job.review.Items
			job.total = job.review.Total
			to = job.review.DestinationRoot
		} else {
			idle.Reset(30 * time.Second)
			items, job.total, err = filefs.PlanWithProgress(job.ctx, source, from, job.request.Names, func() {
				idle.Reset(30 * time.Second)
			})
		}
		if err != nil {
			return err
		}
		s.mu.Lock()
		if s.plannedItems+len(items) > 200000 {
			s.mu.Unlock()
			return &fileFailure{"FILE_LIMIT", "待传输文件数量过多，请完成或取消其他任务后继续。"}
		}
		s.plannedItems += len(items)
		job.plannedCount = len(items)
		job.items = items
		s.mu.Unlock()
	}
	job.checkpoint.Progress.Total = job.total - job.checkpoint.SkippedBytes
	s.setTask(job, func(v *FileTask) { v.Total = job.checkpoint.Progress.Total })
	conflict := func(name string, directory bool) (string, error) {
		if job.request.Policy != "ask" {
			return job.request.Policy, nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		target := path.Join(to, job.checkpoint.DestinationRelative(name))
		if choice := job.decision.take(target, directory, job.checkpoint.ConflictVersion); choice != "" {
			return choice, nil
		}
		job.view.Current = name
		job.view.ConflictSource = path.Join(from, name)
		job.view.ConflictTarget = target
		job.view.ConflictID = workspace.NewID()
		job.view.ConflictDirectory = directory
		return "", filefs.ErrConflict
	}
	return (transfers.Copy{
		Source: source, Destination: dest, Items: job.items, Root: to, Nonce: job.nonce,
		Total: job.total, Checkpoint: &job.checkpoint, Conflict: conflict,
		BeforeCopy: func() { s.persistJob(job, true) },
		Heartbeat:  func() { idle.Reset(30 * time.Second) },
		OnPhase: func(value string) {
			s.setTask(job, func(v *FileTask) { v.Phase = value; v.Speed = 0; v.ETASeconds = -1 })
		},
		OnProgress: func(p filefs.Progress, rate *transfers.Rate) {
			s.setTask(job, func(v *FileTask) {
				v.Done = p.Done
				v.Total = p.Total
				v.Files = p.Files
				v.Skipped = p.Skipped
				v.Current = p.Current
				if rate != nil {
					v.Speed = rate.Speed
					if rate.Speed > 0 {
						v.ETASeconds = (p.Total - p.Done + rate.Speed - 1) / rate.Speed
					}
				}
			})
			s.persistJob(job, false)
		},
	}).Run(job.ctx)
}

type fileJobContext struct {
	context.Context
	owner context.Context
}

func (c fileJobContext) Value(key any) any {
	if v := c.Context.Value(key); v != nil {
		return v
	}
	return c.owner.Value(key)
}
func (j *fileJob) ctxWithCaller() context.Context { return fileJobContext{j.ctx, j.caller} }
func (s *FileService) controlLocked(job *fileJob, action string) error {
	state := job.view.State
	switch action {
	case "pause":
		if job.request.Protocol == "scp" && job.executing {
			return errors.New("SCP 传输中不支持暂停，可取消；失败后从当前文件头重传。")
		}
		if state != "running" && state != "queued" {
			return nil
		}
		job.view.Speed = 0
		job.view.ETASeconds = -1
		if job.executing {
			job.intent = "pause"
			job.view.State = "pausing"
			job.cancel()
		} else {
			job.view.State = "paused"
			job.view.CanResume = true
			job.view.Message = "任务已暂停。"
		}
	case "cancel":
		if state == "completed" || state == "cancelled" {
			return nil
		}
		if job.executing {
			job.intent = "cancel"
			job.view.State = "cancelling"
			job.cancel()
		} else {
			job.intent = "cancel"
			job.view.State = "cancelling"
			job.view.CanResume = false
		}
	case "resume", "retry":
		if action == "retry" && job.view.Reviewed {
			return errors.New("比较复制任务请重新比较并确认后创建新任务。")
		}
		if job.executing || !(state == "paused" || state == "failed" || state == "cancelled") {
			return nil
		}
		if action == "resume" && !job.view.CanResume {
			return errors.New("文件已变化，请重新传输。")
		}
		request := job.request
		if err := s.bindRequest(job, &request, nil); err != nil {
			return err
		}
		job.request = request
		job.reset = action == "retry"
		if job.reset {
			job.decision = conflictDecision{}
		}
		job.intent = ""
		job.view.State = "queued"
		job.view.Code = ""
		job.view.Message = ""
		job.view.CanResume = false
	case "overwrite", "skip", "keepBoth":
		if state != "conflict" || job.executing {
			return errors.New("文件冲突已处理或任务已结束。")
		}
		request := job.request
		if err := s.bindRequest(job, &request, nil); err != nil {
			return err
		}
		job.request = request
		job.decision = conflictDecision{action: action, target: job.view.ConflictTarget, directory: job.view.ConflictDirectory, version: job.checkpoint.ConflictVersion}
		job.view.State = "queued"
		job.view.Message = ""
	default:
		return errors.New("无效的任务操作。")
	}
	s.notifyLocked(job.page)
	s.signal()
	return nil
}

func (s *FileService) ResolveConflict(ctx context.Context, id, conflictID, action string) error {
	page := callerPage(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if page == nil || job == nil || job.page != page || conflictID == "" || job.view.ConflictID != conflictID ||
		(action != "overwrite" && action != "skip" && action != "keepBoth") {
		return errors.New("文件冲突已处理或任务已结束。")
	}
	return s.controlLocked(job, action)
}
func (s *FileService) Control(ctx context.Context, id, action string) error {
	if action == "overwrite" || action == "skip" || action == "keepBoth" {
		return errors.New("文件冲突已处理或任务已结束。")
	}
	page := callerPage(ctx)
	if page == nil {
		return errors.New("文件任务不可用。")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil || job.page != page {
		return errors.New("文件任务不存在。")
	}
	return s.controlLocked(job, action)
}
func (s *FileService) ControlMany(ctx context.Context, ids []string, action string) error {
	page := callerPage(ctx)
	if page == nil {
		return errors.New("文件任务不可用。")
	}
	if len(ids) > 128 {
		return errors.New("批量任务过多。")
	}
	if action != "pause" && action != "resume" && action != "retry" {
		return errors.New("无效的批量操作。")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	jobs := []*fileJob{}
	for _, id := range ids {
		job := s.jobs[id]
		if job == nil || job.page != page {
			return errors.New("文件任务不存在。")
		}
		if !seen[id] {
			seen[id] = true
			jobs = append(jobs, job)
		}
	}
	failed := false
	for _, job := range jobs {
		if err := s.controlLocked(job, action); err != nil {
			failed = true
		}
	}
	if failed {
		return errors.New("部分任务未能继续，请连接原服务器后单独重试。")
	}
	return nil
}
func (s *FileService) ClearCompleted(ctx context.Context) error {
	page := callerPage(ctx)
	if page == nil {
		return errors.New("文件任务不可用。")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		if job.page == page && job.view.State == "completed" && !job.executing {
			delete(s.jobs, id)
		}
	}
	s.notifyLocked(page)
	return nil
}
