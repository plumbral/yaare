package ipc

import (
	"context"

	"yaare/internal/adapters/secrets"
	"yaare/internal/app"
	"yaare/internal/app/vault"
	"yaare/internal/domain/workspace"
)

type WorkspaceService struct {
	application *app.Workspace
	protector   secrets.Protector
	keys        *KeyService
}

func NewWorkspaceService(application *app.Workspace, protectors ...secrets.Protector) *WorkspaceService {
	p := secrets.Protector(secrets.CurrentUser{})
	if len(protectors) > 0 && protectors[0] != nil {
		p = protectors[0]
	}
	return &WorkspaceService{application: application, protector: p}
}

func ConfigureWorkspaceKeys(s *WorkspaceService, keys *KeyService) { s.keys = keys }

type SaveConnectionRequest struct {
	Revision          int64          `json:"revision"`
	Host              workspace.Host `json:"host"`
	RememberPassword  bool           `json:"rememberPassword"`
	Password          string         `json:"password"`
	PrivateKey        string         `json:"privateKey,omitempty"`
	ProxyPassword     string         `json:"proxyPassword,omitempty"`
	GeneratedKeyToken string         `json:"generatedKeyToken,omitempty"`
	KeyToken          string         `json:"keyToken,omitempty"`
	Passphrase        string         `json:"passphrase,omitempty"`
}

func (s *WorkspaceService) SaveConnection(ctx context.Context, r SaveConnectionRequest) (workspace.Result, error) {
	if e := workspaceCaller(ctx); e != nil {
		return workspace.Result{}, e
	}
	if len(r.Password) > 16384 || len(r.Passphrase) > 16384 || len(r.PrivateKey) > 1<<20 {
		return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "密码过长，请检查输入。")
	}
	if len(r.ProxyPassword) > 4096 {
		return workspace.Result{}, workspace.Fail("PROXY_AUTH_INVALID", "代理密码长度无效。")
	}
	input := vault.ConnectionInput{RememberPassword: r.RememberPassword, Password: []byte(r.Password), PrivateKey: []byte(r.PrivateKey),
		Passphrase: []byte(r.Passphrase), ProxyPassword: []byte(r.ProxyPassword)}
	defer clear(input.Password)
	defer clear(input.PrivateKey)
	defer clear(input.Passphrase)
	defer clear(input.ProxyPassword)
	if r.GeneratedKeyToken != "" {
		if s.keys == nil || r.PrivateKey != "" || r.KeyToken != "" || r.Passphrase != "" {
			return workspace.Result{}, workspace.Fail("KEY_INVALID", "保存认证请求无效。")
		}
		material, err := s.keys.generatedPrivateKey(ctx, r.GeneratedKeyToken)
		if err != nil {
			return workspace.Result{}, err
		}
		defer clear(material)
		input.PrivateKey = material
	}
	if r.KeyToken != "" {
		if s.keys == nil || r.PrivateKey != "" {
			return workspace.Result{}, workspace.Fail("KEY_INVALID", "保存认证请求无效。")
		}
		input.KeyPath = s.keys.selectedFile(ctx, r.KeyToken)
		if input.KeyPath == "" {
			return workspace.Result{}, workspace.Fail("KEY_UNAVAILABLE", "请选择私钥文件，原选择可能已过期。")
		}
	}
	h, c, err := vault.PrepareConnection(s.protector, r.Host, input)
	if err != nil {
		return workspace.Result{}, err
	}
	result, err := s.application.SaveConnection(ctx, workspace.Change{Revision: r.Revision, Kind: "saveHost", Host: h}, c)
	if err == nil {
		if r.KeyToken != "" {
			s.keys.Discard(ctx, r.KeyToken)
		}
		if r.GeneratedKeyToken != "" {
			s.keys.Discard(ctx, r.GeneratedKeyToken)
		}
	}
	return result, err
}
func workspaceCaller(ctx context.Context) error {
	if callerPage(ctx) == nil {
		return workspace.Fail("DESKTOP_REQUIRED", "请在桌面窗口中管理本机配置。")
	}
	return nil
}
func (s *WorkspaceService) ServerSystems(ctx context.Context) (map[string]workspace.ServerSystem, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	return s.application.ServerSystems(ctx)
}

func (s *WorkspaceService) Load(ctx context.Context) (workspace.Report, error) {
	if err := workspaceCaller(ctx); err != nil {
		return workspace.Report{}, err
	}
	return s.application.Load(ctx), nil
}
func (s *WorkspaceService) Apply(ctx context.Context, change workspace.Change) (workspace.Result, error) {
	if err := workspaceCaller(ctx); err != nil {
		return workspace.Result{}, err
	}
	return s.application.Apply(ctx, change)
}
func (s *WorkspaceService) Retry(ctx context.Context) (workspace.Report, error) {
	if err := workspaceCaller(ctx); err != nil {
		return workspace.Report{}, err
	}
	return s.application.Retry(ctx), nil
}
func (s *WorkspaceService) Recover(ctx context.Context) (workspace.Report, error) {
	if err := workspaceCaller(ctx); err != nil {
		return workspace.Report{}, err
	}
	return s.application.Recover(ctx)
}

func (s *WorkspaceService) DeletedHosts(ctx context.Context) ([]workspace.DeletedHost, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	return s.application.DeletedHosts(ctx)
}

func (s *WorkspaceService) RestoreDeletedHost(ctx context.Context, backupID, hostID string, revision int64) (workspace.Result, error) {
	if err := workspaceCaller(ctx); err != nil {
		return workspace.Result{}, err
	}
	return s.application.RestoreDeletedHost(ctx, backupID, hostID, revision)
}
