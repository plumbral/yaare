package sqlite

import (
	"context"

	"yaare/internal/adapters/cloudsync"
)

func (s *Store) PreservePortable(ctx context.Context, blob []byte) error {
	_, err := s.saveRestoreCopy(ctx, blob, false)
	return err
}

func (s *Store) PreserveBeforeImport(ctx context.Context, blob []byte) (string, error) {
	return s.saveRestoreCopy(ctx, blob, true)
}

func (s *Store) ReadBeforeImport(id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, s.unavailable()
	}
	return cloudsync.ReadImportRestoreCopy(s.dir, id)
}

func (s *Store) saveRestoreCopy(ctx context.Context, blob []byte, beforeImport bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", s.unavailable()
	}
	return cloudsync.SaveRestoreCopy(ctx, s.dir, blob, s.now(), beforeImport)
}
