//go:build !windows && !linux && !darwin

package secrets

import "errors"

func Protect([]byte, string) ([]byte, error) {
	return nil, errors.New("当前平台不支持保存凭据。")
}
func Unprotect([]byte, string) ([]byte, error) {
	return nil, errors.New("当前平台不支持保存凭据。")
}
func beginCurrentUserScope() (Protector, func()) { return CurrentUser{}, func() {} }
