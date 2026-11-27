package sshagent

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var ErrUnavailable = errors.New("SSH Agent 不可用，请启动系统 Agent，或检查 SSH_AUTH_SOCK。")
var ErrEmpty = errors.New("SSH Agent 尚未加载密钥，请先向 Agent 添加密钥。")

type AgentKey struct {
	Name        string `json:"name"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}
type AgentInfo struct {
	Available bool       `json:"available"`
	Message   string     `json:"message"`
	Keys      []AgentKey `json:"keys"`
}
type Client struct {
	conn   net.Conn
	client agent.ExtendedAgent
	stop   func() bool
}

func Open(ctx context.Context) (*Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := dial(dialCtx, os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return nil, ErrUnavailable
	}
	return newClient(ctx, conn), nil
}
func newClient(ctx context.Context, conn net.Conn) *Client {
	conn.SetDeadline(time.Now().Add(2 * time.Minute))
	return &Client{conn: conn, client: agent.NewClient(conn), stop: context.AfterFunc(ctx, func() { conn.Close() })}
}
func (c *Client) Close() {
	if c.stop != nil {
		c.stop()
	}
	c.conn.Close()
}
func (c *Client) Signers() ([]ssh.Signer, error) {
	signers, err := c.client.Signers()
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(signers) == 0 {
		return nil, ErrEmpty
	}
	if len(signers) > 256 {
		return nil, errors.New("SSH Agent 已加载的密钥过多，请减少到 256 个以内。")
	}
	return signers, nil
}
func Probe(ctx context.Context) AgentInfo {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	info := AgentInfo{Keys: []AgentKey{}}
	c, err := Open(ctx)
	if err != nil {
		info.Message = err.Error()
		return info
	}
	defer c.Close()
	keys, err := c.client.List()
	if err != nil {
		info.Message = ErrUnavailable.Error()
		return info
	}
	info.Available = true
	for _, key := range keys {
		public, err := ssh.ParsePublicKey(key.Blob)
		if err != nil {
			continue
		}
		name := filepath.Base(strings.ReplaceAll(key.Comment, `\`, "/"))
		name = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, name)
		if len([]rune(name)) > 80 {
			name = string([]rune(name)[:80])
		}
		if name == "" || name == "." {
			name = public.Type()
		}
		info.Keys = append(info.Keys, AgentKey{Name: name, Algorithm: public.Type(), Fingerprint: ssh.FingerprintSHA256(public)})
		if len(info.Keys) == 256 {
			break
		}
	}
	if len(info.Keys) == 0 {
		info.Message = ErrEmpty.Error()
	} else {
		info.Message = "使用当前系统 SSH Agent 中已加载的密钥认证。"
	}
	return info
}
