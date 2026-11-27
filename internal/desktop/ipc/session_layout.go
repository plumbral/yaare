package ipc

import (
	"context"
	"yaare/internal/domain/workspace"
)

type sessionLayoutRepository interface {
	SessionLayout(context.Context) (workspace.SessionLayout, error)
	SaveSessionLayout(context.Context, workspace.SessionLayout) (workspace.SessionLayout, error)
}
type SessionLayoutService struct{ repository sessionLayoutRepository }

func NewSessionLayoutService(r sessionLayoutRepository) *SessionLayoutService {
	return &SessionLayoutService{r}
}
func (s *SessionLayoutService) Load(ctx context.Context) (workspace.SessionLayout, error) {
	if err := workspaceCaller(ctx); err != nil {
		return workspace.SessionLayout{}, err
	}
	return s.repository.SessionLayout(ctx)
}
func (s *SessionLayoutService) Save(ctx context.Context, next workspace.SessionLayout) (workspace.SessionLayout, error) {
	if err := workspaceCaller(ctx); err != nil {
		return workspace.SessionLayout{}, err
	}
	return s.repository.SaveSessionLayout(ctx, next)
}
