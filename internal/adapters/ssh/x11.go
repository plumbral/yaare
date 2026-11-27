package sshclient

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const x11Auth = "MIT-MAGIC-COOKIE-1"

type X11Options struct {
	Address   string
	Cookie    string
	Command   string
	Notify    func(string)
	localDial func(context.Context, string) (net.Conn, error)
}
type x11Bridge struct {
	mu     sync.Mutex
	remote ssh.Channel
	local  net.Conn
	closed bool
}

func (b *x11Bridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.remote.Close()
	if b.local != nil {
		b.local.Close()
	}
}
func (b *x11Bridge) setLocal(conn net.Conn) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		conn.Close()
		return false
	}
	b.local = conn
	return true
}

type X11Run struct {
	owner                  *Session
	ctx                    context.Context
	cancel                 context.CancelFunc
	command                *ssh.Session
	address                string
	realCookie, fakeCookie []byte
	dial                   func(context.Context, string) (net.Conn, error)
	notify                 func(string)
	mu                     sync.Mutex
	bridges                map[*x11Bridge]bool
	finished, closed       bool
	exit                   error
	wg                     sync.WaitGroup
	done                   chan struct{}
	doneOnce, closeOnce    sync.Once
}

func (s *Session) StartX11(ctx context.Context, options X11Options) (*X11Run, error) {
	host, portText, e := net.SplitHostPort(options.Address)
	port, portError := strconv.Atoi(portText)
	if e != nil || portError != nil || port < 1 || port > 65535 || host == "" || len(host) > 253 || strings.ContainsAny(host, "\r\n\x00 /\\") || strings.TrimSpace(options.Command) == "" || len(options.Command) > 4096 || strings.ContainsRune(options.Command, 0) {
		return nil, errors.New("X11 地址或远程图形命令无效。")
	}
	if options.Cookie != "" && len(options.Cookie) != 32 {
		return nil, errors.New("本机 X11 Cookie 应为 32 个十六进制字符，或按本机 X 服务器配置留空。")
	}
	realCookie, e := hex.DecodeString(options.Cookie)
	options.Cookie = ""
	if e != nil {
		return nil, errors.New("本机 X11 Cookie 格式无效。")
	}
	s.mu.Lock()
	client, live := s.client, s.connected
	s.mu.Unlock()
	if !live || client == nil || s.ctx.Err() != nil {
		clear(realCookie)
		return nil, errors.New("SSH 已断开，无法启动 X11。")
	}
	command, e := s.openSessionChannel(ctx, client, true)
	if e != nil {
		clear(realCookie)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("服务器无法创建 X11 会话。")
	}
	life, cancel := context.WithCancel(ctx)
	x := &X11Run{owner: s, ctx: life, cancel: cancel, command: command, address: options.Address, realCookie: realCookie, fakeCookie: make([]byte, 16), dial: options.localDial, notify: options.Notify, bridges: map[*x11Bridge]bool{}, done: make(chan struct{})}
	if x.dial == nil {
		x.dial = func(ctx context.Context, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", address)
		}
	}
	_, _ = rand.Read(x.fakeCookie)
	s.x11Mu.Lock()
	if s.x11Active != nil {
		s.x11Mu.Unlock()
		command.Close()
		cancel()
		clear(realCookie)
		clear(x.fakeCookie)
		return nil, errors.New("此 SSH 会话已有 X11 转发，请先停止。")
	}
	s.x11Once.Do(func() {
		s.x11Route = client.HandleChannelOpen("x11")
		if s.x11Route != nil {
			go s.routeX11()
		}
	})
	if s.x11Route == nil {
		s.x11Mu.Unlock()
		command.Close()
		cancel()
		clear(realCookie)
		clear(x.fakeCookie)
		return nil, errors.New("此连接的 X11 通道已被占用。")
	}
	s.x11Active = x
	s.x11Mu.Unlock()
	startCtx, stop := context.WithTimeout(life, 12*time.Second)
	watch := context.AfterFunc(startCtx, func() { _ = command.Close() })
	request := struct {
		Single   bool
		Protocol string
		Cookie   string
		Screen   uint32
	}{false, x11Auth, hex.EncodeToString(x.fakeCookie), 0}
	ok, e := command.SendRequest("x11-req", true, ssh.Marshal(request))
	request.Cookie = ""
	if e != nil || !ok {
		watch()
		stop()
		x.Close()
		return nil, errors.New("服务器未允许 X11 转发，请检查 sshd 的 X11Forwarding 和 xauth。")
	}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	e = command.Start(options.Command)
	watch()
	stop()
	if e != nil {
		x.Close()
		return nil, errors.New("远程图形命令未能启动，请检查命令和服务器权限。")
	}
	go func() {
		e := command.Wait()
		x.mu.Lock()
		x.finished = true
		x.exit = e
		if len(x.bridges) == 0 {
			x.doneOnce.Do(func() { close(x.done) })
		}
		x.mu.Unlock()
	}()
	go func() {
		select {
		case <-life.Done():
		case <-s.ctx.Done():
		case <-x.done:
		}
		x.Close()
	}()
	return x, nil
}
func (s *Session) routeX11() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case incoming, ok := <-s.x11Route:
			if !ok {
				return
			}
			s.x11Mu.Lock()
			active := s.x11Active
			s.x11Mu.Unlock()
			if active == nil {
				_ = incoming.Reject(ssh.Prohibited, "X11 forwarding is not active")
				continue
			}
			active.mu.Lock()
			accept := !active.closed && !active.finished && len(active.bridges) < 16
			active.mu.Unlock()
			if !accept {
				_ = incoming.Reject(ssh.ResourceShortage, "X11 forwarding is unavailable")
				continue
			}
			channel, requests, e := incoming.Accept()
			if e != nil {
				continue
			}
			go ssh.DiscardRequests(requests)
			bridge := &x11Bridge{remote: channel}
			active.mu.Lock()
			if active.closed || active.finished {
				active.mu.Unlock()
				bridge.close()
				continue
			}
			active.bridges[bridge] = true
			active.wg.Add(1)
			active.mu.Unlock()
			go active.forward(bridge)
		}
	}
}

func x11Setup(input io.Reader, fakeCookie, realCookie []byte) ([]byte, error) {
	header := make([]byte, 12)
	if _, e := io.ReadFull(input, header); e != nil {
		return nil, e
	}
	var order binary.ByteOrder
	switch header[0] {
	case 'l':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return nil, errors.New("invalid X11 byte order")
	}
	nameLen, dataLen := int(order.Uint16(header[6:8])), int(order.Uint16(header[8:10]))
	if order.Uint16(header[2:4]) != 11 || nameLen != len(x11Auth) || dataLen != 16 || len(fakeCookie) != 16 || len(realCookie) != 0 && len(realCookie) != 16 {
		return nil, errors.New("invalid X11 authentication")
	}
	pad := func(n int) int { return (n + 3) &^ 3 }
	body := make([]byte, pad(nameLen)+pad(dataLen))
	defer clear(body)
	if _, e := io.ReadFull(input, body); e != nil {
		return nil, e
	}
	if string(body[:nameLen]) != x11Auth || subtle.ConstantTimeCompare(body[pad(nameLen):pad(nameLen)+dataLen], fakeCookie) != 1 {
		return nil, errors.New("unrecognized X11 authentication")
	}
	if len(realCookie) == 0 {
		order.PutUint16(header[6:8], 0)
		order.PutUint16(header[8:10], 0)
		return header, nil
	}
	copy(body[pad(nameLen):], realCookie)
	return append(header, body...), nil
}
func (x *X11Run) report(kind string) {
	if x.notify != nil {
		x.notify(kind)
	}
}
func (x *X11Run) forward(bridge *x11Bridge) {
	defer x.wg.Done()
	defer func() {
		bridge.close()
		x.mu.Lock()
		delete(x.bridges, bridge)
		if x.finished && len(x.bridges) == 0 {
			x.doneOnce.Do(func() { close(x.done) })
		}
		x.mu.Unlock()
	}()
	handshake, stop := context.WithTimeout(x.ctx, 8*time.Second)
	defer stop()
	watch := context.AfterFunc(handshake, bridge.close)
	defer watch()
	setup, e := x11Setup(bridge.remote, x.fakeCookie, x.realCookie)
	if e != nil {
		return
	}
	defer clear(setup)
	local, e := x.dial(handshake, x.address)
	if e != nil {
		x.report("local-unavailable")
		return
	}
	if !bridge.setLocal(local) {
		return
	}
	if _, e = local.Write(setup); e != nil {
		x.report("local-unavailable")
		return
	}
	reply := make([]byte, 8)
	if _, e = io.ReadFull(local, reply); e != nil {
		x.report("local-unavailable")
		return
	}
	if _, e = bridge.remote.Write(reply); e != nil {
		return
	}
	if reply[0] != 1 {
		x.report("authentication-failed")
		var order binary.ByteOrder = binary.LittleEndian
		if setup[0] == 'B' {
			order = binary.BigEndian
		}
		_, _ = io.CopyN(bridge.remote, local, int64(order.Uint16(reply[6:8]))*4)
		return
	}
	watch()
	stop()
	x.report("attached")
	done := make(chan struct{})
	go func() { _, _ = io.Copy(local, bridge.remote); local.Close(); close(done) }()
	_, _ = io.Copy(bridge.remote, local)
	bridge.remote.Close()
	<-done
}
func (x *X11Run) Done() <-chan struct{} { return x.done }
func (x *X11Run) Err() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.exit != nil {
		return errors.New("远程图形命令已退出，请检查命令、本机 X 服务器和认证设置。")
	}
	return nil
}
func (x *X11Run) Close() {
	x.closeOnce.Do(func() {
		x.owner.x11Mu.Lock()
		if x.owner.x11Active == x {
			x.owner.x11Active = nil
		}
		x.owner.x11Mu.Unlock()
		x.mu.Lock()
		x.closed = true
		bridges := make([]*x11Bridge, 0, len(x.bridges))
		for bridge := range x.bridges {
			bridges = append(bridges, bridge)
		}
		x.mu.Unlock()
		x.cancel()
		x.command.Close()
		for _, bridge := range bridges {
			bridge.close()
		}
		x.wg.Wait()
		clear(x.realCookie)
		clear(x.fakeCookie)
		x.doneOnce.Do(func() { close(x.done) })
	})
}
