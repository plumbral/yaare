package sshfixture

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

const Username = "fixture"
const Password = "yaare-loopback-test"
const Greeting = "\x1b[32mSSH fixture / 中文 UTF-8\x1b[0m\r\nfixture$ "

type Dimensions struct{ Columns, Rows uint32 }
type Server struct {
	listener       net.Listener
	mu             sync.Mutex
	host           ssh.Signer
	authorized     []byte
	connections    map[net.Conn]bool
	AuthCalls      atomic.Int32
	Resizes        chan Dimensions
	Inputs         chan []byte
	DenyPTY        atomic.Bool
	Interactive    atomic.Bool
	RekeyThreshold atomic.Uint64
}

func NewSigner() (ssh.Signer, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}

func Start() (*Server, error) {
	key, err := NewSigner()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{listener: listener, host: key, connections: make(map[net.Conn]bool), Resizes: make(chan Dimensions, 32), Inputs: make(chan []byte, 64)}
	go s.accept()
	return s, nil
}
func (s *Server) Address() string { return s.listener.Addr().String() }
func (s *Server) Fingerprint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ssh.FingerprintSHA256(s.host.PublicKey())
}
func (s *Server) SetHostKey(key ssh.Signer) { s.mu.Lock(); s.host = key; s.mu.Unlock() }
func (s *Server) Authorize(key ssh.PublicKey) {
	s.mu.Lock()
	s.authorized = key.Marshal()
	s.mu.Unlock()
}
func (s *Server) Close() {
	s.listener.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.connections {
		conn.Close()
	}
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.connections[conn] = true
		s.mu.Unlock()
		go s.serve(conn)
	}
}
func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	defer func() { s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
	config := &ssh.ServerConfig{
		Config: ssh.Config{RekeyThreshold: s.RekeyThreshold.Load()},
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			s.AuthCalls.Add(1)
			if !s.Interactive.Load() && meta.User() == Username && string(password) == Password {
				return nil, nil
			}
			return nil, errors.New("invalid fixture credentials")
		},
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			s.AuthCalls.Add(1)
			s.mu.Lock()
			ok := meta.User() == Username && bytes.Equal(s.authorized, key.Marshal())
			s.mu.Unlock()
			if ok {
				return nil, nil
			}
			return nil, errors.New("invalid fixture key")
		},
	}
	if s.Interactive.Load() {
		config.KeyboardInteractiveCallback = func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			a, err := challenge("Fixture MFA", "Two-step fixture", []string{"Password:", "Verification code:"}, []bool{false, false})
			if err != nil || len(a) != 2 || a[0] != Password || a[1] != "123456" {
				return nil, errors.New("fixture MFA failed")
			}
			clear(a)
			b, err := challenge("Fixture MFA", "Confirm", []string{"Type yes:"}, []bool{true})
			if err != nil || len(b) != 1 || b[0] != "yes" {
				return nil, errors.New("fixture confirmation failed")
			}
			clear(b)
			return nil, nil
		}
	}
	s.mu.Lock()
	config.AddHostKey(s.host)
	s.mu.Unlock()
	server, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer server.Close()
	go func() {
		for request := range requests {
			if request.WantReply {
				request.Reply(request.Type == "keepalive@openssh.com", nil)
			}
		}
	}()
	for next := range channels {
		if next.ChannelType() != "session" {
			next.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		channel, requests, err := next.Accept()
		if err != nil {
			continue
		}
		go s.session(channel, requests)
	}
}
func (s *Server) session(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	started := make(chan struct{}, 1)
	requestsDone := make(chan struct{})
	var shellOnce sync.Once
	go func() {
		defer close(requestsDone)
		for request := range requests {
			ok := true
			switch request.Type {
			case "pty-req":
				var p struct {
					Term                         string
					Columns, Rows, Width, Height uint32
					Modes                        string
				}
				if ssh.Unmarshal(request.Payload, &p) != nil || s.DenyPTY.Load() {
					ok = false
				} else {
					select {
					case s.Resizes <- Dimensions{p.Columns, p.Rows}:
					default:
					}
				}
			case "window-change":
				var p struct{ Columns, Rows, Width, Height uint32 }
				if ssh.Unmarshal(request.Payload, &p) != nil {
					ok = false
				} else {
					select {
					case s.Resizes <- Dimensions{p.Columns, p.Rows}:
					default:
					}
				}
			case "shell":
				shellOnce.Do(func() { started <- struct{}{} })
			default:
				ok = false
			}
			if request.WantReply {
				request.Reply(ok, nil)
			}
		}
	}()
	select {
	case <-started:
	case <-requestsDone:
		return
	}
	io.WriteString(channel, Greeting)
	buffer := make([]byte, 32768)
	var line []byte
	for {
		n, err := channel.Read(buffer)
		if n > 0 {
			data := append([]byte(nil), buffer[:n]...)
			select {
			case s.Inputs <- data:
			default:
			}
			channel.Write(data)
			for _, b := range data {
				if b == 3 {
					line = nil
					io.WriteString(channel, "^C\r\nfixture$ ")
					continue
				}
				if b == '\r' || b == '\n' {
					command := strings.TrimSpace(string(line))
					line = nil
					io.WriteString(channel, "\r\n")
					switch command {
					case "exit":
						channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						return
					case "burst":
						for i := 0; i < 8192; i++ {
							if _, err = fmt.Fprintf(channel, "remote-output-%05d 中文\r\n", i); err != nil {
								return
							}
						}
					case "whoami":
						io.WriteString(channel, Username+"\r\n")
					case "help":
						io.WriteString(channel, "loopback protocol fixture: whoami, burst, exit\r\n")
					default:
						if command != "" {
							io.WriteString(channel, "received: "+command+"\r\n")
						}
					}
					io.WriteString(channel, "fixture$ ")
				} else if len(line) < 65536 {
					line = append(line, b)
				}
			}
		}
		if err != nil {
			return
		}
	}
}
