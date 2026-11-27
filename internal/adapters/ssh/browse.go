package sshclient

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"

	"github.com/pkg/sftp"
)

const browseIdleTimeout = 60 * time.Second

type browseChannel struct {
	client *sftp.Client
	cancel context.CancelFunc
	close  func()
}

func (channel *browseChannel) dispose() {
	channel.cancel()
	if channel.close != nil {
		channel.close()
	}
}

type idleBrowseChannel struct {
	channel *browseChannel
	taken   chan struct{}
}

type browseResult struct {
	path string
	rows []fs.FileInfo
	err  error
}

func (s *Session) BrowseDirectory(ctx context.Context, path string) (string, []fs.FileInfo, error) {
	return s.browseDirectory(ctx, path, browseIdleTimeout)
}

func (s *Session) browseDirectory(ctx context.Context, path string, idleTimeout time.Duration) (string, []fs.FileInfo, error) {
	if !s.Connected() {
		return "", nil, browseDisconnected()
	}
	select {
	case <-ctx.Done():
		return "", nil, ctx.Err()
	case <-s.ctx.Done():
		return "", nil, browseDisconnected()
	case s.browseSlot <- struct{}{}:
	}
	result := make(chan browseResult, 1)
	go func() {
		reply := s.readBrowseDirectory(ctx, path, idleTimeout)
		<-s.browseSlot
		result <- reply
	}()
	select {
	case <-ctx.Done():
		return "", nil, ctx.Err()
	case <-s.ctx.Done():
		return "", nil, browseDisconnected()
	case reply := <-result:
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		if s.ctx.Err() != nil {
			return "", nil, browseDisconnected()
		}
		return reply.path, reply.rows, reply.err
	}
}

func (s *Session) readBrowseDirectory(ctx context.Context, path string, idleTimeout time.Duration) browseResult {
	for {
		if err := ctx.Err(); err != nil {
			return browseResult{err: err}
		}
		if s.ctx.Err() != nil {
			return browseResult{err: browseDisconnected()}
		}
		channel := s.takeIdleBrowse()
		reused := channel != nil
		var lifetime context.Context
		if !reused {
			var cancel context.CancelFunc
			lifetime, cancel = context.WithCancel(s.ctx)
			channel = &browseChannel{cancel: cancel}
		}
		stop := context.AfterFunc(ctx, channel.cancel)
		var reply browseResult
		if !reused {
			channel.client, channel.close, reply.err = s.openFiles(lifetime, false)
		}
		if reply.err == nil && ctx.Err() == nil && s.ctx.Err() == nil {
			reply.path = path
			if path == "." {
				reply.path, reply.err = channel.client.Getwd()
			}
			if reply.err == nil && ctx.Err() == nil {
				reply.rows, reply.err = channel.client.ReadDirContext(ctx, reply.path)
			}
		}
		if !stop() || ctx.Err() != nil || s.ctx.Err() != nil {
			channel.dispose()
			if err := ctx.Err(); err != nil {
				return browseResult{err: err}
			}
			return browseResult{err: browseDisconnected()}
		}
		if keepBrowseChannel(reply.err) {
			s.keepIdleBrowse(channel, idleTimeout)
			return reply
		}
		channel.dispose()
		if !reused || !browseConnectionLost(reply.err) {
			return reply
		}
	}
}

func browseDisconnected() error {
	return fault("SFTP_DISCONNECTED", "SSH 尚未连接或已断开。")
}

func browseConnectionLost(err error) bool {
	var status *sftp.StatusError
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, sftp.ErrSSHFxConnectionLost) || errors.Is(err, sftp.ErrSSHFxNoConnection) ||
		errors.As(err, &status) && (status.FxCode() == sftp.ErrSSHFxConnectionLost || status.FxCode() == sftp.ErrSSHFxNoConnection)
}

func keepBrowseChannel(err error) bool {
	var status *sftp.StatusError
	return err == nil || !browseConnectionLost(err) &&
		(errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) || errors.As(err, &status))
}

func (s *Session) takeIdleBrowse() *browseChannel {
	idle := s.browseIdle
	if idle == nil {
		return nil
	}
	s.browseIdle = nil
	close(idle.taken)
	return idle.channel
}

func (s *Session) keepIdleBrowse(channel *browseChannel, idleTimeout time.Duration) {
	idle := &idleBrowseChannel{channel: channel, taken: make(chan struct{})}
	s.browseIdle = idle
	timer := time.NewTimer(idleTimeout)
	go func() {
		defer timer.Stop()
		select {
		case <-idle.taken:
			return
		case <-timer.C:
		case <-s.ctx.Done():
		}
		select {
		case <-idle.taken:
		case s.browseSlot <- struct{}{}:
			defer func() { <-s.browseSlot }()
			if s.browseIdle == idle {
				s.takeIdleBrowse().dispose()
			}
		}
	}()
}

func (s *Session) evictIdleBrowse(ctx context.Context) bool {
	select {
	case s.browseSlot <- struct{}{}:
	default:
		return false
	}
	channel := s.takeIdleBrowse()
	if channel == nil {
		<-s.browseSlot
		return false
	}
	done := make(chan struct{})
	go func() {
		channel.dispose()
		<-s.browseSlot
		close(done)
	}()
	select {
	case <-ctx.Done():
		return false
	case <-s.ctx.Done():
		return false
	case <-done:
		return ctx.Err() == nil && s.ctx.Err() == nil
	}
}
