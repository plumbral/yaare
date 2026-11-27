package ipc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/egoist/mygo"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	display "yaare/internal/adapters/desktop"
	sshclient "yaare/internal/adapters/ssh"
)

type DesktopTarget struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}
type X11Draft struct {
	Local     DesktopTarget `json:"local"`
	Cookie    string        `json:"cookie"`
	Command   string        `json:"command"`
	Confirmed bool          `json:"confirmed"`
}
type DesktopPacket struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data string `json:"data"`
}
type desktopStream struct {
	page    *callerScope
	session string
	attempt int
	run     *sshclient.Session
	conn    net.Conn
	write   chan struct{}
	cancel  context.CancelFunc
}
type DesktopService struct {
	ssh     *SSHService
	mu      sync.Mutex
	streams map[string]*desktopStream
	slots   chan struct{}
}

func NewDesktopService(ssh *SSHService) *DesktopService {
	return &DesktopService{ssh: ssh, streams: map[string]*desktopStream{}, slots: make(chan struct{}, 8)}
}
func validateDesktopTarget(target DesktopTarget) error {
	if target.Host == "" || len(target.Host) > 253 || strings.ContainsAny(target.Host, "\x00\r\n /\\") || target.Port < 1 || target.Port > 65535 {
		return errors.New("远程桌面的服务器侧地址或端口无效。")
	}
	return nil
}
func (s *DesktopService) session(ctx context.Context, id string, attempt int, target DesktopTarget) (*sshclient.Session, error) {
	if e := workspaceCaller(ctx); e != nil {
		return nil, e
	}
	if e := validateDesktopTarget(target); e != nil {
		return nil, e
	}
	if s.ssh == nil {
		return nil, errors.New("远程桌面通道不可用。")
	}
	run := s.ssh.owned(ctx, id, attempt)
	if run == nil || !run.Connected() {
		return nil, errors.New("请先连接 SSH，再打开远程桌面。")
	}
	return run, nil
}

func (s *DesktopService) VNC(ctx context.Context, session string, attempt int, target DesktopTarget, events *mygo.Channel[DesktopPacket]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.vNCStream(ctx, session, attempt, target, events)
}
func (s *DesktopService) vNCStream(ctx context.Context, session string, attempt int, target DesktopTarget, events eventSink[DesktopPacket]) error {
	run, e := s.session(ctx, session, attempt, target)
	if e != nil {
		return e
	}
	if events == nil {
		return errors.New("VNC 显示通道不可用。")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return errors.New("远程桌面连接较多，请先关闭不再使用的连接。")
	}
	bounded, stop := context.WithTimeout(ctx, 10*time.Second)
	conn, e := run.OpenTCP(bounded, net.JoinHostPort(strings.Trim(target.Host, "[]"), strconv.Itoa(target.Port)))
	stop()
	if e != nil {
		return e
	}
	defer conn.Close()
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	watch := context.AfterFunc(life, func() { _ = conn.Close() })
	defer watch()
	id := rand.Text()
	stream := &desktopStream{page: callerPage(ctx), session: session, attempt: attempt, run: run, conn: conn, write: make(chan struct{}, 1), cancel: cancel}
	s.mu.Lock()
	s.streams[id] = stream
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.streams, id); s.mu.Unlock() }()
	if e = events.Send(DesktopPacket{ID: id, Type: "ready"}); e != nil {
		return nil
	}
	return streamVNCConn(life, id, conn, events)
}

const (
	vncCoalesceLimit = 64 << 10
	vncCoalesceDelay = 4 * time.Millisecond
)

func streamVNCConn(life context.Context, id string, conn io.ReadCloser, events eventSink[DesktopPacket]) error {
	var (
		bufMu    sync.Mutex
		notFull  = sync.NewCond(&bufMu)
		pending  = make([]byte, 0, vncCoalesceLimit)
		readErr  error
		readDone bool
		closed   bool
	)
	wake := make(chan struct{}, 1)
	notify := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		tmp := make([]byte, 32<<10)
		defer clear(tmp)
		for {
			n, rerr := conn.Read(tmp)
			if n > 0 {
				chunk := tmp[:n]
				bufMu.Lock()
				for len(chunk) > 0 && !closed {
					for len(pending) >= vncCoalesceLimit && !closed {
						notFull.Wait()
					}
					if closed {
						break
					}
					take := min(len(chunk), vncCoalesceLimit-len(pending))
					pending = append(pending, chunk[:take]...)
					chunk = chunk[take:]
					notify()
				}
				bufMu.Unlock()
			}
			if rerr != nil {
				bufMu.Lock()
				readErr = rerr
				readDone = true
				notify()
				bufMu.Unlock()
				return
			}
		}
	}()
	defer func() {
		bufMu.Lock()
		closed = true
		clear(pending)
		pending = pending[:0]
		notFull.Broadcast()
		bufMu.Unlock()
		_ = conn.Close()
		<-readerDone
	}()
	sendBuf := make([]byte, 0, vncCoalesceLimit)
	defer clear(sendBuf)
	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timerC = nil
		}
	}
	defer stopTimer()
	flush := func() bool {
		stopTimer()
		bufMu.Lock()
		if len(pending) == 0 {
			bufMu.Unlock()
			return true
		}
		sendBuf = append(sendBuf[:0], pending...)
		clear(pending)
		pending = pending[:0]
		notFull.Broadcast()
		bufMu.Unlock()
		err := events.Send(DesktopPacket{ID: id, Type: "data", Data: base64.StdEncoding.EncodeToString(sendBuf)})
		clear(sendBuf)
		sendBuf = sendBuf[:0]
		return err == nil
	}
	for {
		bufMu.Lock()
		n := len(pending)
		done := readDone
		rerr := readErr
		bufMu.Unlock()
		if n >= vncCoalesceLimit {
			if !flush() {
				return nil
			}
			continue
		}
		if done {
			if n > 0 && !flush() {
				return nil
			}
			if life.Err() != nil || rerr == io.EOF {
				return nil
			}
			return errors.New("VNC 数据连接已中断，请检查服务后重新连接。")
		}
		if n > 0 && timerC == nil {
			if timer == nil {
				timer = time.NewTimer(vncCoalesceDelay)
			} else {
				timer.Reset(vncCoalesceDelay)
			}
			timerC = timer.C
		}
		select {
		case <-wake:
		case <-timerC:
			timerC = nil
			if !flush() {
				return nil
			}
		case <-life.Done():
			return nil
		}
	}
}
func (s *DesktopService) Write(ctx context.Context, id, data string) error {
	if e := workspaceCaller(ctx); e != nil {
		return e
	}
	if len(data) > 90000 {
		return errors.New("VNC 输入超过单次限制。")
	}
	s.mu.Lock()
	stream := s.streams[id]
	s.mu.Unlock()
	if stream == nil || stream.page != callerPage(ctx) || s.ssh.owned(ctx, stream.session, stream.attempt) != stream.run || !stream.run.Connected() {
		return errors.New("VNC 连接已结束。")
	}
	select {
	case stream.write <- struct{}{}:
		defer func() { <-stream.write }()
	default:
		return errors.New("VNC 输入正在等待发送。")
	}
	buffer, e := base64.StdEncoding.DecodeString(data)
	defer clear(buffer)
	if e != nil || len(buffer) > 64<<10 {
		return errors.New("VNC 输入编码无效。")
	}
	bounded, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	watch := context.AfterFunc(bounded, stream.cancel)
	defer watch()
	for len(buffer) > 0 {
		n, e := stream.conn.Write(buffer)
		if e != nil || n == 0 {
			stream.cancel()
			return errors.New("VNC 输入未确认，请重新连接；不会自动重发。")
		}
		buffer = buffer[n:]
	}
	return nil
}

func (s *DesktopService) RDP(ctx context.Context, session string, attempt int, target DesktopTarget, user string, events *mygo.Channel[DesktopPacket]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.rDPStream(ctx, session, attempt, target, user, events)
}
func (s *DesktopService) rDPStream(ctx context.Context, session string, attempt int, target DesktopTarget, user string, events eventSink[DesktopPacket]) error {
	run, e := s.session(ctx, session, attempt, target)
	if e != nil {
		return e
	}
	if mygo.CallerWindow(ctx) == nil || events == nil {
		return errors.New("请在 Windows 桌面会话中打开 RDP。")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return errors.New("远程桌面连接较多，请先停止不再使用的通道。")
	}
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-run.Done():
			cancel()
		case <-life.Done():
		}
	}()
	tunnel, e := display.StartRDP(life, run.OpenTCP, net.JoinHostPort(strings.Trim(target.Host, "[]"), strconv.Itoa(target.Port)))
	if e != nil {
		return e
	}
	defer tunnel.Close()
	file, cleanup, e := display.WriteRDPFile(tunnel.Address(), user)
	if e != nil {
		return e
	}
	defer cleanup()
	wait, e := display.LaunchRDP(file)
	if e != nil {
		return e
	}
	finished := make(chan struct{})
	go func() { _ = wait(); close(finished) }()
	if e = events.Send(DesktopPacket{Type: "ready"}); e != nil {
		return nil
	}
	select {
	case <-life.Done():
	case <-finished:
	}
	return nil
}

func (s *DesktopService) X11(ctx context.Context, session string, attempt int, draft X11Draft, events *mygo.Channel[DesktopPacket]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.x11Stream(ctx, session, attempt, draft, events)
}
func (s *DesktopService) x11Stream(ctx context.Context, session string, attempt int, draft X11Draft, events eventSink[DesktopPacket]) error {
	run, e := s.session(ctx, session, attempt, draft.Local)
	if e != nil {
		return e
	}
	if mygo.CallerWindow(ctx) == nil || events == nil {
		return errors.New("X11 需要桌面客户端连接本机 X 服务器。")
	}
	if !draft.Confirmed {
		return errors.New("请先确认远程图形命令和本机 X 显示访问。")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return errors.New("远程图形连接较多，请先停止不再使用的会话。")
	}
	states := make(chan string, 8)
	forward, e := run.StartX11(ctx, sshclient.X11Options{Address: net.JoinHostPort(strings.Trim(draft.Local.Host, "[]"), strconv.Itoa(draft.Local.Port)), Cookie: draft.Cookie, Command: draft.Command, Notify: func(kind string) {
		select {
		case states <- kind:
		default:
		}
	}})
	draft.Cookie = ""
	if e != nil {
		return e
	}
	defer forward.Close()
	if e = events.Send(DesktopPacket{Type: "ready"}); e != nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-run.Done():
			return nil
		case <-forward.Done():
			if ctx.Err() != nil {
				return nil
			}
			return forward.Err()
		case kind := <-states:
			if e = events.Send(DesktopPacket{Type: kind}); e != nil {
				return nil
			}
		}
	}
}
