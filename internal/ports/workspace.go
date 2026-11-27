package ports

import (
	"context"
	"yaare/internal/domain/workspace"
)

type WorkspaceRepository interface {
	Load(context.Context) workspace.Report
	ApplyChange(context.Context, workspace.Change, *workspace.HostCredentialChange) (workspace.Result, error)
	Host(context.Context, string) (workspace.Host, int64, bool, error)
	HostCredential(context.Context, string) (workspace.HostCredential, error)
	ProxyCredential(context.Context, string) (workspace.HostCredential, error)
	ServerSystems(context.Context) (map[string]workspace.ServerSystem, error)
	SaveServerSystem(context.Context, string, string, int64, workspace.ServerSystem) error
	DeletedHosts(context.Context) ([]workspace.DeletedHost, error)
	RestoreDeletedHost(context.Context, string, string, int64) (workspace.Result, error)
	Retry(context.Context) workspace.Report
	Recover(context.Context) (workspace.Report, error)
}
