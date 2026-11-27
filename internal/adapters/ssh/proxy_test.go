package sshclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yaare/internal/domain/workspace"
)

func proxyFixture(t *testing.T, handler func(net.Conn)) workspace.ProxyConfig {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); listener.Close() })
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			go func() {
				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				handler(conn)
			}()
		}
	}()
	return workspace.ProxyConfig{Kind: "http", Address: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port}
}
func proxySession(t *testing.T) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := NewSession(ctx, nil, func(Event) error { return nil })
	t.Cleanup(s.Close)
	return s
}

func TestHTTPConnectPreservesPrefetchedBannerAndScopesCredentials(t *testing.T) {
	captured := make(chan *http.Request, 1)
	proxy := proxyFixture(t, func(conn net.Conn) {
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		captured <- req
		_, _ = conn.Write([]byte("HTTP/1.1 103 Early Hints\r\n\r\nHTTP/1.1 200 Connection Established\r\nX-Proxy: fixture\r\n\r\nSSH-2.0-prefetched\r\n"))
		_, _ = io.Copy(io.Discard, conn)
	})
	proxy.UseAuthentication = true
	proxy.Username = "proxy-user"
	cfg := Config{Proxy: proxy, ProxyPassword: []byte("synthetic-secret")}
	conn, err := proxySession(t).dialProxy(&cfg, "target.invalid:22", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if len(cfg.ProxyPassword) != 0 {
		t.Fatal("proxy secret retained")
	}
	request := <-captured
	if request.Method != "CONNECT" || request.Host != "target.invalid:22" || request.Header.Get("Proxy-Authorization") != "Basic cHJveHktdXNlcjpzeW50aGV0aWMtc2VjcmV0" || request.Header.Get("Authorization") != "" {
		t.Fatal("CONNECT authority or credential scope is wrong")
	}
	bytes := make([]byte, len("SSH-2.0-prefetched\r\n"))
	if _, err = io.ReadFull(conn, bytes); err != nil || string(bytes) != "SSH-2.0-prefetched\r\n" {
		t.Fatal("prefetched SSH data lost", err)
	}
}

func TestHTTPProxyEmptyPasswordUsesOneUseProxyChallenge(t *testing.T) {
	captured := make(chan string, 1)
	proxy := proxyFixture(t, func(conn net.Conn) {
		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		captured <- request.Header.Get("Proxy-Authorization")
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		_, _ = io.Copy(io.Discard, conn)
	})
	proxy.UseAuthentication = true
	proxy.Username = "proxy-user"
	var session *Session
	prompts := 0
	session = NewSession(context.Background(), nil, func(event Event) error {
		if event.Type != "authentication" {
			return nil
		}
		prompts++
		challenge := event.Authentication
		if challenge == nil || challenge.Kind != "proxy" || challenge.Address != net.JoinHostPort(proxy.Address, strconv.Itoa(proxy.Port)) || len(challenge.Prompts) != 1 || challenge.Prompts[0].Echo {
			return errors.New("proxy challenge has the wrong endpoint or secret semantics")
		}
		if !session.AnswerAuthentication(challenge.ID, []string{""}) || session.AnswerAuthentication(challenge.ID, []string{"replay"}) {
			return errors.New("proxy response is not accepted exactly once")
		}
		return nil
	})
	t.Cleanup(session.Close)
	session.authName, session.authAddress, session.authKind = "destination", "target.invalid:22", ""
	cfg := Config{Proxy: proxy}
	conn, err := session.dialProxy(&cfg, "target.invalid:22", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if prompts != 1 || <-captured != "Basic cHJveHktdXNlcjo=" {
		t.Fatal("empty HTTP Basic password was not sent through the proxy challenge")
	}
	if session.authName != "destination" || session.authAddress != "target.invalid:22" || session.authKind != "" || len(cfg.ProxyPassword) != 0 {
		t.Fatal("proxy authentication leaked into the SSH stage")
	}
}

func TestSOCKS5RequestAddressFamiliesAndAuthentication(t *testing.T) {
	for _, target := range []string{"destination.invalid:22", "127.0.0.1:22", "[::1]:22"} {
		t.Run(target, func(t *testing.T) {
			received := make(chan string, 1)
			proxy := proxyFixture(t, func(conn net.Conn) {
				var greeting [3]byte
				if _, e := io.ReadFull(conn, greeting[:]); e != nil || greeting != [3]byte{5, 1, 2} {
					return
				}
				_, _ = conn.Write([]byte{5, 2})
				var a [2]byte
				if _, e := io.ReadFull(conn, a[:]); e != nil || a[0] != 1 {
					return
				}
				u := make([]byte, int(a[1]))
				if _, e := io.ReadFull(conn, u); e != nil {
					return
				}
				var n [1]byte
				if _, e := io.ReadFull(conn, n[:]); e != nil {
					return
				}
				p := make([]byte, int(n[0]))
				if _, e := io.ReadFull(conn, p); e != nil || string(u) != "user" || string(p) != "password" {
					return
				}
				clear(p)
				_, _ = conn.Write([]byte{1, 0})
				var head [4]byte
				if _, e := io.ReadFull(conn, head[:]); e != nil || head[0] != 5 || head[1] != 1 || head[2] != 0 {
					return
				}
				size := 4
				if head[3] == 4 {
					size = 16
				}
				if head[3] == 3 {
					if _, e := io.ReadFull(conn, n[:]); e != nil {
						return
					}
					size = int(n[0])
				}
				host := make([]byte, size)
				if _, e := io.ReadFull(conn, host); e != nil {
					return
				}
				var port [2]byte
				if _, e := io.ReadFull(conn, port[:]); e != nil {
					return
				}
				address := string(host)
				if head[3] != 3 {
					address = net.IP(host).String()
				}
				received <- net.JoinHostPort(address, strconv.Itoa(int(binary.BigEndian.Uint16(port[:]))))
				_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				_, _ = conn.Write([]byte("tunnel"))
				_, _ = io.Copy(io.Discard, conn)
			})
			proxy.Kind = "socks5"
			proxy.UseAuthentication = true
			proxy.Username = "user"
			cfg := Config{Proxy: proxy, ProxyPassword: []byte("password")}
			conn, e := proxySession(t).dialProxy(&cfg, target, time.Second)
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close()
			if got := <-received; got != target {
				t.Fatal("wrong remote destination", got, target)
			}
			b := make([]byte, 6)
			if _, e = io.ReadFull(conn, b); e != nil || string(b) != "tunnel" {
				t.Fatal("SOCKS tunnel bytes differ", e)
			}
		})
	}
}

func TestProxyRefusalBoundsCancellationAndNoDirectFallback(t *testing.T) {
	for _, response := range []string{"HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n", "HTTP/1.1 302 Found\r\nLocation: http://example.invalid\r\nContent-Length: 0\r\n\r\n", strings.Repeat("x", 4097) + "\r\n\r\n"} {
		t.Run(response[:8], func(t *testing.T) {
			proxy := proxyFixture(t, func(conn net.Conn) {
				_, _ = http.ReadRequest(bufio.NewReader(conn))
				_, _ = conn.Write([]byte(response))
			})
			cfg := config("127.0.0.1:1")
			cfg.Proxy = proxy
			cfg.ProxyPassword = []byte("do-not-leak")
			_, err := proxySession(t).dialProxy(&cfg, "target.invalid:22", time.Second)
			if err == nil || strings.Contains(err.Error(), "do-not-leak") || len(cfg.ProxyPassword) != 0 {
				t.Fatal("refusal or secret cleanup failed")
			}
		})
	}
	var direct atomic.Int32
	target, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			direct.Add(1)
			c.Close()
		}
	}()
	proxy := proxyFixture(t, func(c net.Conn) {
		_, _ = http.ReadRequest(bufio.NewReader(c))
		_, _ = c.Write([]byte("HTTP/1.1 403 Forbidden\r\n\r\n"))
	})
	cfg := config(target.Addr().String())
	cfg.Proxy = proxy
	run := begin(t, cfg, store(t))
	for {
		select {
		case event := <-run.events:
			if event.Type == "failed" {
				if event.Code != "PROXY_DENIED" {
					t.Fatal(event.Code)
				}
				goto failed
			}
		case <-time.After(3 * time.Second):
			t.Fatal("proxy refusal timed out")
		}
	}
failed:
	run.ended(t)
	if direct.Load() != 0 {
		t.Fatal("proxy refusal caused direct fallback")
	}
	entered := make(chan struct{}, 2)
	silent := proxyFixture(t, func(c net.Conn) {
		_, _ = http.ReadRequest(bufio.NewReader(c))
		entered <- struct{}{}
		_, _ = io.Copy(io.Discard, c)
	})
	s := proxySession(t)
	done := make(chan error, 1)
	go func() { _, err := s.dialProxy(&Config{Proxy: silent}, "target.invalid:22", 5*time.Second); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("proxy handshake did not start")
	}
	s.Close()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancel did not stop proxy IO", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel leaked proxy handshake")
	}
	_, err := proxySession(t).dialProxy(&Config{Proxy: silent}, "target.invalid:22", 30*time.Millisecond)
	var f *Fault
	if !errors.As(err, &f) || f.Code != "PROXY_TIMEOUT" {
		t.Fatal("proxy timeout misclassified", err)
	}
}

func TestSOCKS5RejectsUnexpectedAuthAndCleansFailedPassword(t *testing.T) {
	proxy := proxyFixture(t, func(c net.Conn) { var b [3]byte; _, _ = io.ReadFull(c, b[:]); _, _ = c.Write([]byte{5, 2}) })
	proxy.Kind = "socks5"
	_, err := proxySession(t).dialProxy(&Config{Proxy: proxy}, "target.invalid:22", time.Second)
	var f *Fault
	if !errors.As(err, &f) || f.Code != "PROXY_PROTOCOL_ERROR" {
		t.Fatal("accepted an unoffered method", err)
	}
	var wrote atomic.Int32
	proxy = proxyFixture(t, func(c net.Conn) {
		var b [3]byte
		_, _ = io.ReadFull(c, b[:])
		_, _ = c.Write([]byte{5, 2})
		var x [1]byte
		n, _ := c.Read(x[:])
		wrote.Add(int32(n))
	})
	proxy.Kind = "socks5"
	proxy.UseAuthentication = true
	proxy.Username = "user"
	secret := bytes.Repeat([]byte("s"), 256)
	cfg := Config{Proxy: proxy, ProxyPassword: secret}
	_, err = proxySession(t).dialProxy(&cfg, "target.invalid:22", time.Second)
	if !errors.As(err, &f) || f.Code != "PROXY_AUTH_INVALID" || len(cfg.ProxyPassword) != 0 || !bytes.Equal(secret, make([]byte, len(secret))) || wrote.Load() != 0 {
		t.Fatal("invalid password was retained or sent")
	}
}

func TestHTTPConnectDoesNotSendProxyAuthorizationWithoutOptIn(t *testing.T) {
	headers := make(chan http.Header, 1)
	proxy := proxyFixture(t, func(c net.Conn) {
		r, e := http.ReadRequest(bufio.NewReader(c))
		if e != nil {
			return
		}
		headers <- r.Header
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
		_, _ = io.Copy(io.Discard, c)
	})
	cfg := Config{Proxy: proxy, ProxyPassword: []byte("never-send")}
	conn, e := proxySession(t).dialProxy(&cfg, "target.invalid:22", time.Second)
	if e != nil {
		t.Fatal(e)
	}
	conn.Close()
	if (<-headers).Get("Proxy-Authorization") != "" {
		t.Fatal("credentials sent without opt-in")
	}
	if !bytes.Equal(cfg.ProxyPassword, nil) {
		t.Fatal("unused secret retained")
	}
}
