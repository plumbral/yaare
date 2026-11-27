package sshclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

type proxyBufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *proxyBufferedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

func (s *Session) dialProxy(cfg *Config, target string, timeout time.Duration) (net.Conn, error) {
	defer func() { clear(cfg.ProxyPassword); cfg.ProxyPassword = nil }()
	p := cfg.Proxy
	address := net.JoinHostPort(strings.Trim(p.Address, "[]"), strconv.Itoa(p.Port))
	if err := s.send(Event{Type: "progress", Message: "正在连接 " + strings.ToUpper(p.Kind) + " 代理 · " + address}); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	cancel()
	if err != nil {
		if s.ctx.Err() != nil {
			return nil, s.ctx.Err()
		}
		var dns *net.DNSError
		if errors.As(err, &dns) {
			if dns.IsNotFound {
				return nil, fault("PROXY_DNS_NOT_FOUND", "无法解析代理服务器地址。")
			}
			return nil, fault("PROXY_DNS_FAILED", "无法解析代理服务器地址。")
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fault("PROXY_TIMEOUT", "连接代理服务器超时。")
		}
		return nil, fault("PROXY_CONNECTION_FAILED", "无法连接代理服务器，请检查地址、端口和代理服务。")
	}
	clock := &handshakeClock{conn: conn, timeout: timeout}
	clock.arm()
	defer clock.pause()
	stop := context.AfterFunc(s.ctx, func() { conn.Close() })
	defer stop()
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	var entered []byte
	defer func() { clear(entered) }()
	password := func() ([]byte, error) {
		if len(cfg.ProxyPassword) != 0 {
			return cfg.ProxyPassword, nil
		}
		clock.pause()
		defer clock.arm()
		name, endpoint, kind := s.authName, s.authAddress, s.authKind
		s.authName = strings.ToUpper(p.Kind) + " 代理"
		s.authAddress = address
		s.authKind = "proxy"
		defer func() { s.authName = name; s.authAddress = endpoint; s.authKind = kind }()
		values, e := s.askAuthentication("", "用户名："+p.Username, []string{"代理密码"}, []bool{false})
		if e != nil {
			return nil, e
		}
		defer clear(values)
		entered = values[0]
		return entered, nil
	}
	result := net.Conn(conn)
	if p.Kind == "socks5" {
		err = socksConnect(conn, target, p.Username, p.UseAuthentication, password)
	} else {
		result, err = httpConnect(conn, target, p.Username, p.UseAuthentication, password)
	}
	if err != nil {
		if s.ctx.Err() != nil {
			return nil, s.ctx.Err()
		}
		clock.mu.Lock()
		expired := clock.expired
		clock.mu.Unlock()
		if expired {
			return nil, fault("PROXY_TIMEOUT", "代理连接协商超时。")
		}
		var f *Fault
		if errors.As(err, &f) {
			return nil, err
		}
		return nil, fault("PROXY_PROTOCOL_ERROR", "代理协商失败，代理关闭连接或返回了无效响应。")
	}
	if s.ctx.Err() != nil {
		return nil, s.ctx.Err()
	}
	clock.pause()
	if len(entered) != 0 && cfg.SaveProxyPassword != nil {
		if err := cfg.SaveProxyPassword(entered); err != nil {
			_ = s.send(Event{Type: "progress", Message: "无法加密保存代理密码。"})
		}
	}
	ok = true
	return result, nil
}

func proxyWrite(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func socksConnect(conn net.Conn, target, username string, authenticate bool, password func() ([]byte, error)) error {
	method := byte(0)
	if authenticate {
		method = 2
	}
	if err := proxyWrite(conn, []byte{5, 1, method}); err != nil {
		return err
	}
	var response [2]byte
	if _, err := io.ReadFull(conn, response[:]); err != nil {
		return err
	}
	if response[0] != 5 {
		return fault("PROXY_PROTOCOL_ERROR", "服务器没有返回有效的 SOCKS5 响应。")
	}
	if response[1] == 255 {
		return fault("PROXY_AUTH_FAILED", "SOCKS5 代理不接受当前认证方式，请检查代理认证设置。")
	}
	if response[1] != method {
		return fault("PROXY_PROTOCOL_ERROR", "SOCKS5 代理选择了未提供的认证方式。")
	}
	if authenticate {
		secret, err := password()
		if err != nil {
			return err
		}
		if len(username) < 1 || len(username) > 255 || len(secret) < 1 || len(secret) > 255 {
			return fault("PROXY_AUTH_INVALID", "SOCKS5 用户名和密码应分别为 1–255 字节。")
		}
		auth := make([]byte, 0, len(username)+len(secret)+3)
		auth = append(auth, 1, byte(len(username)))
		auth = append(auth, username...)
		auth = append(auth, byte(len(secret)))
		auth = append(auth, secret...)
		err = proxyWrite(conn, auth)
		clear(auth)
		if err != nil {
			return err
		}
		if _, err = io.ReadFull(conn, response[:]); err != nil {
			return err
		}
		if response[0] != 1 {
			return fault("PROXY_PROTOCOL_ERROR", "SOCKS5 认证响应无效。")
		}
		if response[1] != 0 {
			return fault("PROXY_AUTH_FAILED", "代理用户名或密码错误，请重新连接。")
		}
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return fault("INVALID_PROXY", "代理目标地址无效。")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fault("INVALID_PROXY", "代理目标端口无效。")
	}
	request := []byte{5, 1, 0}
	ip := net.ParseIP(host)
	if v4 := ip.To4(); v4 != nil {
		request = append(request, 1)
		request = append(request, v4...)
	} else if ip != nil {
		request = append(request, 4)
		request = append(request, ip.To16()...)
	} else {
		if len(host) < 1 || len(host) > 253 {
			return fault("INVALID_PROXY", "代理目标主机名无效。")
		}
		request = append(request, 3, byte(len(host)))
		request = append(request, host...)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(n))
	if err = proxyWrite(conn, request); err != nil {
		return err
	}
	var head [4]byte
	if _, err = io.ReadFull(conn, head[:]); err != nil {
		return err
	}
	if head[0] != 5 || head[2] != 0 {
		return fault("PROXY_PROTOCOL_ERROR", "SOCKS5 隧道响应无效。")
	}
	if head[1] != 0 {
		switch head[1] {
		case 2:
			return fault("PROXY_DENIED", "代理规则拒绝访问链路入口，请检查代理访问策略。")
		case 3, 4:
			return fault("PROXY_TARGET_UNREACHABLE", "代理无法到达链路入口，请检查主机地址和代理网络。")
		case 5:
			return fault("PROXY_TARGET_REFUSED", "链路入口拒绝了代理的连接。")
		default:
			return fault("PROXY_CONNECT_FAILED", "SOCKS5 代理无法建立目标隧道。")
		}
	}
	size := 0
	switch head[3] {
	case 1:
		size = 4
	case 4:
		size = 16
	case 3:
		var b [1]byte
		if _, err = io.ReadFull(conn, b[:]); err != nil {
			return err
		}
		size = int(b[0])
		if size == 0 {
			return fault("PROXY_PROTOCOL_ERROR", "SOCKS5 返回的绑定地址为空。")
		}
	default:
		return fault("PROXY_PROTOCOL_ERROR", "SOCKS5 返回的地址类型无效。")
	}
	tail := make([]byte, size+2)
	_, err = io.ReadFull(conn, tail)
	return err
}

func httpConnect(conn net.Conn, target, username string, authenticate bool, password func() ([]byte, error)) (net.Conn, error) {
	request := []byte("CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n")
	if authenticate {
		secret, err := password()
		if err != nil {
			return nil, err
		}
		if len(secret) > 4096 {
			return nil, fault("PROXY_AUTH_INVALID", "HTTP 代理密码不能超过 4096 字节。")
		}
		raw := make([]byte, 0, len(username)+1+len(secret))
		raw = append(raw, username...)
		raw = append(raw, ':')
		raw = append(raw, secret...)
		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(raw)))
		base64.StdEncoding.Encode(encoded, raw)
		clear(raw)
		request = slices.Grow(request, len("Proxy-Authorization: Basic ")+len(encoded)+4)
		request = append(request, "Proxy-Authorization: Basic "...)
		request = append(request, encoded...)
		clear(encoded)
		request = append(request, '\r', '\n')
	}
	request = append(request, '\r', '\n')
	err := proxyWrite(conn, request)
	clear(request)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 4096)
	remaining := 32768
	for interim := 0; interim < 5; interim++ {
		header := make([]byte, 0, 1024)
		for {
			line, e := reader.ReadSlice('\n')
			if e != nil {
				return nil, fault("PROXY_PROTOCOL_ERROR", "HTTP 代理响应不完整，或单行响应超过 4 KB。")
			}
			if len(line) > remaining {
				return nil, fault("PROXY_PROTOCOL_ERROR", "HTTP 代理响应头超过 32 KB。")
			}
			remaining -= len(line)
			header = append(header, line...)
			if bytes.Equal(line, []byte("\r\n")) {
				break
			}
		}
		response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(header)), &http.Request{Method: http.MethodConnect})
		if err != nil {
			return nil, fault("PROXY_PROTOCOL_ERROR", "HTTP 代理返回了无效响应。")
		}
		if response.StatusCode >= 100 && response.StatusCode < 200 && response.StatusCode != 101 {
			continue
		}
		if response.StatusCode == 407 {
			if authenticate {
				values := response.Header.Values("Proxy-Authenticate")
				if len(values) > 0 && !strings.Contains(strings.ToLower(strings.Join(values, ",")), "basic") {
					return nil, fault("PROXY_AUTH_UNSUPPORTED", "HTTP 代理当前支持 Basic 认证；此代理要求其他认证方式。")
				}
			}
			return nil, fault("PROXY_AUTH_FAILED", "HTTP 代理认证失败或要求认证，请检查代理用户名和密码。")
		}
		if response.StatusCode < 200 || response.StatusCode > 299 {
			return nil, fault("PROXY_DENIED", "HTTP 代理拒绝建立隧道，请检查访问策略和链路入口。")
		}
		return &proxyBufferedConn{Conn: conn, reader: reader}, nil
	}
	return nil, fault("PROXY_PROTOCOL_ERROR", "HTTP 代理连续返回了过多临时响应。")
}
