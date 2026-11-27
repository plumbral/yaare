package sshclient

import (
	"errors"
	"yaare/internal/domain/workspace"
)

func (s *Session) BeginTerminalTransfer() (string, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected || s.stdin == nil || s.ctx.Err() != nil {
		return "", errors.New("SSH 已断开，无法开始终端文件传输。")
	}
	if s.terminalTransfer != "" {
		return "", errors.New("此窗格已有终端文件传输。")
	}
	s.terminalTransfer = workspace.NewID()
	return s.terminalTransfer, nil
}
func (s *Session) OwnsTerminalTransfer(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return id != "" && s.terminalTransfer == id && s.connected && s.ctx.Err() == nil
}
func (s *Session) TerminalTransferActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminalTransfer != ""
}
func (s *Session) WriteTerminalTransfer(id string, data []byte) bool {
	if id == "" {
		return false
	}
	return s.writeTerminal(data, id)
}
func (s *Session) EndTerminalTransfer(id string, abort bool) bool {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if id == "" || s.terminalTransfer != id {
		s.mu.Unlock()
		return false
	}
	input, live := s.stdin, s.connected
	s.terminalTransfer = ""
	s.mu.Unlock()
	if abort && live && input != nil {
		_, _ = input.Write([]byte{24, 24, 24, 24, 24, 24, 24, 24, 8, 8, 8, 8, 8, 8, 8, 8})
	}
	return true
}

func (s *Session) StartZmodemUpload() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	ready := s.connected && s.directory.Ready && s.stdin != nil && s.terminalTransfer == ""
	input := s.stdin
	s.mu.Unlock()
	if !ready || s.ctx.Err() != nil {
		return errors.New("请回到终端空命令行后再启动 rz，或手动运行 rz。")
	}
	s.directoryBusy()
	_, e := input.Write([]byte(" rz -b\r"))
	if e != nil {
		s.cancel()
		return errors.New("rz 启动请求未确认，请检查终端，不要重复发送。")
	}
	return nil
}
