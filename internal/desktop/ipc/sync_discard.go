package ipc

import (
	"context"
)

func (s *FileService) DiscardSync(ctx context.Context, id string) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.syncIntents[id]; ok && p.page == callerPage(ctx) {
		delete(s.syncIntents, id)
	}
	return nil
}
