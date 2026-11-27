package ipc

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"path"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/domain/workspace"
)

type DirectorySyncRequest struct {
	ComparisonID  string `json:"comparisonId"`
	DeleteEnabled bool   `json:"deleteEnabled"`
	Confirmed     bool   `json:"confirmed"`
}
type DirectorySyncView struct {
	ID            string                   `json:"id"`
	Revision      int                      `json:"revision"`
	State         string                   `json:"state"`
	Message       string                   `json:"message"`
	Actions       []filefs.BilateralAction `json:"actions"`
	Updated       int64                    `json:"updated"`
	DeleteEnabled bool                     `json:"deleteEnabled"`
}
type DirectorySyncResolution struct {
	ID           string `json:"id"`
	Revision     int    `json:"revision"`
	Path         string `json:"path"`
	Choice       string `json:"choice"`
	Content      string `json:"content"`
	LeftVersion  string `json:"leftVersion"`
	RightVersion string `json:"rightVersion"`
}
type directorySyncRun struct {
	page       *callerScope
	ctx        context.Context
	cancel     context.CancelFunc
	op         sync.Mutex
	scanMu     sync.Mutex
	scanCancel context.CancelFunc
	wake       chan struct{}
	done       chan struct{}
	request    ComparisonRequest
	view       DirectorySyncView
	comparison filefs.Comparison
	base       filefs.SyncBaseline
	cache      filefs.SyncScanCache
	lastFull   time.Time
}

func (run *directorySyncRun) cancelScan() {
	run.scanMu.Lock()
	cancel := run.scanCancel
	run.scanMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (run *directorySyncRun) clearScanCancel() {
	run.scanMu.Lock()
	run.scanCancel = nil
	run.scanMu.Unlock()
}

func (s *FileService) PreviewDirectorySync(ctx context.Context, id string, deleteEnabled bool) ([]filefs.BilateralAction, error) {
	p, e := s.comparison(ctx, id)
	if e != nil {
		return nil, e
	}
	actions, e := filefs.PlanBilateral(p.view.Comparison, filefs.SyncBaseline{}, deleteEnabled)
	return actions, wrapFileError(e)
}

func (s *FileService) DirectorySync(ctx context.Context, request DirectorySyncRequest, events *mygo.Channel[DirectorySyncView]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.directorySyncStream(ctx, request, events)
}
func (s *FileService) directorySyncStream(ctx context.Context, request DirectorySyncRequest, events eventSink[DirectorySyncView]) error {
	if !request.Confirmed || events == nil {
		return errors.New("请先扫描并确认自动同步范围。")
	}
	p, e := s.comparison(ctx, request.ComparisonID)
	if e != nil {
		return e
	}
	if _, e = filefs.PlanBilateral(p.view.Comparison, filefs.SyncBaseline{}, request.DeleteEnabled); e != nil {
		return fileProblem(e)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	run := &directorySyncRun{page: callerPage(ctx), ctx: runCtx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), request: p.request, comparison: p.view.Comparison, base: filefs.SyncBaseline{}, view: DirectorySyncView{ID: workspace.NewID(), State: "scanning", DeleteEnabled: request.DeleteEnabled}}
	s.mu.Lock()
	for _, other := range s.directorySyncs {
		if other.page == run.page {
			s.mu.Unlock()
			return errors.New("此窗口已有自动同步，请先停止原同步。")
		}
	}
	s.directorySyncs[run.view.ID] = run
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.directorySyncs, run.view.ID); s.mu.Unlock(); close(run.done) }()
	for {
		run.op.Lock()
		e = s.directorySyncCycle(run, events.Send)
		run.op.Unlock()
		if e != nil {
			if runCtx.Err() != nil {
				return nil
			}
			return e
		}
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-runCtx.Done():
			timer.Stop()
			return nil
		case <-s.stop:
			timer.Stop()
			return nil
		case <-run.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *FileService) StopDirectorySync(ctx context.Context, sessionID string) error {
	if e := workspaceCaller(ctx); e != nil {
		return e
	}
	page := callerPage(ctx)
	s.mu.Lock()
	var waits []chan struct{}
	for _, run := range s.directorySyncs {
		if run.page == page && run.request.Right.SessionID == sessionID {
			run.cancel()
			waits = append(waits, run.done)
		}
	}
	s.mu.Unlock()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for _, done := range waits {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("自动同步正在停止，请稍后再次关闭连接。")
		}
	}
	return nil
}
func (s *FileService) syncEmit(run *directorySyncRun, send func(DirectorySyncView) error, state, message string) error {
	s.mu.Lock()
	run.view.State, run.view.Message = state, message
	view := run.view
	view.Actions = append([]filefs.BilateralAction{}, run.view.Actions...)
	s.mu.Unlock()
	return send(view)
}
func (s *FileService) scanWithFS(ctx context.Context, run *directorySyncRun, l, r filefs.FS, lp, rp string, force bool) (filefs.Comparison, error) {
	if force {
		run.cache = filefs.SyncScanCache{}
	}
	c, e := filefs.CompareTreesCached(ctx, l, r, lp, rp, runtime.GOOS == "windows", &run.cache)
	return c, wrapFileError(e)
}
func (s *FileService) directorySyncCycle(run *directorySyncRun, send func(DirectorySyncView) error) error {
	if e := s.syncEmit(run, send, "scanning", "正在检查两端变化…"); e != nil {
		return e
	}
	scanCtx, cancelScan := context.WithTimeout(run.ctx, 30*time.Second)
	var preempted atomic.Bool
	run.scanMu.Lock()
	run.scanCancel = func() {
		preempted.Store(true)
		cancelScan()
	}
	run.scanMu.Unlock()
	cleanupScan := func() {
		run.clearScanCancel()
		cancelScan()
	}
	l, cl, lp, e := s.filesystem(scanCtx, run.page, run.request.Left)
	if e != nil {
		cleanupScan()
		if preempted.Load() && run.ctx.Err() == nil {
			return nil
		}
		return e
	}
	r, cr, rp, e := s.filesystem(scanCtx, run.page, run.request.Right)
	if e != nil {
		cl()
		cleanupScan()
		if preempted.Load() && run.ctx.Err() == nil {
			return nil
		}
		return e
	}
	handlesOpen := true
	closeHandles := func() {
		if handlesOpen {
			handlesOpen = false
			cr()
			cl()
		}
	}
	defer closeHandles()

	force := time.Since(run.lastFull) >= 5*time.Minute
	c, e := s.scanWithFS(scanCtx, run, l, r, lp, rp, force)
	run.clearScanCancel()
	if e != nil {
		cancelScan()
		if preempted.Load() && run.ctx.Err() == nil {
			return nil
		}
		return e
	}
	if force {
		run.lastFull = time.Now()
	}
	actions, e := filefs.PlanBilateral(c, run.base, run.view.DeleteEnabled)
	if e != nil {
		cancelScan()
		return fileProblem(e)
	}
	filefs.AcceptSyncBaseline(run.base, c)
	s.mu.Lock()
	if run.view.Revision == 0 || !reflect.DeepEqual(run.comparison.Entries, c.Entries) {
		run.view.Revision++
	}
	run.comparison = c
	run.view.Actions = actions
	run.view.Updated = time.Now().UnixMilli()
	s.mu.Unlock()

	hasCopy := false
	for _, a := range actions {
		if a.Kind == "upload" || a.Kind == "download" {
			hasCopy = true
			break
		}
	}
	if hasCopy {
		closeHandles()
		cancelScan()
		for _, direction := range []string{"upload", "download"} {
			plan := filefs.BilateralCopies(actions, run.request.Left.Path, run.request.Right.Path, direction)
			if len(plan.Items) == 0 {
				continue
			}
			if e = s.syncEmit(run, send, "copying", "正在通过传输队列同步"+map[string]string{"upload": "到远程", "download": "到本地"}[direction]+"…"); e != nil {
				return e
			}
			source, dest := run.request.Left, run.request.Right
			if direction == "download" {
				source, dest = dest, source
			}
			id, err := s.enqueueOwnedTransfer(run.page, run.ctx, TransferRequest{Source: source, Destination: dest, Names: []string{"双向同步"}, Policy: "overwrite"}, nil, plan)
			if err != nil {
				return err
			}
			if err = s.awaitSyncTransfer(run, id); err != nil {
				return err
			}
		}
	}

	hasDelete := false
	for _, a := range actions {
		if a.Kind == "delete-local" || a.Kind == "delete-remote" {
			hasDelete = true
			break
		}
	}
	ensurePostHandles := func() (context.Context, error) {
		if handlesOpen && scanCtx.Err() == nil {
			return scanCtx, nil
		}
		closeHandles()
		cancelScan()
		scanCtx, cancelScan = context.WithTimeout(run.ctx, 30*time.Second)
		var err error
		l, cl, lp, err = s.filesystem(scanCtx, run.page, run.request.Left)
		if err != nil {
			return nil, err
		}
		r, cr, rp, err = s.filesystem(scanCtx, run.page, run.request.Right)
		if err != nil {
			cl()
			return nil, err
		}
		handlesOpen = true
		return scanCtx, nil
	}
	if hasDelete {
		if e = s.syncEmit(run, send, "copying", "正在校验并同步删除…"); e != nil {
			cancelScan()
			return e
		}
		opCtx, err := ensurePostHandles()
		if err != nil {
			cancelScan()
			return err
		}
		if e = wrapFileError(filefs.ApplyBilateralDeletes(opCtx, l, r, run.request.Left.Path, run.request.Right.Path, actions)); e != nil {
			cancelScan()
			return e
		}
	}
	if len(actions) > 0 {
		opCtx, err := ensurePostHandles()
		if err != nil {
			cancelScan()
			return err
		}
		c, e = s.scanWithFS(opCtx, run, l, r, lp, rp, false)
		if e != nil {
			cancelScan()
			return e
		}
		filefs.AcceptSyncBaseline(run.base, c)
		actions, e = filefs.PlanBilateral(c, run.base, run.view.DeleteEnabled)
		if e != nil {
			cancelScan()
			return fileProblem(e)
		}
		s.mu.Lock()
		if !reflect.DeepEqual(run.comparison.Entries, c.Entries) {
			run.view.Revision++
		}
		run.comparison = c
		run.view.Actions = actions
		run.view.Updated = time.Now().UnixMilli()
		s.mu.Unlock()
	}
	closeHandles()
	cancelScan()
	state, message := "watching", "自动同步已开启，每 30 秒检查；每 5 分钟重新校验完整内容。"
	for _, a := range actions {
		if a.Kind == "conflict" {
			state, message = "conflict", "有冲突等待处理，其他安全变化继续同步。"
			break
		}
	}
	return s.syncEmit(run, send, state, message)
}
func (s *FileService) withSyncFiles(run *directorySyncRun, fn func(context.Context, filefs.FS, filefs.FS) error) error {
	ctx, cancel := context.WithTimeout(run.ctx, 30*time.Second)
	defer cancel()
	l, cl, _, e := s.filesystem(ctx, run.page, run.request.Left)
	if e != nil {
		return e
	}
	defer cl()
	r, cr, _, e := s.filesystem(ctx, run.page, run.request.Right)
	if e != nil {
		return e
	}
	defer cr()
	return wrapFileError(fn(ctx, l, r))
}
func (s *FileService) awaitSyncTransfer(run *directorySyncRun, id string) error {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	if s.listeners[run.page] == nil {
		s.listeners[run.page] = map[chan struct{}]bool{}
	}
	s.listeners[run.page][ch] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.listeners[run.page], ch); s.mu.Unlock() }()
	for {
		s.mu.Lock()
		job := s.jobs[id]
		state, message := "", ""
		if job != nil {
			state, message = job.view.State, job.view.Message
		}
		s.mu.Unlock()
		if state == "completed" {
			return nil
		}
		if state == "failed" || state == "cancelled" || state == "conflict" || state == "paused" || state == "" {
			return errors.New("自动同步已停止：" + message + " 请在任务页处理后重新扫描。")
		}
		select {
		case <-ch:
		case <-run.ctx.Done():
			s.mu.Lock()
			if job != nil {
				job.intent = "cancel"
				job.cancel()
			}
			s.mu.Unlock()
			s.signal()
			return run.ctx.Err()
		case <-s.stop:
			return context.Canceled
		}
	}
}
func (s *FileService) directorySync(ctx context.Context, id string) (*directorySyncRun, error) {
	if e := workspaceCaller(ctx); e != nil {
		return nil, e
	}
	s.mu.Lock()
	run := s.directorySyncs[id]
	s.mu.Unlock()
	if run == nil || run.page != callerPage(ctx) || run.ctx.Err() != nil {
		return nil, errors.New("自动同步已停止，请重新扫描。")
	}
	return run, nil
}
func (s *FileService) SyncConflictText(ctx context.Context, id, relative string, revision int) (ComparedText, error) {
	run, e := s.directorySync(ctx, id)
	if e != nil {
		return ComparedText{}, e
	}
	a, e := s.syncConflict(run, relative, revision)
	if e != nil {
		return ComparedText{}, e
	}
	if a.Directory || !a.Left.Exists || !a.Right.Exists {
		return ComparedText{}, errors.New("文本合并需要两端均为普通文件。")
	}
	out := ComparedText{}
	e = s.withSyncFiles(run, func(ctx context.Context, l, r filefs.FS) error {
		var e error
		out.Left, e = filefs.ReadText(ctx, l, path.Join(run.request.Left.Path, relative))
		if e != nil {
			return e
		}
		out.Right, e = filefs.ReadText(ctx, r, path.Join(run.request.Right.Path, relative))
		return e
	})
	return out, e
}
func (s *FileService) syncConflict(run *directorySyncRun, relative string, revision int) (filefs.BilateralAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.view.Revision == revision {
		for _, a := range run.view.Actions {
			if a.Path == relative && a.Kind == "conflict" {
				return a, nil
			}
		}
	}
	return filefs.BilateralAction{}, errors.New("冲突状态已更新，请重新选择。")
}
func (s *FileService) ResolveDirectorySync(ctx context.Context, request DirectorySyncResolution) error {
	run, e := s.directorySync(ctx, request.ID)
	if e != nil {
		return e
	}
	run.cancelScan()
	run.op.Lock()
	defer run.op.Unlock()
	a, e := s.syncConflict(run, request.Path, request.Revision)
	if e != nil {
		return e
	}
	if a.Directory {
		return errors.New("目录类型冲突请手动处理后重新扫描。")
	}
	switch request.Choice {
	case "local", "remote":
		src := a.Left
		a.Kind = "upload"
		if request.Choice == "remote" {
			src = a.Right
			a.Kind = "download"
		}
		if !src.Exists {
			if !run.view.DeleteEnabled {
				return errors.New("此操作会删除另一端文件，请先停止并开启删除同步。")
			}
			a.Kind = "delete-remote"
			if request.Choice == "remote" {
				a.Kind = "delete-local"
			}
			e = s.withSyncFiles(run, func(ctx context.Context, l, r filefs.FS) error {
				return filefs.ApplyBilateralDeletes(ctx, l, r, run.request.Left.Path, run.request.Right.Path, []filefs.BilateralAction{a})
			})
		} else {
			plan := filefs.BilateralCopies([]filefs.BilateralAction{a}, run.request.Left.Path, run.request.Right.Path, a.Kind)
			source, dest := run.request.Left, run.request.Right
			if a.Kind == "download" {
				source, dest = dest, source
			}
			var id string
			id, e = s.enqueueOwnedTransfer(run.page, run.ctx, TransferRequest{Source: source, Destination: dest, Names: []string{"解决同步冲突"}, Policy: "overwrite"}, nil, plan)
			if e == nil {
				e = s.awaitSyncTransfer(run, id)
			}
		}
	case "merge":
		if !a.Left.Exists || !a.Right.Exists || len(request.Content) > filefs.MaxTextBytes {
			return errors.New("合并需要两端普通文本，内容不超过 2 MiB。")
		}
		e = s.withSyncFiles(run, func(ctx context.Context, l, r filefs.FS) error {
			return filefs.MergeBilateralText(ctx, l, r, path.Join(run.request.Left.Path, a.Path), path.Join(run.request.Right.Path, a.Path), request.LeftVersion, request.RightVersion, request.Content, workspace.NewID())
		})
	default:
		return errors.New("请选择本地、远程或文本合并。")
	}
	if e != nil {
		return e
	}
	run.cache = filefs.SyncScanCache{}
	select {
	case run.wake <- struct{}{}:
	default:
	}
	return nil
}
