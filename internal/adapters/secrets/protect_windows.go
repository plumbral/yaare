//go:build windows

package secrets

import (
	"errors"
	"golang.org/x/sys/windows"
	"unsafe"
)

func crypt(data []byte, id string, decrypt bool) ([]byte, error) {
	if len(data) == 0 || len(data) > 2*1024*1024 {
		return nil, errors.New("凭证内容无效。")
	}
	entropy := []byte("Yaare identity v1:" + id)
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	extra := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&input, nil, &extra, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&input, nil, &extra, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, errors.New("Windows 无法保护或解锁此凭证，请在原 Windows 账户中重试。")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	bytes := unsafe.Slice(out.Data, int(out.Size))
	result := append([]byte(nil), bytes...)
	clear(bytes)
	return result, nil
}
func Protect(data []byte, id string) ([]byte, error)   { return crypt(data, id, false) }
func Unprotect(data []byte, id string) ([]byte, error) { return crypt(data, id, true) }
func beginCurrentUserScope() (Protector, func())       { return CurrentUser{}, func() {} }
