//go:build linux

package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretDestination     = "org.freedesktop.secrets"
	secretService         = "org.freedesktop.Secret.Service"
	secretItem            = "org.freedesktop.Secret.Item"
	linuxCredentialHeader = "SCL1"
)

var (
	errLinuxKeyring    = errors.New("Linux 系统密钥环不可用。请启动支持 Secret Service 的系统密钥环后重试；也可以使用本次手动认证。")
	errLinuxLocked     = errors.New("Linux 系统密钥环已锁定。请在系统中解锁密钥环后重试。")
	errLinuxMissing    = errors.New("Linux 凭据密钥不存在。请在原 Linux 账户中解锁密钥环，或重新保存认证信息。")
	errLinuxCredential = errors.New("Linux 凭据无法解锁。请使用原账户的系统密钥环，或通过加密配置导入迁移资料。")
)

func Protect(data []byte, id string) ([]byte, error) {
	if len(data) == 0 || len(data) > 2<<20 {
		return nil, errors.New("凭证内容无效。")
	}
	key, err := linuxMasterKey(true)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return sealLinuxCredential(data, id, key)
}

func Unprotect(data []byte, id string) ([]byte, error) {
	if len(data) < 4+12+16 || len(data) > 3<<20 || string(data[:4]) != linuxCredentialHeader {
		return nil, errLinuxCredential
	}
	key, err := linuxMasterKey(false)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return openLinuxCredential(data, id, key)
}

type linuxScopedProtector struct {
	mu     sync.Mutex
	key    []byte
	closed bool
}

func beginCurrentUserScope() (Protector, func()) {
	scope := &linuxScopedProtector{}
	return scope, scope.release
}

func (s *linuxScopedProtector) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key != nil {
		clear(s.key)
		s.key = nil
	}
	s.closed = true
}

func (s *linuxScopedProtector) acquireKey(create bool) ([32]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [32]byte
	if s.closed {
		return out, errLinuxCredential
	}
	if len(s.key) != 32 {
		key, err := linuxMasterKey(create)
		if err != nil {
			return out, err
		}
		s.key = key
	}
	copy(out[:], s.key)
	return out, nil
}

func (s *linuxScopedProtector) Protect(data []byte, id string) ([]byte, error) {
	if len(data) == 0 || len(data) > 2<<20 {
		return nil, errors.New("凭证内容无效。")
	}
	key, err := s.acquireKey(true)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	return sealLinuxCredential(data, id, key[:])
}

func (s *linuxScopedProtector) Unprotect(data []byte, id string) ([]byte, error) {
	if len(data) < 4+12+16 || len(data) > 3<<20 || string(data[:4]) != linuxCredentialHeader {
		return nil, errLinuxCredential
	}
	key, err := s.acquireKey(false)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	return openLinuxCredential(data, id, key[:])
}

func linuxCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errLinuxCredential
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errLinuxCredential
	}
	return cipher.NewGCM(block)
}
func sealLinuxCredential(data []byte, id string, key []byte) ([]byte, error) {
	aead, err := linuxCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, errLinuxCredential
	}
	return aead.Seal(append([]byte(linuxCredentialHeader), nonce...), nonce, data, []byte("Yaare Linux credential v1:"+id)), nil
}
func openLinuxCredential(data []byte, id string, key []byte) ([]byte, error) {
	aead, err := linuxCipher(key)
	if err != nil {
		return nil, err
	}
	n := aead.NonceSize()
	if len(data) < 4+n+aead.Overhead() || string(data[:4]) != linuxCredentialHeader {
		return nil, errLinuxCredential
	}
	plain, err := aead.Open(nil, data[4:4+n], data[4+n:], []byte("Yaare Linux credential v1:"+id))
	if err != nil {
		return nil, errLinuxCredential
	}
	return plain, nil
}

type keyringSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func linuxMasterKey(create bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, errLinuxKeyring
	}
	defer conn.Close()
	service := conn.Object(secretDestination, "/org/freedesktop/secrets")
	var ignored dbus.Variant
	var session dbus.ObjectPath
	if err = service.CallWithContext(ctx, secretService+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&ignored, &session); err != nil {
		return nil, errLinuxKeyring
	}
	defer conn.Object(secretDestination, session).CallWithContext(ctx, "org.freedesktop.Secret.Session.Close", 0)
	attributes := map[string]string{"application": "app.yaare", "purpose": "credential-master-v1"}
	find := func() ([]byte, error) {
		var unlocked, locked []dbus.ObjectPath
		if err := service.CallWithContext(ctx, secretService+".SearchItems", 0, attributes).Store(&unlocked, &locked); err != nil {
			return nil, errLinuxKeyring
		}
		if len(locked) != 0 {
			return nil, errLinuxLocked
		}
		if len(unlocked) == 0 {
			return nil, errLinuxMissing
		}
		if len(unlocked) != 1 {
			return nil, errLinuxCredential
		}
		var secret keyringSecret
		if err := conn.Object(secretDestination, unlocked[0]).CallWithContext(ctx, secretItem+".GetSecret", 0, session).Store(&secret); err != nil {
			return nil, errLinuxLocked
		}
		if len(secret.Value) != 32 {
			clear(secret.Value)
			return nil, errLinuxCredential
		}
		return secret.Value, nil
	}
	key, err := find()
	if !errors.Is(err, errLinuxMissing) || !create {
		return key, err
	}
	for {
		var result uint32
		if err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.RequestName", 0, "app.yaare.CredentialMaster", uint32(dbus.NameFlagDoNotQueue)).Store(&result); err != nil {
			return nil, errLinuxKeyring
		}
		if result == uint32(dbus.RequestNameReplyPrimaryOwner) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, errLinuxKeyring
		case <-time.After(20 * time.Millisecond):
		}
	}
	key, err = find()
	if !errors.Is(err, errLinuxMissing) {
		return key, err
	}
	var collection dbus.ObjectPath
	if err = service.CallWithContext(ctx, secretService+".ReadAlias", 0, "default").Store(&collection); err != nil || collection == "/" || collection == "" {
		return nil, errLinuxKeyring
	}
	object := conn.Object(secretDestination, collection)
	var locked dbus.Variant
	if err = object.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.Secret.Collection", "Locked").Store(&locked); err != nil {
		return nil, errLinuxKeyring
	}
	if isLocked, ok := locked.Value().(bool); !ok || isLocked {
		return nil, errLinuxLocked
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, errLinuxCredential
	}
	defer clear(key)
	properties := map[string]dbus.Variant{
		secretItem + ".Label":      dbus.MakeVariant("Yaare credential encryption key"),
		secretItem + ".Attributes": dbus.MakeVariant(attributes),
	}
	var item, prompt dbus.ObjectPath
	if err = object.CallWithContext(ctx, "org.freedesktop.Secret.Collection.CreateItem", 0, properties, keyringSecret{session, []byte{}, key, "application/octet-stream"}, true).Store(&item, &prompt); err != nil {
		return nil, errLinuxKeyring
	}
	if prompt != "/" {
		return nil, errLinuxLocked
	}
	return find()
}
