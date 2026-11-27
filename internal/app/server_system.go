package app

import (
	"context"
	"yaare/internal/domain/workspace"
)

func (s *Workspace) ServerSystems(ctx context.Context) (map[string]workspace.ServerSystem, error) {
	return s.repository.ServerSystems(ctx)
}

func (s *Workspace) SaveServerSystem(ctx context.Context, id, route string, started int64, info workspace.ServerSystem) error {
	return s.repository.SaveServerSystem(ctx, id, route, started, info)
}
