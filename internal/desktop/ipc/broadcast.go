package ipc

import (
	"context"
	"errors"

	"time"
	sshclient "yaare/internal/adapters/ssh"
)

type BroadcastTarget struct {
	SessionID string `json:"sessionId"`
	Attempt   int    `json:"attempt"`
}
type BroadcastRequest struct {
	ID      string            `json:"id"`
	Targets []BroadcastTarget `json:"targets"`
	Command string            `json:"command"`
	Execute bool              `json:"execute"`
}
type BroadcastItem struct {
	SessionID string `json:"sessionId"`
	Attempt   int    `json:"attempt"`
	State     string `json:"state"`
	Message   string `json:"message"`
}
type BroadcastResult struct {
	ID      string          `json:"id"`
	Items   []BroadcastItem `json:"items"`
	Pending bool            `json:"pending"`
}
type broadcastRecord struct {
	page    *callerScope
	at      time.Time
	result  BroadcastResult
	pending int
}
type broadcastWriter struct {
	valid func() bool
	write func([]byte) bool
}

func (s *SSHService) Broadcast(ctx context.Context, request BroadcastRequest) (BroadcastResult, error) {
	if err := workspaceCaller(ctx); err != nil {
		return BroadcastResult{}, err
	}
	writers := make([]broadcastWriter, len(request.Targets))
	for i, target := range request.Targets {
		run := s.owned(ctx, target.SessionID, target.Attempt)
		if run == nil {
			continue
		}
		id, attempt := target.SessionID, target.Attempt
		writers[i] = broadcastWriter{valid: func() bool { return s.owned(ctx, id, attempt) == run && run.Connected() }, write: run.Write}
	}
	return s.beginBroadcast(ctx, callerPage(ctx), request, writers)
}
func snapshotBroadcast(record *broadcastRecord) BroadcastResult {
	out := record.result
	out.Items = append([]BroadcastItem(nil), out.Items...)
	out.Pending = record.pending > 0
	for i := range out.Items {
		if out.Items[i].State == "pending" {
			out.Items[i].State = "unknown"
			out.Items[i].Message = "写入结果尚未确认，请检查终端；不要重复发送。"
		}
	}
	return out
}
func (s *SSHService) beginBroadcast(ctx context.Context, page *callerScope, request BroadcastRequest, writers []broadcastWriter) (BroadcastResult, error) {
	if page == nil || !sessionIDPattern.MatchString(request.ID) || len(request.Targets) < 1 || len(request.Targets) > 8 || len(writers) != len(request.Targets) {
		return BroadcastResult{}, errors.New("请选择当前标签内 1–8 个窗格。")
	}
	seen := map[string]bool{}
	for _, target := range request.Targets {
		if !sessionIDPattern.MatchString(target.SessionID) || target.Attempt < 1 || seen[target.SessionID] {
			return BroadcastResult{}, errors.New("广播目标无效或重复。")
		}
		seen[target.SessionID] = true
	}
	payload, err := sshclient.BroadcastInput(request.Command, request.Execute)
	request.Command = ""
	if err != nil {
		return BroadcastResult{}, err
	}
	if err = ctx.Err(); err != nil {
		clear(payload)
		return BroadcastResult{}, err
	}
	s.mu.Lock()
	if s.broadcasts == nil {
		s.broadcasts = map[string]*broadcastRecord{}
		s.broadcastSlots = make(chan struct{}, 8)
	}
	for id, r := range s.broadcasts {
		if r.pending == 0 && time.Since(r.at) > 10*time.Minute {
			delete(s.broadcasts, id)
		}
	}
	if prior := s.broadcasts[request.ID]; prior != nil {
		clear(payload)
		if prior.page != page {
			s.mu.Unlock()
			return BroadcastResult{}, errors.New("广播记录不可用。")
		}
		result := snapshotBroadcast(prior)
		s.mu.Unlock()
		return result, nil
	}
	for _, r := range s.broadcasts {
		if r.page == page && r.pending > 0 {
			clear(payload)
			s.mu.Unlock()
			return BroadcastResult{}, errors.New("上一次发送仍未确认，请先检查终端并刷新发送状态。")
		}
	}
	if len(s.broadcasts) >= 128 {
		clear(payload)
		s.mu.Unlock()
		return BroadcastResult{}, errors.New("短时间内广播次数过多，请稍后再试。")
	}
	record := &broadcastRecord{page: page, at: time.Now(), result: BroadcastResult{ID: request.ID, Items: make([]BroadcastItem, len(request.Targets))}}
	acquired := 0
	for _, writer := range writers {
		if writer.write == nil {
			continue
		}
		select {
		case s.broadcastSlots <- struct{}{}:
			acquired++
		default:
			for range acquired {
				<-s.broadcastSlots
			}
			clear(payload)
			s.mu.Unlock()
			return BroadcastResult{}, errors.New("正在发送的目标较多，请稍后再试。")
		}
	}
	for i, target := range request.Targets {
		item := BroadcastItem{SessionID: target.SessionID, Attempt: target.Attempt, State: "not-sent", Message: "连接已变化或断开，未发送。"}
		if writers[i].write != nil {
			item.State = "pending"
			item.Message = ""
			record.pending++
		}
		record.result.Items[i] = item
	}
	s.broadcasts[request.ID] = record
	s.mu.Unlock()
	done := make(chan struct{}, 8)
	if acquired == 0 {
		clear(payload)
	}
	for i, writer := range writers {
		if writer.write == nil {
			continue
		}
		go func(index int, w broadcastWriter) {
			state, message := "not-sent", "连接已变化或断开，未发送。"
			defer func() {
				if recover() != nil {
					state, message = "unknown", "写入结果未确认，请检查终端，不要重复发送。"
				}
				s.mu.Lock()
				record.result.Items[index].State = state
				record.result.Items[index].Message = message
				record.pending--
				if record.pending == 0 {
					clear(payload)
				}
				s.mu.Unlock()
				<-s.broadcastSlots
				done <- struct{}{}
			}()
			if w.valid != nil && w.valid() {
				state, message = "unknown", "写入失败，可能部分发送；请检查终端，不要重复发送。"
				if w.write(payload) {
					state, message = "sent", "已发送到此终端；不代表命令执行成功。"
				}
			}
		}(i, writer)
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		s.mu.Lock()
		result := snapshotBroadcast(record)
		s.mu.Unlock()
		if !result.Pending {
			return result, nil
		}
		select {
		case <-done:
		case <-ctx.Done():
			s.mu.Lock()
			result = snapshotBroadcast(record)
			s.mu.Unlock()
			return result, nil
		case <-timer.C:
			s.mu.Lock()
			result = snapshotBroadcast(record)
			s.mu.Unlock()
			return result, nil
		}
	}
}
func (s *SSHService) BroadcastStatus(ctx context.Context, id string) (BroadcastResult, error) {
	if err := workspaceCaller(ctx); err != nil {
		return BroadcastResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.broadcasts[id]
	if r == nil || r.page != callerPage(ctx) {
		return BroadcastResult{}, errors.New("发送记录不可用，请检查各终端。")
	}
	return snapshotBroadcast(r), nil
}
