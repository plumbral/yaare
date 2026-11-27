package cloudsync

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	MaxBlob        = 24 << 20
	MaxPlaintext   = 20 << 20
	magic          = "YAARE2\x00\x00"
	legacyProfile  = 1
	argonProfile   = 2
	bucketSize     = 4096
	tagSize        = 16
	archivePurpose = "archive"
	saltOffset     = len(magic) + 1
	nonceOffset    = saltOffset + 16
	headerSize     = nonceOffset + 12
)

func ValidPassword[P ~string | ~[]byte](pass P) bool {
	return len(pass) <= 1024 && utf8.RuneCount([]byte(pass)) >= 12
}

func ValidEnvelope(blob []byte) bool {
	if len(blob) < headerSize+tagSize || len(blob) > MaxBlob || string(blob[:len(magic)]) != magic {
		return false
	}
	switch blob[len(magic)] {
	case legacyProfile:
		return true
	case argonProfile:
		return len(blob)%bucketSize == 0
	default:
		return false
	}
}

func Legacy(blob []byte) bool { return ValidEnvelope(blob) && blob[len(magic)] == legacyProfile }

func derive(pass []byte, profile byte, salt []byte) []byte {
	if profile == legacyProfile {
		return argon2.IDKey(pass, salt, 2, 32*1024, 1, 32)
	}
	return argon2.IDKey(pass, salt, 3, 64*1024, 2, 32)
}

func compressPlaintext(plain []byte) ([]byte, error) {
	if len(plain) > MaxPlaintext {
		return nil, errors.New("同步密码至少 12 个字符，配置最多 20 MiB。")
	}
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err = gz.Write(plain); err != nil {
		_ = gz.Close()
		clear(buf.Bytes())
		return nil, err
	}
	if err = gz.Close(); err != nil {
		clear(buf.Bytes())
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompressPlaintext(compressed []byte) ([]byte, error) {
	reader := bytes.NewReader(compressed)
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return nil, err
	}
	gz.Multistream(false)
	plain, err := io.ReadAll(io.LimitReader(gz, MaxPlaintext+1))
	closeErr := gz.Close()
	if err != nil || closeErr != nil || len(plain) > MaxPlaintext || reader.Len() != 0 {
		clear(plain)
		return nil, errors.New("云端加密数据格式无效。")
	}
	return plain, nil
}

var errOpen = errors.New("同步密码不正确或云端数据已损坏；本机资料未修改。")

type Sealer struct {
	salt [16]byte
	key  []byte
}

func NewSealer(pass []byte, keys *KeyCache) (*Sealer, error) {
	if !ValidPassword(pass) {
		return nil, errors.New("同步密码至少 12 个字符，最多 1 KiB。")
	}
	var s Sealer
	if _, err := rand.Read(s.salt[:]); err != nil {
		return nil, err
	}
	s.key = keys.key(pass, argonProfile, s.salt)
	return &s, nil
}

func (s *Sealer) Close() { clear(s.key); s.key = nil }

func paddedSize(compressed int) int {
	return (headerSize + tagSize + 4 + compressed + bucketSize - 1) / bucketSize * bucketSize
}

func (s *Sealer) Seal(plain []byte, purpose string) ([]byte, error) {
	if len(s.key) != 32 || purpose == "" {
		return nil, errors.New("加密配置上下文无效。")
	}
	compressed, err := compressPlaintext(plain)
	if err != nil {
		return nil, err
	}
	defer clear(compressed)
	size := paddedSize(len(compressed))
	if size > MaxBlob {
		return nil, errors.New("加密配置超过大小限制。")
	}
	header := make([]byte, headerSize, size)
	copy(header, magic)
	header[len(magic)] = argonProfile
	copy(header[saltOffset:], s.salt[:])
	nonce := header[nonceOffset:headerSize]
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	body := make([]byte, size-headerSize-tagSize)
	defer clear(body)
	binary.BigEndian.PutUint32(body, uint32(len(compressed)))
	copy(body[4:], compressed)
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	aad := append(header[:headerSize:headerSize], purpose...)
	return aead.Seal(header, nonce, body, aad), nil
}

func OpenObject(blob, pass []byte, purpose string, keys *KeyCache) ([]byte, error) {
	if !ValidEnvelope(blob) || !ValidPassword(pass) || purpose == "" {
		return nil, errors.New("云端加密数据格式无效。")
	}
	profile := blob[len(magic)]
	key := keys.key(pass, profile, [16]byte(blob[saltOffset:nonceOffset]))
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	aad := blob[:headerSize:headerSize]
	if profile == argonProfile {
		aad = append(aad, purpose...)
	}
	body, err := aead.Open(nil, blob[nonceOffset:headerSize], blob[headerSize:], aad)
	if err != nil {
		return nil, errOpen
	}
	defer clear(body)
	compressed := body
	if profile == argonProfile {
		n := binary.BigEndian.Uint32(body)
		if n > uint32(len(body)-4) || paddedSize(int(n)) != len(blob) {
			return nil, errOpen
		}
		compressed = body[4 : 4+n]
	}
	plain, err := decompressPlaintext(compressed)
	if err != nil {
		return nil, errOpen
	}
	return plain, nil
}

func Seal[P ~string | ~[]byte](plain []byte, pass P) ([]byte, error) {
	if len(plain) > MaxPlaintext {
		return nil, errors.New("配置最多 20 MiB。")
	}
	s, err := NewSealer([]byte(pass), nil)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.Seal(plain, archivePurpose)
}

func Open[P ~string | ~[]byte](blob []byte, pass P) ([]byte, error) {
	return OpenObject(blob, []byte(pass), archivePurpose, nil)
}
