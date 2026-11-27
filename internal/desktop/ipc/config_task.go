package ipc

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"
	syncapp "yaare/internal/app/sync"
)

type SyncTaskStatus struct {
	Configured bool   `json:"configured"`
	Paused     bool   `json:"paused"`
	Kind       string `json:"kind"`
	Stage      string `json:"stage"`
	Issue      string `json:"issue"`
	LastSync   int64  `json:"lastSync"`
	LastBackup int64  `json:"lastBackup"`
}

type SyncStatusEvent struct {
	Settings SyncSettings   `json:"settings"`
	Task     SyncTaskStatus `json:"task"`
	Applied  uint64         `json:"applied"`
	Error    string         `json:"error"`
}

func (s *SyncService) TaskStatus(ctx context.Context) (SyncTaskStatus, error) {
	if err := workspaceCaller(ctx); err != nil {
		return SyncTaskStatus{}, err
	}
	settings, _, err := s.readSettings(ctx)
	return s.taskView(settings), err
}

func (s *SyncService) taskView(settings SyncSettings) SyncTaskStatus {
	s.taskMu.RLock()
	view := s.task
	s.taskMu.RUnlock()
	view.Configured = settings.Target.Provider != ""
	view.Paused = settings.Paused
	view.LastSync, view.LastBackup = settings.LastSync, settings.LastBackup
	if settings.Issue != "" && view.Stage == "" {
		view.Issue = settings.Issue
	}
	return view
}

func (s *SyncService) Watch(ctx context.Context, events *mygo.Channel[SyncStatusEvent]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.watchStatus(ctx, events)
}

func (s *SyncService) watchStatus(ctx context.Context, events eventSink[SyncStatusEvent]) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	settingsChanged, unsubscribe := s.cloud.store.SubscribeSync()
	defer unsubscribe()
	taskChanged := make(chan struct{}, 1)
	s.taskMu.Lock()
	if s.taskListeners == nil {
		s.taskListeners = make(map[chan struct{}]struct{})
	}
	s.taskListeners[taskChanged] = struct{}{}
	s.taskMu.Unlock()
	defer func() { s.taskMu.Lock(); delete(s.taskListeners, taskChanged); s.taskMu.Unlock() }()

	var view, sent SyncStatusEvent
	var retry <-chan time.Time
	first, readSettings := true, true
	for ctx.Err() == nil {
		if readSettings {
			settings, applied, err := s.readSettings(ctx)
			view.Settings, view.Applied, view.Error = settings, applied, ""
			retry = nil
			if err != nil {
				view.Error = err.Error()
				retry = time.After(15 * time.Second)
			}
		}
		view.Task = s.taskView(view.Settings)
		if first || view != sent {
			if err := events.Send(view); err != nil {
				return nil
			}
			first, sent = false, view
		}
		readSettings = true
		select {
		case <-ctx.Done():
			return nil
		case <-settingsChanged:
		case <-retry:
		case <-taskChanged:
			readSettings = false
		}
	}
	return nil
}

func (s *SyncService) notifyTaskLocked() {
	for changes := range s.taskListeners {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
}

func (s *SyncService) setTask(kind, stage, issue string) {
	s.taskMu.Lock()
	if next := (SyncTaskStatus{Kind: kind, Stage: stage, Issue: issue}); next != s.task {
		s.task = next
		s.notifyTaskLocked()
	}
	s.taskMu.Unlock()
}

func (s *SyncService) taskStage(stage string) {
	s.taskMu.Lock()
	if s.task.Kind != "" && s.task.Stage != stage {
		s.task.Stage = stage
		s.notifyTaskLocked()
	}
	s.taskMu.Unlock()
}

func (s *SyncService) Resume(ctx context.Context, version int64) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	s.runner.LockInteractive()
	locked := true
	defer func() {
		if locked {
			s.runner.Unlock()
		}
	}()
	r, c, err := s.load(ctx)
	defer c.clearSecrets()
	if err != nil {
		return err
	}
	if r.Version != version || c.Target.Provider == "" {
		return errors.New("同步设置已修改，请重新读取。")
	}
	if !r.Paused {
		return nil
	}
	if r.BaselineLost && c.CanSync {
		return syncapp.ErrInvalidBaseline
	}
	r.BaselineLost = false
	r.Paused = false
	if err := s.cloud.store.SaveSync(ctx, r); err != nil {
		return err
	}
	s.runner.Unlock()
	locked = false
	s.runner.Wake()
	return nil
}
