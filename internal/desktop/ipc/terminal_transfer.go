package ipc

import (
	"context"
	"encoding/base64"
	"errors"

	"time"
)

func (s *SSHService) TerminalTransferBegin(ctx context.Context, id string, attempt int) (string, error) {
	if e := workspaceCaller(ctx); e != nil {
		return "", e
	}
	s.mu.Lock()
	run := s.runs[id]
	s.mu.Unlock()
	if run == nil || run.page != callerPage(ctx) || run.attempt != attempt {
		return "", errors.New("SSH 已变化，未开始终端传输。")
	}
	run.logMu.Lock()
	defer run.logMu.Unlock()
	token, e := run.session.BeginTerminalTransfer()
	if e != nil {
		return "", e
	}
	if recording := run.record.Swap(nil); recording != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, e = recording.Stop(stopCtx, "ZMODEM 文件传输开始，录制已停止，结束后可手动重新开启。")
		cancel()
		if e != nil {
			run.session.EndTerminalTransfer(token, true)
			return "", errors.New("会话录制未能确认停止，未开始文件传输。")
		}
	}
	return token, nil
}
func (s *SSHService) TerminalTransferWrite(ctx context.Context, id string, attempt int, token, data string) bool {
	if len(data) > 90000 {
		return false
	}
	run := s.owned(ctx, id, attempt)
	if run == nil {
		return false
	}
	bytes, e := base64.StdEncoding.DecodeString(data)
	if e != nil {
		return false
	}
	defer clear(bytes)
	return run.WriteTerminalTransfer(token, bytes)
}
func (s *SSHService) TerminalTransferEnd(ctx context.Context, id string, attempt int, token string, abort bool) bool {
	run := s.owned(ctx, id, attempt)
	return run != nil && run.EndTerminalTransfer(token, abort)
}
func (s *SSHService) StartZmodemUpload(ctx context.Context, id string, attempt int) error {
	run := s.owned(ctx, id, attempt)
	if run == nil {
		return errors.New("请先连接 SSH。")
	}
	return run.StartZmodemUpload()
}
