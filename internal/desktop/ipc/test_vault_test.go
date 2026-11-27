package ipc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

type testVault struct{ aead cipher.AEAD }

func newTestVault(key []byte) (*testVault, error) {
	if len(key) != 32 {
		return nil, errors.New("服务端密钥须为 32 字节。")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	return &testVault{aead}, nil
}
func (v *testVault) Protect(data []byte, id string) ([]byte, error) {
	if len(data) < 1 || len(data) > 2<<20 {
		return nil, errors.New("凭证内容无效。")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, e := io.ReadFull(rand.Reader, nonce); e != nil {
		return nil, e
	}
	out := append([]byte("SCW1"), nonce...)
	return v.aead.Seal(out, nonce, data, []byte("Yaare test credential v1:"+id)), nil
}
func (v *testVault) Unprotect(data []byte, id string) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(data) < 4+n+v.aead.Overhead() || len(data) > 3<<20 || string(data[:4]) != "SCW1" {
		return nil, errors.New("测试凭证无法解锁。")
	}
	raw, e := v.aead.Open(nil, data[4:4+n], data[4+n:], []byte("Yaare test credential v1:"+id))
	if e != nil {
		return nil, errors.New("测试凭证无法解锁。")
	}
	return raw, nil
}
