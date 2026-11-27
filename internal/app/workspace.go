package app

import (
	"context"
	"yaare/internal/domain/workspace"
	"yaare/internal/ports"
)

type Workspace struct{ repository ports.WorkspaceRepository }

func NewWorkspace(repository ports.WorkspaceRepository) *Workspace {
	return &Workspace{repository: repository}
}
func (s *Workspace) Load(ctx context.Context) workspace.Report  { return s.repository.Load(ctx) }
func (s *Workspace) Retry(ctx context.Context) workspace.Report { return s.repository.Retry(ctx) }
func (s *Workspace) Recover(ctx context.Context) (workspace.Report, error) {
	return s.repository.Recover(ctx)
}
func (s *Workspace) DeletedHosts(ctx context.Context) ([]workspace.DeletedHost, error) {
	return s.repository.DeletedHosts(ctx)
}
func (s *Workspace) RestoreDeletedHost(ctx context.Context, backupID, hostID string, revision int64) (workspace.Result, error) {
	return s.repository.RestoreDeletedHost(ctx, backupID, hostID, revision)
}
func (s *Workspace) Apply(ctx context.Context, change workspace.Change) (workspace.Result, error) {
	return s.repository.ApplyChange(ctx, change, nil)
}
func (s *Workspace) SaveConnection(ctx context.Context, change workspace.Change, c workspace.HostCredentialChange) (workspace.Result, error) {
	if change.Kind != "saveHost" || change.Host.ID != c.HostID {
		return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
	}
	return s.repository.ApplyChange(ctx, change, &c)
}

func (s *Workspace) Host(ctx context.Context, id string) (workspace.Host, int64, bool, error) {
	return s.repository.Host(ctx, id)
}

func (s *Workspace) HostCredential(ctx context.Context, id string) (workspace.Host, workspace.HostCredential, error) {
	h, _, found, err := s.Host(ctx, id)
	if err != nil {
		return workspace.Host{}, workspace.HostCredential{}, err
	}
	if found && h.HasCredential {
		credential, err := s.repository.HostCredential(ctx, id)
		if err != nil {
			return workspace.Host{}, workspace.HostCredential{}, err
		}
		if workspace.CredentialMatches(h, credential) {
			return h, credential, nil
		}
		return workspace.Host{}, workspace.HostCredential{}, workspace.Fail("CREDENTIAL_UNAVAILABLE", "此连接的已保存认证已变化，请重新打开连接。")
	}
	return workspace.Host{}, workspace.HostCredential{}, workspace.Fail("CREDENTIAL_UNAVAILABLE", "此连接未保存密码，请重新输入。")
}

func (s *Workspace) ProxyCredential(ctx context.Context, id string) (workspace.HostCredential, error) {
	return s.repository.ProxyCredential(ctx, id)
}
