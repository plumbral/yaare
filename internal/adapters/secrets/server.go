package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

type recordFormat struct {
	header, context string
	invalid         error
}

var serverRecord = recordFormat{"YAW1", "Yaare server credential v1:", errors.New("服务端凭证无法解锁，请检查原实例密钥。")}

type recordVault struct {
	aead   cipher.AEAD
	format recordFormat
}

type ServerVault struct{ *recordVault }

func NewServerVault(key []byte) (*ServerVault, error) {
	if len(key) != 32 {
		return nil, errors.New("服务端密钥须为 32 字节。")
	}
	vault, e := newRecordVault(key, serverRecord)
	if e != nil {
		return nil, e
	}
	return &ServerVault{vault}, nil
}

func newRecordVault(key []byte, format recordFormat) (*recordVault, error) {
	if len(key) != 32 {
		return nil, format.invalid
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	return &recordVault{aead, format}, nil
}
func (v *recordVault) Protect(data []byte, id string) ([]byte, error) {
	if len(data) < 1 || len(data) > 2<<20 {
		return nil, errors.New("凭证内容无效。")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, e := io.ReadFull(rand.Reader, nonce); e != nil {
		return nil, e
	}
	out := append([]byte(v.format.header), nonce...)
	return v.aead.Seal(out, nonce, data, []byte(v.format.context+id)), nil
}
func (v *recordVault) Unprotect(data []byte, id string) ([]byte, error) {
	n := v.aead.NonceSize()
	h := len(v.format.header)
	if len(data) < h+n+v.aead.Overhead() || len(data) > 3<<20 || string(data[:h]) != v.format.header {
		return nil, v.format.invalid
	}
	raw, e := v.aead.Open(nil, data[h:h+n], data[h+n:], []byte(v.format.context+id))
	if e != nil {
		return nil, v.format.invalid
	}
	return raw, nil
}
