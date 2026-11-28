package sshclient

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"yaare/internal/adapters/sessionlog"
	"yaare/internal/domain/workspace"
)

type Config struct {
	Host              string
	Port              int
	Username          string
	Authentication    string
	UseAgent          bool
	Password          []byte
	PrivateKeyPath    string
	PrivateKey        []byte
	Passphrase        []byte
	Timeout           time.Duration
	Name              string
	Jumps             []Config
	Forwards          []workspace.ForwardRule
	preparedSigner    ssh.Signer
	Proxy             workspace.ProxyConfig
	ProxyPassword     []byte
	SaveProxyPassword func([]byte) error
}

type Event struct {
	Type           string           `json:"type"`
	Revision       int              `json:"revision"`
	Data           []byte           `json:"data,omitempty"`
	Challenge      *Challenge       `json:"challenge,omitempty"`
	Authentication *AuthChallenge   `json:"authentication,omitempty"`
	Code           string           `json:"code,omitempty"`
	Message        string           `json:"message,omitempty"`
	Forward        *ForwardState    `json:"forward,omitempty"`
	Directory      *DirectoryState  `json:"directory,omitempty"`
	Log            *sessionlog.Info `json:"log,omitempty"`
}

type Session struct {
	ctx              context.Context
	cancel           context.CancelFunc
	trust            *TrustStore
	emit             func(Event) error
	eventMu          sync.Mutex
	revision         int
	mu               sync.Mutex
	writeMu          sync.Mutex
	resizeMu         sync.Mutex
	stdin            io.WriteCloser
	shell            *ssh.Session
	client           *ssh.Client
	connected        bool
	terminalTransfer string
	fileIdentity     string
	browseSlot       chan struct{}
	browseIdle       *idleBrowseChannel
	x11Mu            sync.Mutex
	x11Once          sync.Once
	x11Route         <-chan ssh.NewChannel
	x11Active        *X11Run
	cols, rows       int
	pending          *Challenge
	answer           chan string
	pendingAuth      *AuthChallenge
	authAnswer       chan [][]byte
	outputReady      chan struct{}
	outputMu         sync.Mutex
	output           []byte
	outputErr        error
	outputTimer      *time.Timer
	outputEpoch      uint64
	authAddress      string
	authName         string
	authKind         string
	forwardMu        sync.Mutex
	forwards         map[string]*forwardRun
	forwardSlots     chan struct{}
	forwardGates     map[string]chan struct{}
	forwardRevision  int
	directory        DirectoryState
	directorySent    DirectoryState
	directoryHasSent bool
	directorySending bool
	directoryClosed  bool
	directoryDone    chan struct{}
	integration      *shellIntegrationAttempt
	directoryDecoder directoryDecoder
	inputEpoch       uint64
}

func NewSession(ctx context.Context, trust *TrustStore, emit func(Event) error) *Session {
	ctx, cancel := context.WithCancel(ctx)
	return &Session{ctx: ctx, cancel: cancel, trust: trust, emit: emit, cols: 80, rows: 24, outputReady: make(chan struct{}), browseSlot: make(chan struct{}, 1)}
}

func (s *Session) send(event Event) error {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if event.Type != "output" {
		s.revision++
		event.Revision = s.revision
	}
	if err := s.emit(event); err != nil {
		s.cancel()
		return err
	}
	return nil
}

func (s *Session) ask(challenge Challenge) (string, error) {
	challenge.Name = s.authName
	answer := make(chan string, 1)
	s.mu.Lock()
	s.pending = &challenge
	s.answer = answer
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.pending = nil; s.answer = nil; s.mu.Unlock() }()
	if err := s.send(Event{Type: "hostkey", Challenge: &challenge}); err != nil {
		return "", err
	}
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return "", s.ctx.Err()
	case decision := <-answer:
		return decision, nil
	case <-timer.C:
		return "", fault("HOST_KEY_TIMEOUT", "服务器指纹确认已超时，请重新连接。")
	}
}

func (s *Session) Resolve(id, decision string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil || s.pending.ID != id || s.ctx.Err() != nil || !s.pending.accepts(decision) {
		return false
	}
	select {
	case s.answer <- decision:
		s.pending = nil
		return true
	default:
		return false
	}
}
func (s *Session) Close() { s.cancel() }

func (s *Session) NotifyLog(info sessionlog.Info) { _ = s.send(Event{Type: "log", Log: &info}) }

func (s *Session) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected && s.ctx.Err() == nil
}

func (s *Session) Write(data []byte) bool {
	return s.writeTerminal(data, "")
}
func (s *Session) writeTerminal(data []byte, transfer string) bool {
	if len(data) == 0 {
		return true
	}
	if len(data) > 65536 {
		return false
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	stdin, ok := s.stdin, s.connected && s.terminalTransfer == transfer
	s.mu.Unlock()
	if !ok || stdin == nil || s.ctx.Err() != nil {
		return false
	}
	if transfer == "" {
		s.directoryBusy()
	}
	n, err := stdin.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		s.cancel()
		return false
	}
	return true
}

func (s *Session) Resize(columns, rows int) bool {
	if columns < 1 || columns > 1000 || rows < 1 || rows > 1000 {
		return false
	}
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	s.mu.Lock()
	s.cols, s.rows = columns, rows
	shell, live := s.shell, s.connected
	s.mu.Unlock()
	if shell != nil && live {
		if err := shell.WindowChange(rows, columns); err != nil {
			s.cancel()
			return false
		}
	}
	return s.ctx.Err() == nil
}

type remoteWriter struct{ s *Session }

func (w remoteWriter) Write(data []byte) (int, error) {
	select {
	case <-w.s.outputReady:
	case <-w.s.ctx.Done():
		return 0, w.s.ctx.Err()
	}
	w.s.outputMu.Lock()
	w.s.mu.Lock()
	epoch := w.s.inputEpoch
	transferring := w.s.terminalTransfer != ""
	w.s.mu.Unlock()
	var directory *DirectoryState
	directory = w.s.directoryDecoder.feed(data, epoch)
	if p := w.s.directoryDecoder.persistent; p != nil && !p.checking {
		p.checking = true
		go w.s.verifyPersistentDirectory(p)
	}
	if transferring && directory != nil && !directory.Ready {
		directory = nil
	}
	defer func() {
		if directory != nil {
			if transferring && directory.Ready {
				value := *directory
				time.AfterFunc(5*time.Millisecond, func() {
					if w.s.ctx.Err() == nil {
						_ = w.s.flushOutput()
						w.s.reportDirectory(value, epoch)
					}
				})
			} else {
				w.s.reportDirectory(*directory, epoch)
			}
		}
		w.s.outputMu.Unlock()
	}()
	if w.s.outputErr != nil {
		return 0, w.s.outputErr
	}
	n := 0
	for len(data) > 0 {
		size := min(len(data), 32768-len(w.s.output))
		w.s.output = append(w.s.output, data[:size]...)
		n += size
		data = data[size:]
		if len(w.s.output) == 32768 {
			if err := w.s.flushLocked(); err != nil {
				return n, err
			}
		}
	}
	if len(w.s.output) > 0 && w.s.outputTimer == nil {
		w.s.outputEpoch++
		epoch := w.s.outputEpoch
		w.s.outputTimer = time.AfterFunc(4*time.Millisecond, func() {
			w.s.outputMu.Lock()
			defer w.s.outputMu.Unlock()
			if epoch == w.s.outputEpoch {
				_ = w.s.flushLocked()
			}
		})
	}
	return n, nil
}

func (s *Session) flushLocked() error {
	s.outputEpoch++
	if s.outputTimer != nil {
		s.outputTimer.Stop()
		s.outputTimer = nil
	}
	if len(s.output) > 0 && s.outputErr == nil {
		s.outputErr = s.send(Event{Type: "output", Data: s.output})
		s.output = s.output[:0]
	}
	return s.outputErr
}
func (s *Session) flushOutput() error {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	return s.flushLocked()
}

func signer(path string, passphrase []byte) (ssh.Signer, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fault("KEY_UNAVAILABLE", "无法读取私钥，请重新选择文件。")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	defer clear(data)
	if err != nil || len(data) > 1024*1024 {
		return nil, fault("KEY_INVALID", "私钥文件不可用或超过 1 MB。")
	}
	return parseSigner(data, passphrase)
}

func parseSigner(data, passphrase []byte) (ssh.Signer, error) {
	if len(data) == 0 || len(data) > 1024*1024 {
		return nil, fault("KEY_INVALID", "私钥内容为空或超过 1 MB。")
	}
	if IsEncryptedPKCS8(data) {
		return nil, fault("KEY_INVALID", EncryptedPKCS8Message)
	}
	var key ssh.Signer
	var err error
	if len(passphrase) != 0 {
		key, err = ssh.ParsePrivateKeyWithPassphrase(data, passphrase)
	} else {
		key, err = ssh.ParsePrivateKey(data)
	}
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, fault("KEY_PASSPHRASE_REQUIRED", "此私钥已加密，请输入私钥口令。")
		}
		if errors.Is(err, x509.IncorrectPasswordError) {
			return nil, fault("KEY_DECRYPT_FAILED", "私钥口令错误。")
		}
		return nil, fault("KEY_INVALID", "私钥解析失败，请选择有效的 OpenSSH 或 PEM 私钥。")
	}
	return key, nil
}

func classify(err error) (string, string) {
	var f *Fault
	if errors.As(err, &f) {
		return f.Code, f.Message
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		if dns.IsNotFound {
			return "DNS_NOT_FOUND", "无法解析服务器地址。"
		}
		return "DNS_FAILED", "无法解析服务器地址。"
	}
	err = normalizeSocketError(err)
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "CONNECTION_TIMEOUT", "连接或 SSH 握手超时。"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "CONNECTION_REFUSED", "服务器拒绝连接，请检查地址、端口和 SSH 服务。"
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETDOWN) {
		return "NETWORK_UNREACHABLE", "SSH 连接失败，请检查网络和服务器配置。"
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "CONNECTION_LOST", "SSH 连接已断开。"
	}
	var algorithm *ssh.AlgorithmNegotiationError
	if errors.As(err, &algorithm) {
		return "ALGORITHM_UNSUPPORTED", "服务器没有可用的安全 SSH 算法。"
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return "AUTHENTICATION_FAILED", "SSH 认证失败，请检查用户名、密码或私钥。"
	}
	return "CONNECTION_FAILED", "SSH 连接失败，请检查网络和服务器配置。"
}

func (s *Session) Run(cfg *Config) {
	defer s.cancel()
	defer func() {
		s.mu.Lock()
		s.connected = false
		s.stdin = nil
		s.shell = nil
		s.client = nil
		s.pending = nil
		s.answer = nil
		s.mu.Unlock()
	}()
	err := s.run(cfg)
	if flushErr := s.flushOutput(); err == nil {
		err = flushErr
	}
	s.stopDirectoryNotifications()
	if s.ctx.Err() != nil {
		_ = s.send(Event{Type: "disconnected", Code: "CANCELLED", Message: "连接已取消或断开。"})
		return
	}
	if err != nil {
		code, message := classify(err)
		_ = s.send(Event{Type: "failed", Code: code, Message: message})
		return
	}
	_ = s.send(Event{Type: "disconnected", Code: "REMOTE_CLOSED", Message: "远程会话已结束。"})
}

func (s *Session) run(cfg *Config) error {
	defer cfg.ClearSecrets()
	client, conn, cleanup, err := s.dialChain(cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := s.send(Event{Type: "progress", Message: "正在启动远程终端…"}); err != nil {
		return err
	}
	shell, err := client.NewSession()
	if err != nil {
		return fault("SESSION_FAILED", "服务器无法创建 SSH 会话。")
	}
	defer shell.Close()
	stdin, err := shell.StdinPipe()
	if err != nil {
		return fault("SESSION_FAILED", "无法打开终端输入通道。")
	}
	shell.Stdout = remoteWriter{s}
	shell.Stderr = remoteWriter{s}
	s.mu.Lock()
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	if err = shell.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}); err != nil {
		return fault("PTY_FAILED", "服务器拒绝分配交互式终端。")
	}
	s.directoryDecoder = directoryDecoder{token: workspace.NewID(), host: cfg.Host}
	if err = shell.Shell(); err != nil {
		return fault("SHELL_FAILED", "服务器无法启动登录 Shell。")
	}
	_ = conn.SetDeadline(time.Time{})
	s.mu.Lock()
	s.stdin = stdin
	s.shell = shell
	s.client = client
	s.connected = true
	latestCols, latestRows := s.cols, s.rows
	s.mu.Unlock()
	if latestCols != cols || latestRows != rows {
		s.Resize(latestCols, latestRows)
	}
	if err = s.send(Event{Type: "connected", Message: "SSH 已连接。"}); err != nil {
		return err
	}
	close(s.outputReady)
	s.initializeForwards(cfg.Forwards)
	defer s.closeForwards()
	keepaliveCtx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	go conn.watchIdle(keepaliveCtx, 30*time.Second, func() error {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		return err
	})
	err = shell.Wait()
	var exit *ssh.ExitError
	if err == nil || errors.As(err, &exit) {
		return nil
	}
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	return fault("CONNECTION_LOST", "SSH 连接已断开。")
}
