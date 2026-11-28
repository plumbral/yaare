package sshclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
	"yaare/internal/adapters/sshagent"
	"yaare/internal/domain/workspace"
)

func (cfg *Config) ClearSecrets() {
	clear(cfg.Password)
	cfg.Password = nil
	clear(cfg.Passphrase)
	cfg.Passphrase = nil
	cfg.PrivateKeyPath = ""
	clear(cfg.PrivateKey)
	cfg.PrivateKey = nil
	cfg.preparedSigner = nil
	clear(cfg.ProxyPassword)
	cfg.ProxyPassword = nil
	cfg.SaveProxyPassword = nil
	for i := range cfg.Jumps {
		cfg.Jumps[i].ClearSecrets()
	}
}

type handshakeClock struct {
	mu      sync.Mutex
	timer   *time.Timer
	conn    net.Conn
	timeout time.Duration
	epoch   uint64
	expired bool
}

func (c *handshakeClock) arm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.epoch++
	epoch := c.epoch
	c.timer = time.AfterFunc(c.timeout, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.epoch == epoch {
			c.expired = true
			c.conn.Close()
		}
	})
}
func (c *handshakeClock) pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

func (s *Session) dialChain(cfg *Config) (*ssh.Client, *sessionTransport, func(), error) {
	if err := workspace.ValidateProxy(cfg.Proxy); err != nil {
		return nil, nil, func() {}, fault("INVALID_PROXY", "请检查代理地址、端口和认证设置。")
	}
	if len(cfg.Jumps) > 8 {
		return nil, nil, func() {}, fault("JUMP_LIMIT", "最多使用 8 台跳板机。")
	}
	clients := []*ssh.Client{}
	conns := []net.Conn{}
	stops := []func() bool{}
	cleanup := func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
		for i := len(clients) - 1; i >= 0; i-- {
			clients[i].Close()
		}
		for _, c := range conns {
			c.Close()
		}
	}
	fail := func(err error) (*ssh.Client, *sessionTransport, func(), error) {
		cleanup()
		return nil, nil, func() {}, err
	}
	hops := make([]*Config, 0, len(cfg.Jumps)+1)
	for i := range cfg.Jumps {
		hops = append(hops, &cfg.Jumps[i])
	}
	hops = append(hops, cfg)
	var previous *ssh.Client
	var transport *sessionTransport
	for i, hop := range hops {
		if s.ctx.Err() != nil {
			return fail(s.ctx.Err())
		}
		jump := i < len(cfg.Jumps)
		if hop.UseAgent && (hop.Authentication != "key" || hop.PrivateKeyPath != "" || len(hop.PrivateKey) > 0 || len(hop.Passphrase) != 0) {
			return fail(fault("INVALID_AUTH", "SSH Agent 不能同时使用私钥文件、粘贴内容或口令。"))
		}
		if !jump && hop.Authentication == "key" && !hop.UseAgent {
			var err error
			if len(hop.PrivateKey) > 0 && hop.PrivateKeyPath != "" {
				return fail(fault("KEY_INVALID", "请选择一种私钥来源。"))
			}
			if len(hop.PrivateKey) > 0 {
				hop.preparedSigner, err = parseSigner(hop.PrivateKey, hop.Passphrase)
			} else {
				hop.preparedSigner, err = signer(hop.PrivateKeyPath, hop.Passphrase)
			}
			if err != nil {
				return fail(err)
			}
		}
		address := net.JoinHostPort(strings.Trim(hop.Host, "[]"), strconv.Itoa(hop.Port))
		if hop.Host == "" || len(hop.Host) > 253 || strings.ContainsAny(hop.Host, "/\\\x00 \t\r\n") || hop.Port < 1 || hop.Port > 65535 || strings.TrimSpace(hop.Username) == "" || len(hop.Username) > 128 || strings.ContainsAny(hop.Username, "\x00\r\n") || hop.Timeout < 3*time.Second || hop.Timeout > 120*time.Second || len(hop.Password) > 16384 || len(hop.Passphrase) > 16384 {
			return fail(fault("INVALID_CONFIG", "请检查链路中的服务器地址、端口、用户名和连接超时。"))
		}
		s.authAddress = address
		s.authName = hop.Name
		label := "目标主机"
		if jump {
			label = fmt.Sprintf("跳板机 %d/%d · %s", i+1, len(cfg.Jumps), hop.Name)
		}
		if err := s.send(Event{Type: "progress", Message: "正在连接" + label + " · " + address}); err != nil {
			return fail(err)
		}
		dialCtx, cancel := context.WithTimeout(s.ctx, hop.Timeout)
		var conn net.Conn
		var err error
		if previous == nil {
			if cfg.Proxy.Kind == "auto" {
				proxy, password, resolveErr := systemProxy(dialCtx, address)
				if resolveErr != nil {
					cancel()
					code, message := classify(resolveErr)
					return fail(fault(code, label+"（"+address+"）："+message))
				}
				clear(cfg.ProxyPassword)
				cfg.Proxy, cfg.ProxyPassword = proxy, []byte(password)
			}
			if cfg.Proxy.Kind != "" && cfg.Proxy.Kind != "none" {
				cancel()
				conn, err = s.dialProxy(cfg, address, hop.Timeout)
			} else {
				conn, err = (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
			}
		} else {
			conn, err = previous.DialContext(dialCtx, "tcp", address)
		}
		cancel()
		if err != nil {
			code, message := classify(err)
			return fail(fault(code, label+"（"+address+"）："+message))
		}
		if !jump {
			transport = &sessionTransport{Conn: conn}
			conn = transport
		}
		conns = append(conns, conn)
		stops = append(stops, context.AfterFunc(s.ctx, func() { conn.Close() }))
		if err := s.send(Event{Type: "progress", Message: "正在进行 SSH 握手 · " + address}); err != nil {
			return fail(err)
		}
		client, err := s.handshake(conn, hop, address, jump)
		hop.ClearSecrets()
		if err != nil {
			code, message := classify(err)
			return fail(fault(code, label+"（"+address+"）："+message))
		}
		clients = append(clients, client)
		previous = client
	}
	return previous, transport, cleanup, nil
}

func (s *Session) handshake(conn net.Conn, cfg *Config, address string, jump bool) (*ssh.Client, error) {
	var agentClient *sshagent.Client
	defer func() {
		if agentClient != nil {
			agentClient.Close()
		}
	}()
	clock := &handshakeClock{conn: conn, timeout: cfg.Timeout}
	clock.arm()
	defer clock.pause()
	ask := func(label string) ([]byte, error) {
		clock.pause()
		defer clock.arm()
		answers, err := s.askAuthentication("", "", []string{label}, []bool{false})
		if err != nil {
			return nil, err
		}
		defer clear(answers)
		return answers[0], nil
	}
	var auth ssh.AuthMethod
	switch cfg.Authentication {
	case "password", "none":
		auth = ssh.PasswordCallback(func() (string, error) {
			if jump && len(cfg.Password) == 0 {
				answer, err := ask("跳板机登录密码")
				defer clear(answer)
				return string(answer), err
			}
			return string(cfg.Password), nil
		})
	case "key":
		auth = ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			if cfg.UseAgent {
				var err error
				if agentClient == nil {
					agentClient, err = sshagent.Open(s.ctx)
					if err != nil {
						return nil, fault("AGENT_UNAVAILABLE", err.Error())
					}
				}
				signers, err := agentClient.Signers()
				if err != nil {
					code := "AGENT_UNAVAILABLE"
					if errors.Is(err, sshagent.ErrEmpty) {
						code = "AGENT_EMPTY"
					}
					return nil, fault(code, err.Error())
				}
				result := make([]ssh.Signer, 0, len(signers))
				for _, key := range signers {
					result = append(result, timedAgentSigner{Signer: key, clock: clock})
				}
				return result, nil
			}
			if cfg.preparedSigner != nil {
				return []ssh.Signer{cfg.preparedSigner}, nil
			}
			if len(cfg.PrivateKey) > 0 && cfg.PrivateKeyPath != "" {
				return nil, fault("KEY_INVALID", "请选择一种私钥来源。")
			}
			if len(cfg.PrivateKey) == 0 && cfg.PrivateKeyPath == "" {
				return nil, fault("KEY_UNAVAILABLE", "此主机没有可用私钥，请先关联已保存的身份或密钥。")
			}
			parse := func() (ssh.Signer, error) {
				if len(cfg.PrivateKey) > 0 {
					return parseSigner(cfg.PrivateKey, cfg.Passphrase)
				}
				return signer(cfg.PrivateKeyPath, cfg.Passphrase)
			}
			key, err := parse()
			var f *Fault
			if errors.As(err, &f) && f.Code == "KEY_PASSPHRASE_REQUIRED" && jump {
				cfg.Passphrase, err = ask("跳板机私钥口令")
				if err == nil {
					key, err = parse()
				}
			}
			if err != nil {
				return nil, err
			}
			return []ssh.Signer{key}, nil
		})
	default:
		return nil, fault("AUTH_REQUIRED", "请选择密码或私钥认证。")
	}
	algs := ssh.SupportedAlgorithms()
	trustPath, err := s.trust.path()
	if err != nil {
		return nil, err
	}
	hostTrust, err := s.trust.load(trustPath, address)
	if err != nil {
		return nil, err
	}
	hostAlgorithms := hostTrust.algorithms(algs.HostKeys)
	if len(hostAlgorithms) == 0 {
		return nil, fault("ALGORITHM_UNSUPPORTED", "服务器没有可用的安全 SSH 算法。")
	}
	var pinned []byte
	var pinMu sync.Mutex
	config := &ssh.ClientConfig{User: cfg.Username, Auth: []ssh.AuthMethod{auth}, ClientVersion: "SSH-2.0-Yaare", Config: ssh.Config{KeyExchanges: algs.KeyExchanges, Ciphers: algs.Ciphers, MACs: algs.MACs}, HostKeyAlgorithms: hostAlgorithms,
		HostKeyCallback: func(host string, _ net.Addr, key ssh.PublicKey) error {
			pinMu.Lock()
			defer pinMu.Unlock()
			if pinned != nil {
				if bytes.Equal(pinned, key.Marshal()) {
					return nil
				}
				return fault("HOST_KEY_CHANGED", "当前 SSH 会话的服务器指纹发生变化，连接已拒绝。")
			}
			clock.pause()
			defer clock.arm()
			if err := s.trust.verify(s.ctx, trustPath, host, key, s.ask); err != nil {
				return err
			}
			pinned = append([]byte(nil), key.Marshal()...)
			return s.send(Event{Type: "progress", Message: "正在进行 SSH 认证 · " + address})
		}}
	if !jump {
		config.BannerCallback = s.showAuthenticationBanner
	}
	config.Auth = append(config.Auth, ssh.RetryableAuthMethod(ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		clock.pause()
		defer clock.arm()
		values, err := s.askAuthentication(name, instruction, questions, echos)
		if err != nil {
			return nil, err
		}
		answers := make([]string, len(values))
		for i, value := range values {
			answers[i] = string(value)
			clear(value)
		}
		return answers, nil
	}), 3))
	cc, chans, requests, err := ssh.NewClientConn(conn, address, config)
	config.Auth = nil
	if err != nil {
		clock.mu.Lock()
		expired := clock.expired
		clock.mu.Unlock()
		if expired {
			return nil, fault("CONNECTION_TIMEOUT", "连接或 SSH 握手超时。")
		}
		return nil, hostTrust.negotiationError(err)
	}
	if !jump {
		s.mu.Lock()
		s.fileIdentity = fmt.Sprintf("%s\x00%s\x00%x", address, cfg.Username, sha256.Sum256(pinned))
		s.mu.Unlock()
	}
	return ssh.NewClient(cc, chans, requests), nil
}

func (s *Session) showAuthenticationBanner(message string) error {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	message = strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n':
			return '\n'
		case '\t':
			return r
		}
		if !unicode.IsGraphic(r) {
			return utf8.RuneError
		}
		return r
	}, message)
	data := []byte(strings.ReplaceAll(message, "\n", "\r\n"))
	for len(data) > 0 {
		size := min(len(data), 32768)
		if err := s.send(Event{Type: "output", Data: data[:size]}); err != nil {
			return err
		}
		data = data[size:]
	}
	return nil
}

type timedAgentSigner struct {
	ssh.Signer
	clock *handshakeClock
}

func (k timedAgentSigner) Sign(random io.Reader, data []byte) (*ssh.Signature, error) {
	k.clock.pause()
	defer k.clock.arm()
	return k.Signer.Sign(random, data)
}
func (k timedAgentSigner) SignWithAlgorithm(random io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	k.clock.pause()
	defer k.clock.arm()
	if signer, ok := k.Signer.(ssh.AlgorithmSigner); ok {
		return signer.SignWithAlgorithm(random, data, algorithm)
	}
	return nil, fault("AGENT_ALGORITHM", "SSH Agent 不支持请求的签名算法。")
}
