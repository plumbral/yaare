package secrets

type Protector interface {
	Protect([]byte, string) ([]byte, error)
	Unprotect([]byte, string) ([]byte, error)
}

type ScopedProtector interface {
	BeginScope() (Protector, func())
}

func WithScope(p Protector) (Protector, func()) {
	if scoped, ok := p.(ScopedProtector); ok {
		return scoped.BeginScope()
	}
	return p, func() {}
}

type CurrentUser struct{}

func (CurrentUser) Protect(data []byte, id string) ([]byte, error)   { return Protect(data, id) }
func (CurrentUser) Unprotect(data []byte, id string) ([]byte, error) { return Unprotect(data, id) }
func (CurrentUser) BeginScope() (Protector, func())                  { return beginCurrentUserScope() }
