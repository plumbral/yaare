package ipc

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/sessionlog"

	"github.com/egoist/mygo"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

type SSHConnectRequest struct {
	HostID             string                  `json:"hostId,omitempty"`
	SessionID          string                  `json:"sessionId"`
	Attempt            int                     `json:"attempt"`
	Address            string                  `json:"address"`
	Port               int                     `json:"port"`
	Username           string                  `json:"username"`
	Authentication     string                  `json:"authentication"`
	KeyID              string                  `json:"keyId"`
	UseAgent           bool                    `json:"useAgent"`
	Password           string                  `json:"password"`
	PrivateKey         string                  `json:"privateKey"`
	TemporaryKeyToken  string                  `json:"temporaryKeyToken,omitempty"`
	Passphrase         string                  `json:"passphrase"`
	TimeoutSeconds     int                     `json:"timeoutSeconds"`
	IdentityID         string                  `json:"identityId"`
	UseSavedCredential bool                    `json:"useSavedCredential,omitempty"`
	Name               string                  `json:"name"`
	JumpHostIDs        []string                `json:"jumpHostIds"`
	Forwards           []workspace.ForwardRule `json:"forwards"`
	Proxy              workspace.ProxyConfig   `json:"proxy"`
}

type ownedSSH struct {
	profileID   string
	systemRoute string
	name        string
	page        *callerScope
	attempt     int
	session     *sshclient.Session
	logMu       sync.Mutex
	record      atomic.Pointer[sessionlog.Recording]
}

type SSHService struct {
	credentialProtector secrets.Protector
	mu                  sync.Mutex
	runs                map[string]*ownedSSH
	keys                *KeyService
	trust               *sshclient.TrustStore
	identities          *IdentityService
	workspace           *app.Workspace
	logs                *sessionlog.Store
	broadcasts          map[string]*broadcastRecord
	broadcastSlots      chan struct{}
}

func NewSSHService(keys *KeyService, roots ...func() (string, error)) (*SSHService, func()) {
	s := &SSHService{credentialProtector: secrets.CurrentUser{}, keys: keys, runs: make(map[string]*ownedSSH), trust: &sshclient.TrustStore{Path: func() (string, error) {
		root, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "Yaare", "known_hosts"), nil
	}}}
	if len(roots) > 0 {
		s.trust.Path = func() (string, error) { path, err := roots[0](); return filepath.Join(path, "known_hosts"), err }
	}
	return s, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, run := range s.runs {
			run.session.Close()
		}
	}
}

var sessionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func ConfigureSSHIdentities(s *SSHService, identities *IdentityService) { s.identities = identities }

func ConfigureSSHWorkspace(s *SSHService, application *app.Workspace) { s.workspace = application }

func (s *SSHService) Connect(ctx context.Context, request SSHConnectRequest, events *mygo.Channel[sshclient.Event]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.connectStream(ctx, request, events)
}
func (s *SSHService) connectStream(ctx context.Context, request SSHConnectRequest, events eventSink[sshclient.Event]) error {
	page := callerPage(ctx)
	if page == nil {
		return errors.New("请在桌面窗口中连接 SSH")
	}
	if !sessionIDPattern.MatchString(request.SessionID) || request.Attempt < 1 || request.Attempt > 1000000 {
		return errors.New("无效的会话请求")
	}

	fail := func(err error) error {
		var f *sshclient.Fault
		code, message := "CONNECTION_FAILED", "无法准备 SSH 连接。"
		if errors.As(err, &f) {
			code, message = f.Code, f.Message
		}
		_ = events.Send(sshclient.Event{Type: "failed", Revision: 1, Code: code, Message: message})
		return nil
	}
	if len(request.JumpHostIDs) > 8 {
		return fail(&sshclient.Fault{Code: "JUMP_LIMIT", Message: "最多使用 8 台跳板机。"})
	}
	if err := workspace.ValidateForwards(request.Forwards); err != nil {
		return fail(&sshclient.Fault{Code: "INVALID_FORWARD", Message: "请检查端口转发配置。"})
	}
	cfg, release, err := s.resolveSSHConfig(ctx, request)
	request.Password = ""
	request.Passphrase = ""
	request.PrivateKey = ""
	if err != nil {
		return fail(err)
	}
	defer release()
	defer cfg.ClearSecrets()
	cfg.Forwards = request.Forwards
	if len(request.JumpHostIDs) > 0 {
		if s.workspace == nil {
			return fail(&sshclient.Fault{Code: "JUMP_UNAVAILABLE", Message: "主机配置暂不可用。"})
		}
		seen := map[string]bool{}
		for _, id := range request.JumpHostIDs {
			if seen[id] {
				return fail(&sshclient.Fault{Code: "JUMP_UNAVAILABLE", Message: "跳板机不存在或链路包含重复主机。"})
			}
			host, _, ok, err := s.workspace.Host(ctx, id)
			if err != nil {
				return fail(&sshclient.Fault{Code: "JUMP_UNAVAILABLE", Message: "无法读取跳板机配置。"})
			}
			if !ok {
				return fail(&sshclient.Fault{Code: "JUMP_UNAVAILABLE", Message: "跳板机不存在或链路包含重复主机。"})
			}
			seen[id] = true
			seconds := host.TimeoutSeconds
			if seconds == 0 {
				seconds = 15
			}
			hop, releaseHop, e := s.resolveSSHConfig(ctx, SSHConnectRequest{HostID: host.ID, UseSavedCredential: host.HasCredential, Name: host.Name, Address: host.Address, Port: host.Port, Username: host.Username, Authentication: host.Authentication, KeyID: host.KeyID, UseAgent: host.UseAgent, IdentityID: host.IdentityID, TimeoutSeconds: seconds})
			if e != nil {
				return fail(e)
			}
			defer releaseHop()
			cfg.Jumps = append(cfg.Jumps, hop)
		}
	}
	profileID := request.HostID
	if !sessionIDPattern.MatchString(profileID) {
		profileID = ""
	}
	run := &ownedSSH{page: page, attempt: request.Attempt, name: cfg.Name, profileID: profileID, systemRoute: activeSystemRoute(request, cfg)}
	run.session = sshclient.NewSession(ctx, s.trust, func(event sshclient.Event) error {
		if event.Type == "output" {
			if recorder := run.record.Load(); recorder != nil {
				recorder.Capture(event.Data)
			}
		}
		return events.Send(event)
	})
	s.mu.Lock()
	old := s.runs[request.SessionID]
	if (old != nil && (old.page != page || old.attempt >= request.Attempt)) || (old == nil && len(s.runs) >= 16) {
		s.mu.Unlock()
		run.session.Close()
		return errors.New("会话正在结束或已达到 16 个连接上限，请稍后重试")
	}
	if old != nil {
		old.session.Close()
	}
	s.runs[request.SessionID] = run
	s.mu.Unlock()
	defer func() {
		run.logMu.Lock()
		if recorder := run.record.Swap(nil); recorder != nil {
			budget, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = recorder.Stop(budget, "")
			cancel()
		}
		run.logMu.Unlock()
		s.mu.Lock()
		if s.runs[request.SessionID] == run {
			delete(s.runs, request.SessionID)
		}
		s.mu.Unlock()
	}()
	run.session.Run(&cfg)
	return nil
}

func (s *SSHService) owned(ctx context.Context, id string, attempt int) *sshclient.Session {
	run := s.ownedRun(ctx, id, attempt)
	if run == nil {
		return nil
	}
	return run.session
}

func (s *SSHService) ownedRun(ctx context.Context, id string, attempt int) *ownedSSH {
	page := callerPage(ctx)
	if page == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil || run.page != page || run.attempt != attempt {
		return nil
	}
	return run
}

func (s *SSHService) Write(ctx context.Context, id string, attempt int, data string) bool {
	if len(data) > 90000 {
		return false
	}
	run := s.owned(ctx, id, attempt)
	if run == nil {
		return false
	}
	bytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return false
	}
	defer clear(bytes)
	return run.Write(bytes)
}

func (s *SSHService) Resize(ctx context.Context, id string, attempt, columns, rows int) bool {
	run := s.owned(ctx, id, attempt)
	return run != nil && run.Resize(columns, rows)
}

func (s *SSHService) ConfirmHost(ctx context.Context, id string, attempt int, challengeID, decision string) bool {
	run := s.owned(ctx, id, attempt)
	return run != nil && run.Resolve(challengeID, decision)
}

func (s *SSHService) AnswerAuthentication(ctx context.Context, id string, attempt int, challengeID string, answers []string) bool {
	defer clear(answers)
	run := s.owned(ctx, id, attempt)
	return run != nil && run.AnswerAuthentication(challengeID, answers)
}

func (s *SSHService) Disconnect(ctx context.Context, id string, attempt int) {
	if run := s.owned(ctx, id, attempt); run != nil {
		run.Close()
	}
}

func (s *SSHService) ChangeDirectory(ctx context.Context, id string, attempt int, path string) error {
	run := s.owned(ctx, id, attempt)
	if run == nil {
		return errors.New("SSH 会话已结束或不可用。")
	}
	return run.ChangeDirectory(path)
}
