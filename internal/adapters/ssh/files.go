package sshclient

import (
	"context"
	"errors"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func (s *Session) FileIdentity() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected || s.client == nil || s.ctx.Err() != nil || s.fileIdentity == "" {
		return "", fault("SFTP_DISCONNECTED", "请重新连接原 SSH 会话后继续任务。")
	}
	return s.fileIdentity, nil
}

func (s *Session) OpenFiles(ctx context.Context) (*sftp.Client, func(), error) {
	return s.openFiles(ctx, true)
}

func (s *Session) openSessionChannel(ctx context.Context, client *ssh.Client, allowIdleEviction bool) (*ssh.Session, error) {
	channel, err := client.NewSession()
	var rejection *ssh.OpenChannelError
	if allowIdleEviction && errors.As(err, &rejection) &&
		(rejection.Reason == ssh.ResourceShortage || rejection.Reason == ssh.ConnectionFailed) && s.evictIdleBrowse(ctx) {
		channel, err = client.NewSession()
	}
	if cancellation := ctx.Err(); cancellation != nil {
		if channel != nil {
			_ = channel.Close()
		}
		return nil, cancellation
	}
	return channel, err
}

func (s *Session) openFiles(ctx context.Context, allowIdleEviction bool) (*sftp.Client, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	client, live := s.client, s.connected
	s.mu.Unlock()
	if !live || client == nil || s.ctx.Err() != nil {
		return nil, nil, fault("SFTP_DISCONNECTED", "SSH 尚未连接或已断开。")
	}
	subsystem, err := s.openSessionChannel(ctx, client, allowIdleEviction)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, err
		}
		return nil, nil, fault("SFTP_UNAVAILABLE", "无法创建 SFTP 通道。")
	}
	var once sync.Once
	done := make(chan struct{})
	closeSubsystem := func() { once.Do(func() { close(done); _ = subsystem.Close() }) }
	go func() {
		select {
		case <-ctx.Done():
			closeSubsystem()
		case <-s.ctx.Done():
			closeSubsystem()
		case <-done:
		}
	}()
	input, err := subsystem.StdinPipe()
	if err != nil {
		closeSubsystem()
		return nil, nil, err
	}
	output, err := subsystem.StdoutPipe()
	if err != nil {
		closeSubsystem()
		return nil, nil, err
	}
	if err = subsystem.RequestSubsystem("sftp"); err != nil {
		closeSubsystem()
		return nil, nil, fault("SFTP_UNAVAILABLE", "服务器未提供 SFTP 子系统。")
	}
	files, err := sftp.NewClientPipe(output, input, sftp.UseConcurrentReads(false), sftp.UseConcurrentWrites(false))
	if err != nil {
		closeSubsystem()
		return nil, nil, fault("SFTP_UNAVAILABLE", "SFTP 协议初始化失败。")
	}
	return files, func() { closeSubsystem(); _ = files.Close() }, nil
}
