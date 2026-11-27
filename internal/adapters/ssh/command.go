package sshclient

import (
	"context"
	"io"
	"net"
	filefs "yaare/internal/adapters/files"
	"strings"
	"sync"
)

func (s *Session) OpenTCP(ctx context.Context, address string) (net.Conn, error) {
	if _, _, e := net.SplitHostPort(address); e != nil {
		return nil, fault("TARGET_INVALID", "目标地址或端口无效。")
	}
	s.mu.Lock()
	client, live := s.client, s.connected
	s.mu.Unlock()
	if !live || client == nil || s.ctx.Err() != nil {
		return nil, fault("TARGET_DISCONNECTED", "SSH 已断开，请重新连接。")
	}
	connection, e := client.DialContext(ctx, "tcp", address)
	if e != nil {
		return nil, fault("TARGET_UNAVAILABLE", "服务器无法访问目标地址，请检查地址、端口及 SSH 转发权限。")
	}
	return connection, nil
}
func (s *Session) Done() <-chan struct{} { return s.ctx.Done() }

func (s *Session) OpenCommand(ctx context.Context, command string) (filefs.RemoteCommand, error) {
	if err := ctx.Err(); err != nil {
		return filefs.RemoteCommand{}, err
	}
	if len(command) == 0 || len(command) > 65536 || strings.ContainsRune(command, 0) {
		return filefs.RemoteCommand{}, fault("COMMAND_INVALID", "远程操作请求无效。")
	}
	s.mu.Lock()
	client, live := s.client, s.connected
	s.mu.Unlock()
	if !live || client == nil || s.ctx.Err() != nil {
		return filefs.RemoteCommand{}, fault("COMMAND_DISCONNECTED", "SSH 已断开，无法执行远程操作。")
	}
	channel, e := s.openSessionChannel(ctx, client, true)
	if e != nil {
		if ctx.Err() != nil {
			return filefs.RemoteCommand{}, ctx.Err()
		}
		return filefs.RemoteCommand{}, fault("COMMAND_UNAVAILABLE", "服务器无法创建远程操作通道。")
	}
	var once sync.Once
	var output *io.PipeReader
	var outputWriter *io.PipeWriter
	done := make(chan struct{})
	closeChannel := func() {
		once.Do(func() {
			close(done)
			if output != nil {
				_ = output.Close()
			}
			if outputWriter != nil {
				_ = outputWriter.Close()
			}
			_ = channel.Close()
		})
	}
	input, e := channel.StdinPipe()
	if e != nil {
		closeChannel()
		return filefs.RemoteCommand{}, e
	}
	output, outputWriter = io.Pipe()
	channel.Stdout = outputWriter
	channel.Stderr = io.Discard
	go func() {
		select {
		case <-ctx.Done():
			closeChannel()
		case <-s.ctx.Done():
			closeChannel()
		case <-done:
		}
	}()
	if e = channel.Start(command); e != nil {
		closeChannel()
		return filefs.RemoteCommand{}, fault("COMMAND_UNAVAILABLE", "服务器拒绝远程操作，未继续。")
	}
	waited := make(chan struct{})
	var waitErr error
	go func() { waitErr = channel.Wait(); _ = outputWriter.Close(); close(waited) }()
	wait := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-waited:
			return waitErr
		case <-done:
			return io.ErrClosedPipe
		}
	}
	return filefs.RemoteCommand{Input: input, Output: output, Wait: wait, Close: closeChannel}, nil
}
