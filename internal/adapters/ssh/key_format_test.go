package sshclient

import (
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedPKCS8ReportsFormatNotPassphrase(t *testing.T) {
	data := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("synthetic")})
	file := filepath.Join(t.TempDir(), "encrypted.pem")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, passphrase := range []string{"", "test-passphrase"} {
		_, err := signer(file, []byte(passphrase))
		if code, message := classify(err); code != "KEY_INVALID" || message != EncryptedPKCS8Message {
			t.Fatalf("unsupported format via file returned %q: %q", code, message)
		}
		_, err = parseSigner(append([]byte("PEM preamble\n"), data...), []byte(passphrase))
		if code, message := classify(err); code != "KEY_INVALID" || message != EncryptedPKCS8Message {
			t.Fatalf("unsupported pasted format returned %q: %q", code, message)
		}
	}
}
