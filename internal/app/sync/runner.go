package sync

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"yaare/internal/adapters/configsync"
)

type Runner struct {
	operation sync.Mutex
	bgMu      sync.Mutex
	bgCancel  context.CancelFunc
	wake      chan struct{}
}

func NewRunner() *Runner { return &Runner{wake: make(chan struct{}, 1)} }

func (s *Runner) Unlock() { s.operation.Unlock() }

func (s *Runner) cancelBackground() {
	s.bgMu.Lock()
	cancel := s.bgCancel
	s.bgMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Runner) ClearBackgroundCancel() {
	s.bgMu.Lock()
	s.bgCancel = nil
	s.bgMu.Unlock()
}

func (s *Runner) LockInteractive() {
	s.cancelBackground()
	s.operation.Lock()
}
func (s *Runner) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func stopTimer(t *time.Timer) {
	if t == nil {
		return
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

func (s *Runner) Start(localChanges <-chan struct{}, unsubscribeLocal func(), debounceDelay time.Duration, tick func(context.Context) error, setTask func(string, string, string), onUnavailable func(context.Context), closeResources func()) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	if debounceDelay <= 0 {
		debounceDelay = 5 * time.Second
	}
	go func() {
		defer close(done)
		defer unsubscribeLocal()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		var debounce *time.Timer
		var debounceC <-chan time.Time
		defer func() { stopTimer(debounce) }()
		failures := 0
		for {
			urgent := false
			select {
			case <-ctx.Done():
				return
			case <-localChanges:
				if debounce == nil {
					debounce = time.NewTimer(debounceDelay)
				} else {
					stopTimer(debounce)
					debounce.Reset(debounceDelay)
				}
				debounceC = debounce.C
				continue
			case <-debounceC:
				debounceC = nil
				stopTimer(timer)
				urgent = true
			case <-timer.C:
			case <-s.wake:
				stopTimer(timer)
				stopTimer(debounce)
				debounceC = nil
				failures = 0
				urgent = true
			}
			delay := time.Minute
			if s.operation.TryLock() {
				run, cancelRun := context.WithTimeout(ctx, 90*time.Second)
				var preempted atomic.Bool
				s.bgMu.Lock()
				s.bgCancel = func() {
					preempted.Store(true)
					cancelRun()
				}
				s.bgMu.Unlock()

				e := tick(run)

				s.ClearBackgroundCancel()
				cancelRun()
				if preempted.Load() && ctx.Err() == nil {
					setTask("", "", "")
					delay = 5 * time.Second
				} else if e != nil && !errors.Is(e, configsync.ErrChanged) && ctx.Err() == nil {
					setTask("", "", "retry")
					failures++
					if failures >= 3 {
						statusCtx, finishStatus := context.WithTimeout(ctx, 5*time.Second)
						onUnavailable(statusCtx)
						finishStatus()
					}
					if failures > 5 {
						delay = 15 * time.Minute
					} else {
						delay = time.Duration(failures+1) * time.Minute
					}
				} else {
					setTask("", "", "")
					failures = 0
				}
				s.operation.Unlock()
			} else if urgent {
				delay = 2 * time.Second
			}
			stopTimer(timer)
			timer.Reset(delay)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
			closeResources()
		})
	}
}
