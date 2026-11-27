package secrets

import (
	"errors"
	"sync"
)

var ErrWorkspaceKey = errors.New("无法验证此工作区的凭据密钥。请解锁原系统钥匙串，或从加密备份恢复到新工作区。")

type Checked struct {
	Base  Protector
	Check func(Protector) error
}

func (c Checked) Protect(data []byte, purpose string) ([]byte, error) {
	scope, release := c.BeginScope()
	defer release()
	return scope.Protect(data, purpose)
}

func (c Checked) Unprotect(data []byte, purpose string) ([]byte, error) {
	return c.Base.Unprotect(data, purpose)
}

func (c Checked) BeginScope() (Protector, func()) {
	raw, release := WithScope(c.Base)
	return &checkedScope{Protector: raw, verify: sync.OnceValue(func() error { return c.Check(raw) })}, release
}

type checkedScope struct {
	Protector
	verify func() error
}

func (s *checkedScope) Protect(data []byte, purpose string) ([]byte, error) {
	if err := s.verify(); err != nil {
		return nil, err
	}
	return s.Protector.Protect(data, purpose)
}
