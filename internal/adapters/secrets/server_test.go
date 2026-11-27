package secrets

import (
	"bytes"
	"testing"
)

func TestServerVaultAuthenticatesOwnerAndCiphertext(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	v, e := NewServerVault(key)
	if e != nil {
		t.Fatal(e)
	}
	clear(key)
	plain := []byte("test-only password")
	sealed, e := v.Protect(plain, "identity-a")
	if e != nil || bytes.Contains(sealed, plain) {
		t.Fatal("not encrypted", e)
	}
	out, e := v.Unprotect(sealed, "identity-a")
	if e != nil || !bytes.Equal(out, plain) {
		t.Fatal("round trip", e)
	}
	if _, e = v.Unprotect(sealed, "identity-b"); e == nil {
		t.Fatal("wrong identity")
	}
	sealed[len(sealed)-1] ^= 1
	if _, e = v.Unprotect(sealed, "identity-a"); e == nil {
		t.Fatal("tampered")
	}
	if _, e = v.Unprotect([]byte("old plaintext"), "identity-a"); e == nil {
		t.Fatal("fallback")
	}
	if _, e = NewServerVault([]byte("short")); e == nil {
		t.Fatal("short key")
	}
}
