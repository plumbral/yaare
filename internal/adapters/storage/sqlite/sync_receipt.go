package sqlite

import "path/filepath"

func (s *Store) SyncReceiptPath() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", s.unavailable()
	}
	return filepath.Join(s.dir, "sync-publication.pending"), nil
}
