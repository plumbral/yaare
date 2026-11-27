package secrets

import (
	"crypto/rand"
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	macService          = "app.yaare"
	macAccount          = "credential-master-v1"
	itemNotFound  int32 = -25300
	duplicateItem int32 = -25299
)

var (
	errKeychainAccess  = errors.New("无法访问 macOS 钥匙串。请检查当前账户的钥匙串与应用访问授权。")
	errKeychainLocked  = errors.New("macOS 钥匙串已锁定。请解锁后重试。")
	errKeychainMissing = errors.New("原 macOS 钥匙串密钥不存在。请恢复原钥匙串或导入加密配置。")
	macRecord          = recordFormat{"YAM1", "Yaare macOS credential v1:", errors.New("macOS 凭据无法解密。请检查原账户的钥匙串或导入加密配置。")}
)

type keychainAPI struct {
	copyDefault func(*uintptr) int32
	getStatus   func(uintptr, *uint32) int32
	find        func(uintptr, uint32, string, uint32, string, *uint32, *unsafe.Pointer, uintptr) int32
	add         func(uintptr, uint32, string, uint32, string, uint32, *byte, uintptr) int32
	freeContent func(uintptr, unsafe.Pointer) int32
	release     func(uintptr)
}

var macKeychain = sync.OnceValues(func() (*keychainAPI, error) {
	security, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, errKeychainAccess
	}
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, errKeychainAccess
	}
	api := new(keychainAPI)
	for _, entry := range []struct {
		lib  uintptr
		name string
		dst  any
	}{
		{security, "SecKeychainCopyDefault", &api.copyDefault},
		{security, "SecKeychainGetStatus", &api.getStatus},
		{security, "SecKeychainFindGenericPassword", &api.find},
		{security, "SecKeychainAddGenericPassword", &api.add},
		{security, "SecKeychainItemFreeContent", &api.freeContent},
		{cf, "CFRelease", &api.release},
	} {
		addr, e := purego.Dlsym(entry.lib, entry.name)
		if e != nil || addr == 0 {
			return nil, errKeychainAccess
		}
		purego.RegisterFunc(entry.dst, addr)
	}
	return api, nil
})

func (api *keychainAPI) read(chain uintptr) ([]byte, error) {
	var length uint32
	var data unsafe.Pointer
	switch api.find(chain, uint32(len(macService)), macService, uint32(len(macAccount)), macAccount, &length, &data, 0) {
	case itemNotFound:
		return nil, errKeychainMissing
	case 0:
		if data == nil {
			return nil, macRecord.invalid
		}
		defer api.freeContent(0, data)
		if length != 32 {
			return nil, macRecord.invalid
		}
		return append([]byte(nil), unsafe.Slice((*byte)(data), 32)...), nil
	default:
		return nil, errKeychainAccess
	}
}

func (api *keychainAPI) masterKey(chain uintptr, create bool) ([]byte, error) {
	var flags uint32
	if api.getStatus(chain, &flags) != 0 {
		return nil, errKeychainAccess
	}
	if flags&1 == 0 {
		return nil, errKeychainLocked
	}
	key, err := api.read(chain)
	if !create || !errors.Is(err, errKeychainMissing) {
		return key, err
	}
	candidate := make([]byte, 32)
	if _, err = rand.Read(candidate); err != nil {
		return nil, err
	}
	switch api.add(chain, uint32(len(macService)), macService, uint32(len(macAccount)), macAccount, 32, &candidate[0], 0) {
	case 0:
		return candidate, nil
	case duplicateItem:
		clear(candidate)
		return api.read(chain)
	default:
		clear(candidate)
		return nil, errKeychainAccess
	}
}

func macMasterKey(create bool) ([]byte, error) {
	api, err := macKeychain()
	if err != nil {
		return nil, err
	}
	var chain uintptr
	if api.copyDefault(&chain) != 0 || chain == 0 {
		return nil, errKeychainAccess
	}
	defer api.release(chain)
	return api.masterKey(chain, create)
}

func Protect(data []byte, id string) ([]byte, error) {
	scope, release := beginCurrentUserScope()
	defer release()
	return scope.Protect(data, id)
}

func Unprotect(data []byte, id string) ([]byte, error) {
	scope, release := beginCurrentUserScope()
	defer release()
	return scope.Unprotect(data, id)
}

func beginCurrentUserScope() (Protector, func()) {
	scope := &recordScope{format: macRecord, loadKey: macMasterKey}
	return scope, scope.release
}
