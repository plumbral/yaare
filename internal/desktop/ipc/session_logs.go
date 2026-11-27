package ipc

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"os"
	"path/filepath"
	"time"
	"yaare/internal/adapters/sessionlog"
)

func ConfigureSessionLogs(s *SSHService, logs *sessionlog.Store) { s.logs = logs }
func (s *SSHService) LogList(ctx context.Context) ([]sessionlog.Info, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	if s.logs == nil {
		return nil, errors.New("日志服务不可用。")
	}
	return s.logs.List()
}
func (s *SSHService) LogStart(ctx context.Context, id string, attempt int) (sessionlog.Info, error) {
	if err := workspaceCaller(ctx); err != nil {
		return sessionlog.Info{}, err
	}
	s.mu.Lock()
	run := s.runs[id]
	s.mu.Unlock()
	if run == nil || run.page != callerPage(ctx) || run.attempt != attempt || s.logs == nil {
		return sessionlog.Info{}, errors.New("当前连接不可用，请重新连接后记录。")
	}
	run.logMu.Lock()
	defer run.logMu.Unlock()
	if !run.session.Connected() {
		return sessionlog.Info{}, errors.New("请等待 SSH 连接成功后再记录。")
	}
	if run.session.TerminalTransferActive() {
		return sessionlog.Info{}, errors.New("终端文件传输期间不能录制会话，结束后可重新开启。")
	}
	if r := run.record.Load(); r != nil {
		if r.Info().State == "recording" {
			return r.Info(), nil
		}
		run.record.Store(nil)
	}
	recorder, err := s.logs.Start(id, attempt, run.name, func(info sessionlog.Info) { run.session.NotifyLog(info) })
	if err != nil {
		return sessionlog.Info{}, err
	}
	run.record.Store(recorder)
	info := recorder.Info()
	go run.session.NotifyLog(info)
	return info, nil
}
func (s *SSHService) LogStop(ctx context.Context, id string, attempt int) (sessionlog.Info, error) {
	if err := workspaceCaller(ctx); err != nil {
		return sessionlog.Info{}, err
	}
	s.mu.Lock()
	run := s.runs[id]
	s.mu.Unlock()
	if run == nil || run.page != callerPage(ctx) || run.attempt != attempt {
		return sessionlog.Info{}, errors.New("该连接已结束，请刷新日志列表。")
	}
	run.logMu.Lock()
	defer run.logMu.Unlock()
	r := run.record.Swap(nil)
	if r == nil {
		return sessionlog.Info{}, errors.New("当前窗格未开启记录。")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return r.Stop(ctx, "")
}
func (s *SSHService) LogRead(ctx context.Context, q sessionlog.Query) (sessionlog.Page, error) {
	if err := workspaceCaller(ctx); err != nil {
		return sessionlog.Page{}, err
	}
	if s.logs == nil {
		return sessionlog.Page{}, errors.New("日志服务不可用。")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.logs.Read(ctx, q)
}
func (s *SSHService) LogDelete(ctx context.Context, id string) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	if s.logs == nil {
		return errors.New("日志服务不可用。")
	}
	return s.logs.Delete(id)
}
func (s *SSHService) LogExport(ctx context.Context, id string) (bool, error) {
	if err := workspaceCaller(ctx); err != nil {
		return false, err
	}
	if s.logs == nil {
		return false, errors.New("日志服务不可用。")
	}
	dest, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: mygo.CallerWindow(ctx), Title: uiText(callerPage(ctx), "导出会话日志"), DefaultPath: "Yaare-session.txt"})
	if err != nil {
		return false, errors.New("无法选择日志导出位置。")
	}
	if dest == "" {
		return false, nil
	}
	file, err := os.CreateTemp(filepath.Dir(dest), ".yaare-log-*")
	if err != nil {
		return false, errors.New("无法写入导出目录。")
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	err = s.logs.Export(ctx, id, file)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return false, errors.New("日志导出未完成，原文件未更改。")
	}
	if err = os.Rename(tmp, dest); err != nil {
		return false, errors.New("无法完成日志导出，原文件未更改。")
	}
	return true, nil
}
