package ipc

import (
	"context"

	"sync/atomic"
	"testing"
	"time"
)

func TestBroadcastOwnedTargetsAndIdempotentSubmission(t *testing.T) {
	s, shutdown := NewSSHService(NewKeyService())
	defer shutdown()
	page := &callerScope{}
	var writes atomic.Int32
	request := BroadcastRequest{ID: "once", Command: "printf '中文'", Execute: true}
	writers := []broadcastWriter{}
	for i := 0; i < 8; i++ {
		id := string(rune('a' + i))
		request.Targets = append(request.Targets, BroadcastTarget{SessionID: id, Attempt: 1})
		valid := i != 3
		writers = append(writers, broadcastWriter{valid: func() bool { return valid }, write: func(data []byte) bool {
			writes.Add(1)
			if string(data) != "printf '中文'\r" {
				panic("incorrect command framing")
			}
			return true
		}})
	}
	result, e := s.beginBroadcast(context.Background(), page, request, writers)
	if e != nil || result.Pending || len(result.Items) != 8 || writes.Load() != 7 || result.Items[3].State != "not-sent" {
		t.Fatal("delivery", e, writes.Load())
	}
	for i, item := range result.Items {
		if i != 3 && item.State != "sent" {
			t.Fatal("healthy target not sent")
		}
	}
	if _, e = s.beginBroadcast(context.Background(), page, request, writers); e != nil || writes.Load() != 7 {
		t.Fatal("replayed request", e)
	}
	if _, e = s.beginBroadcast(context.Background(), &callerScope{}, request, writers); e == nil {
		t.Fatal("cross-page cached result")
	}
	if _, e = s.Broadcast(context.Background(), request); e == nil {
		t.Fatal("unowned public send")
	}
	if _, e = s.BroadcastStatus(context.Background(), "once"); e == nil {
		t.Fatal("unowned status")
	}
}
func TestBroadcastUnconfirmedWritesCannotBeAutomaticallyReplayed(t *testing.T) {
	s, shutdown := NewSSHService(NewKeyService())
	defer shutdown()
	page := &callerScope{}
	started, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := BroadcastRequest{ID: "pending", Command: "pwd", Execute: true, Targets: []BroadcastTarget{{SessionID: "a", Attempt: 1}}}
	writer := []broadcastWriter{{valid: func() bool { return true }, write: func([]byte) bool { close(started); <-release; return true }}}
	out := make(chan BroadcastResult, 1)
	go func() { result, _ := s.beginBroadcast(ctx, page, request, writer); out <- result }()
	<-started
	cancel()
	select {
	case result := <-out:
		if !result.Pending || result.Items[0].State != "unknown" {
			t.Fatal("unknown write mislabeled")
		}
	case <-time.After(time.Second):
		t.Fatal("wait was not bounded by caller")
	}
	different := request
	different.ID = "new"
	if _, e := s.beginBroadcast(context.Background(), page, different, writer); e == nil {
		t.Fatal("queued another command behind unconfirmed write")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		pending := s.broadcasts[request.ID].pending
		s.mu.Unlock()
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker not released")
		}
		time.Sleep(time.Millisecond)
	}
	cached, e := s.beginBroadcast(context.Background(), page, request, writer)
	if e != nil || cached.Pending || cached.Items[0].State != "sent" {
		t.Fatal("cached final result", e)
	}
}
