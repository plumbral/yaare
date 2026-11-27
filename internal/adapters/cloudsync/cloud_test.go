package cloudsync

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"strings"
	"testing"
)

func TestEncryptionTamperWrongPasswordAndRandomness(t *testing.T) {
	plain := []byte("password and private key test material")
	password := "Only-A-Test-Passphrase!"
	a, e := Seal(plain, password)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := Seal(plain, password)
	if bytes.Equal(a, b) || bytes.Contains(a, plain) {
		t.Fatal("ciphertext exposed plaintext or reused nonce")
	}
	opened, e := Open(a, password)
	if e != nil || !bytes.Equal(opened, plain) {
		t.Fatal(e)
	}
	if _, e = Open(a, "Wrong-Password-Only!"); e == nil {
		t.Fatal("wrong password accepted")
	}
	a[len(a)-1] ^= 1
	if _, e = Open(a, password); e == nil {
		t.Fatal("tamper accepted")
	}
}

func TestEnvelopeRejectsUnknownFormatAndKDFBeforeDerivation(t *testing.T) {
	const password = "Only-A-Test-Passphrase!"
	sealed, err := Seal([]byte("test"), password)
	if err != nil || !ValidEnvelope(sealed) || ValidEnvelope(sealed[:headerSize]) {
		t.Fatal("V2 envelope structure not recognized", err)
	}
	foreign := bytes.Clone(sealed)
	copy(foreign, "OTHERFMT")
	if ValidEnvelope(foreign) {
		t.Fatal("unrecognized format accepted")
	}
	changedNonce := bytes.Clone(sealed)
	changedNonce[nonceOffset] ^= 1
	if _, err = Open(changedNonce, password); err == nil {
		t.Fatal("tampered header accepted")
	}
	sealed[len(magic)]++
	if ValidEnvelope(sealed) {
		t.Fatal("unrecognized KDF profile accepted")
	}
}

func TestEnvelopeGzipCompressionZipBombAndRunePassword(t *testing.T) {
	if ValidPassword("密码很短") {
		t.Fatal("4-rune UTF-8 password accepted by byte length")
	}
	if _, err := Seal([]byte("payload"), "密码很短"); err == nil {
		t.Fatal("Seal accepted 4-rune password")
	}
	unicodePass := "这是一个十二字符同步密码"
	if !ValidPassword(unicodePass) {
		t.Fatal("12-rune UTF-8 password rejected")
	}
	repetitive := bytes.Repeat([]byte(`{"host":"prod-web-01","notes":"repetitive config json"}`), 4096)
	sealed, err := Seal(repetitive, unicodePass)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) >= len(repetitive)/10 {
		t.Fatalf("expected gzip compression to shrink payload dramatically: plain=%d sealed=%d", len(repetitive), len(sealed))
	}
	opened, err := Open(sealed, unicodePass)
	if err != nil || !bytes.Equal(opened, repetitive) {
		t.Fatalf("compressed roundtrip failed: %v", err)
	}

	var bombBuf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&bombBuf, gzip.BestCompression)
	chunk := make([]byte, 1<<20)
	for range 21 {
		_, _ = gz.Write(chunk)
	}
	_ = gz.Close()
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key := derive([]byte(unicodePass), legacyProfile, salt)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	_, _ = rand.Read(nonce)
	header := append(append(append([]byte(magic), legacyProfile), salt...), nonce...)
	bombEnvelope := aead.Seal(header, nonce, bombBuf.Bytes(), header)
	if _, err := Open(bombEnvelope, unicodePass); err == nil {
		t.Fatal("decompression bomb exceeding MaxPlaintext was accepted")
	}

	compressed, err := compressPlaintext([]byte(strings.Repeat("x", 64)))
	if err != nil {
		t.Fatal(err)
	}
	trailingEnvelope := aead.Seal(bytes.Clone(header), nonce, append(compressed, 0x00), header)
	if _, err := Open(trailingEnvelope, unicodePass); err == nil {
		t.Fatal("trailing bytes after gzip stream were accepted")
	}
}

func TestPublicationBucketsPurposeAndKeyLifecycle(t *testing.T) {
	pass := []byte("publication fixture password")
	var keys KeyCache
	keys.Bind("fixture-epoch")
	defer keys.Clear()
	sealer, err := NewSealer(pass, &keys)
	if err != nil {
		t.Fatal(err)
	}
	defer sealer.Close()

	first, err := sealer.Seal([]byte("short"), "current.yaaresync")
	if err != nil {
		t.Fatal(err)
	}
	second, err := sealer.Seal(bytes.Repeat([]byte("different content"), 40), "backups/fixture")
	if err != nil {
		t.Fatal(err)
	}
	if first[len(magic)] != 2 || len(first) != bucketSize || len(first) != len(second) ||
		!bytes.Equal(first[saltOffset:nonceOffset], second[saltOffset:nonceOffset]) ||
		bytes.Equal(first[nonceOffset:headerSize], second[nonceOffset:headerSize]) {
		t.Fatal("related objects must share a KDF salt and size bucket and use separate nonces")
	}
	entry := keys.entries[0].key
	for _, name := range []string{"current.yaaresync", "archive", "backups/other"} {
		if _, err := OpenObject(second, pass, name, &keys); err == nil {
			t.Fatalf("accepted backup at %q", name)
		}
	}
	if _, err := OpenObject(second, pass, "backups/fixture", &keys); err != nil {
		t.Fatal(err)
	}

	if len(entry) != 32 || &entry[0] != &keys.entries[0].key[0] || keys.next != 1 {
		t.Fatal("expected cache hit on repeated same-salt opens")
	}
	keys.Bind("changed-password-epoch")
	if !bytes.Equal(entry, make([]byte, 32)) {
		t.Fatal("stale key survived password change")
	}
	if _, err := OpenObject(first, []byte("different test password"), "current.yaaresync", &keys); err == nil {
		t.Fatal("old password's key reused")
	}
	entry = keys.entries[0].key
	keys.Clear()
	if !bytes.Equal(entry, make([]byte, 32)) || keys.Bind("") != nil {
		t.Fatal("cache clear left a key accessible")
	}
	sealer.Close()
	if _, err := sealer.Seal(nil, "current.yaaresync"); err == nil {
		t.Fatal("closed sealer reused")
	}
}

func TestDerivedCacheEvictionClearsKey(t *testing.T) {
	pass := []byte("cache bound test password")
	var c KeyCache
	c.Bind("test")
	defer c.Clear()
	var evicted []byte
	for i := range len(c.entries) + 1 {
		salt := [16]byte{byte(i)}
		clear(c.key(pass, legacyProfile, salt))
		if i == 0 {
			evicted = c.entries[0].key
		}
	}
	if !bytes.Equal(evicted, make([]byte, 32)) || c.entries[0].salt[0] != byte(len(c.entries)) {
		t.Fatal("oldest cache slot not securely evicted")
	}
}

func TestPaddedPayloadBounds(t *testing.T) {
	pass := []byte("padding fixture password")
	var keys KeyCache
	keys.Bind("fixture")
	defer keys.Clear()
	sealer, err := NewSealer(pass, &keys)
	if err != nil {
		t.Fatal(err)
	}
	defer sealer.Close()
	raw := make([]byte, 8700)
	rand.Read(raw)
	blob, err := sealer.Seal(raw, archivePurpose)
	if err != nil || len(blob) != 3*bucketSize {
		t.Fatal("incorrect multi-bucket size", len(blob), err)
	}
	got, err := OpenObject(blob, pass, archivePurpose, &keys)
	if err != nil || !bytes.Equal(raw, got) {
		t.Fatal("multi-bucket round trip", err)
	}

	zipped, err := compressPlaintext([]byte("valid compressed data"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(sealer.key)
	aead, _ := cipher.NewGCM(block)
	for _, size := range []uint32{uint32(len(zipped)), ^uint32(0)} {
		header := bytes.Clone(blob[:headerSize])
		nonce := header[nonceOffset:]
		rand.Read(nonce)
		body := make([]byte, len(blob)-headerSize-tagSize)
		binary.BigEndian.PutUint32(body, size)
		copy(body[4:], zipped)
		aad := append(bytes.Clone(header), archivePurpose...)
		bad := aead.Seal(header, nonce, body, aad)
		if !ValidEnvelope(bad) {
			t.Fatal("fixture must reach authenticated payload checks")
		}
		if _, err := OpenObject(bad, pass, archivePurpose, &keys); err == nil {
			t.Fatalf("accepted invalid authenticated compressed length %d", size)
		}
	}
}
