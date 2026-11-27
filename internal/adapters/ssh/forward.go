package sshclient

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"yaare/internal/domain/workspace"
)

type ForwardState struct {
	Rule     workspace.ForwardRule `json:"rule"`
	State    string                `json:"state"`
	Code     string                `json:"code"`
	Message  string                `json:"message"`
	Revision int                   `json:"revision"`
}
type forwardRun struct {
	rule     workspace.ForwardRule
	ctx      context.Context
	cancel   context.CancelFunc
	listener net.Listener
	conns    map[net.Conn]bool
	state    ForwardState
}

func (s *Session) initializeForwards(rules []workspace.ForwardRule) {
	s.forwardMu.Lock()
	s.forwards = map[string]*forwardRun{}
	s.forwardSlots = make(chan struct{}, 64)
	s.forwardGates = map[string]chan struct{}{}
	for _, rule := range rules {
		s.forwardGates[rule.ID] = make(chan struct{}, 1)
		s.forwards[rule.ID] = &forwardRun{rule: rule, state: ForwardState{Rule: rule, State: "stopped", Message: "已停止"}}
	}
	s.forwardMu.Unlock()
	for _, rule := range rules {
		if rule.Enabled {
			go s.SetForward(rule.ID, true)
		} else {
			s.send(Event{Type: "forward", Forward: &ForwardState{Rule: rule, State: "stopped", Message: "已停止"}})
		}
	}
}

func (s *Session) forwardState(r *forwardRun, state, code, message string) {
	s.forwardMu.Lock()
	if s.forwards[r.rule.ID] != r {
		s.forwardMu.Unlock()
		return
	}
	next := ForwardState{Rule: r.rule, State: state, Code: code, Message: message, Revision: r.state.Revision}
	if r.state == next {
		s.forwardMu.Unlock()
		return
	}
	s.forwardRevision++
	next.Revision = s.forwardRevision
	r.state = next
	s.forwardMu.Unlock()
	_ = s.send(Event{Type: "forward", Forward: &next})
}

func (s *Session) SetForward(id string, start bool) bool {
	s.mu.Lock()
	client, connected := s.client, s.connected
	s.mu.Unlock()
	if !connected || client == nil || s.ctx.Err() != nil {
		return false
	}
	s.forwardMu.Lock()
	old := s.forwards[id]
	if old == nil {
		s.forwardMu.Unlock()
		return false
	}
	if start && (old.state.State == "starting" || old.state.State == "running") {
		s.forwardMu.Unlock()
		return true
	}
	if old.cancel != nil {
		old.cancel()
	}
	if old.listener != nil && old.rule.Kind != "remote" {
		old.listener.Close()
	}
	for conn := range old.conns {
		conn.Close()
	}
	if !start {
		r := &forwardRun{rule: old.rule}
		s.forwards[id] = r
		s.forwardMu.Unlock()
		s.forwardState(r, "stopped", "", "已停止")
		return true
	}
	ctx, cancel := context.WithCancel(s.ctx)
	r := &forwardRun{rule: old.rule, ctx: ctx, cancel: cancel, conns: map[net.Conn]bool{}, state: ForwardState{State: "starting"}}
	gate := s.forwardGates[id]
	s.forwards[id] = r
	s.forwardMu.Unlock()
	s.forwardState(r, "starting", "", "正在启动")
	var listener net.Listener
	var err error
	if r.rule.Kind == "remote" {
		select {
		case gate <- struct{}{}:
		default:
			cancel()
			s.forwardState(r, "failed", "FORWARD_BUSY", "上一条服务器监听请求仍在结束，请稍后重试。")
			return true
		}
		ready := make(chan remoteForwardResult)
		go ownRemoteForward(ctx, gate, ready, func() (net.Listener, error) {
			return client.Listen("tcp", workspace.ForwardAddress(r.rule))
		})
		timer := time.NewTimer(15 * time.Second)
		select {
		case got := <-ready:
			listener, err = got.listener, got.err
		case <-ctx.Done():
			err = ctx.Err()
		case <-timer.C:
			err = context.DeadlineExceeded
		}
		timer.Stop()
	} else {
		listener, err = (&net.ListenConfig{}).Listen(ctx, "tcp", workspace.ForwardAddress(r.rule))
	}
	if err != nil {
		cancel()
		s.forwardState(r, "failed", "FORWARD_LISTEN_FAILED", "监听失败：端口被占用、服务器拒绝，或请求超时。")
		return true
	}
	s.forwardMu.Lock()
	if s.forwards[id] != r || ctx.Err() != nil {
		s.forwardMu.Unlock()
		cancel()
		if r.rule.Kind != "remote" {
			listener.Close()
		}
		return true
	}
	r.listener = listener
	s.forwardMu.Unlock()
	s.forwardState(r, "running", "", "运行中")
	go s.acceptForward(client, r, listener)
	return true
}

type remoteForwardResult struct {
	listener net.Listener
	err      error
}

func ownRemoteForward(ctx context.Context, gate chan struct{}, ready chan<- remoteForwardResult, listen func() (net.Listener, error)) {
	defer func() { <-gate }()
	listener, err := listen()
	if listener != nil {
		defer listener.Close()
	}
	select {
	case ready <- remoteForwardResult{listener, err}:
		if err == nil {
			<-ctx.Done()
		}
	case <-ctx.Done():
	}
}

func (s *Session) acceptForward(client *ssh.Client, r *forwardRun, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if r.ctx.Err() == nil {
				r.cancel()
				s.forwardState(r, "failed", "FORWARD_LISTENER_CLOSED", "监听已中断，请重试。")
			}
			return
		}
		select {
		case s.forwardSlots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		s.forwardMu.Lock()
		if r.ctx.Err() != nil {
			s.forwardMu.Unlock()
			conn.Close()
			<-s.forwardSlots
			return
		}
		r.conns[conn] = true
		s.forwardMu.Unlock()
		go func() {
			defer func() {
				conn.Close()
				s.forwardMu.Lock()
				delete(r.conns, conn)
				s.forwardMu.Unlock()
				<-s.forwardSlots
			}()
			s.serveForward(client, r, conn)
		}()
	}
}

func (s *Session) serveForward(client *ssh.Client, r *forwardRun, in net.Conn) {
	target := net.JoinHostPort(strings.Trim(r.rule.TargetAddress, "[]"), strconv.Itoa(r.rule.TargetPort))
	if r.rule.Kind == "dynamic" {
		var ok bool
		target, ok = socksRequest(in)
		if !ok {
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
	defer cancel()
	var out net.Conn
	var err error
	if r.rule.Kind == "remote" {
		out, err = (&net.Dialer{}).DialContext(ctx, "tcp", target)
	} else {
		out, err = client.DialContext(ctx, "tcp", target)
	}
	if err != nil {
		if r.rule.Kind == "dynamic" {
			_, _ = in.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		}
		if r.ctx.Err() == nil {
			s.forwardState(r, "running", "FORWARD_TARGET_FAILED", "监听中；最近一次目标连接失败。")
		}
		return
	}
	defer out.Close()
	s.forwardMu.Lock()
	if r.ctx.Err() != nil {
		s.forwardMu.Unlock()
		return
	}
	r.conns[out] = true
	s.forwardMu.Unlock()
	defer func() { s.forwardMu.Lock(); delete(r.conns, out); s.forwardMu.Unlock() }()
	if r.rule.Kind == "dynamic" {
		if _, err = in.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
			return
		}
	}
	_ = in.SetDeadline(time.Time{})
	var wg sync.WaitGroup
	wg.Add(2)
	copyHalf := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if closer, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		} else {
			dst.Close()
		}
	}
	go copyHalf(out, in)
	go copyHalf(in, out)
	wg.Wait()
}

func socksRequest(conn net.Conn) (string, bool) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:2]); err != nil || head[0] != 5 || head[1] == 0 {
		return "", false
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", false
	}
	noAuth := false
	for _, method := range methods {
		if method == 0 {
			noAuth = true
		}
	}
	if !noAuth {
		_, _ = conn.Write([]byte{5, 255})
		return "", false
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return "", false
	}
	if _, err := io.ReadFull(conn, head[:]); err != nil || head[0] != 5 || head[2] != 0 {
		return "", false
	}
	if head[1] != 1 {
		_, _ = conn.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return "", false
	}
	var host string
	switch head[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", false
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", false
		}
		host = net.IP(b).String()
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil || length[0] == 0 {
			return "", false
		}
		b := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", false
		}
		host = string(b)
	default:
		_, _ = conn.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
		return "", false
	}
	var port [2]byte
	if _, err := io.ReadFull(conn, port[:]); err != nil {
		return "", false
	}
	p := binary.BigEndian.Uint16(port[:])
	if p == 0 {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(int(p))), true
}

func (s *Session) closeForwards() {
	s.forwardMu.Lock()
	defer s.forwardMu.Unlock()
	for _, r := range s.forwards {
		if r.cancel != nil {
			r.cancel()
		}
		if r.listener != nil && r.rule.Kind != "remote" {
			r.listener.Close()
		}
		for conn := range r.conns {
			conn.Close()
		}
	}
	s.forwards = nil
}
