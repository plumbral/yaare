package secrets

import (
	"errors"
	"sync"
)

type recordScope struct {
	mu      sync.Mutex
	format  recordFormat
	loadKey func(create bool) ([]byte, error)
	key     []byte
	closed  bool
}

func (s *recordScope) vault(create bool) (*recordVault, error) {
	if s.closed {
		return nil, s.format.invalid
	}
	if s.key == nil {
		key, err := s.loadKey(create)
		if err != nil || len(key) != 32 {
			clear(key)
			if err != nil {
				return nil, err
			}
			return nil, s.format.invalid
		}
		s.key = key
	}
	return newRecordVault(s.key, s.format)
}

func (s *recordScope) Protect(data []byte, id string) ([]byte, error) {
	if len(data) == 0 || len(data) > 2<<20 {
		return nil, errors.New("凭证内容无效。")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.vault(true)
	if err != nil {
		return nil, err
	}
	return v.Protect(data, id)
}

func (s *recordScope) Unprotect(data []byte, id string) ([]byte, error) {
	h := len(s.format.header)
	if len(data) < h+12+16 || len(data) > 3<<20 || string(data[:h]) != s.format.header {
		return nil, s.format.invalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.vault(false)
	if err != nil {
		return nil, err
	}
	return v.Unprotect(data, id)
}

func (s *recordScope) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.key)
	s.key = nil
	s.closed = true
}
