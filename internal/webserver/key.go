package webserver

import (
	"bytes"
	"errors"
	"path/filepath"
	"yaare/internal/adapters/secrets"
)

const keyMarker = "instance-key-check-v1"

func verifyInstanceKey(root string, vault *secrets.ServerVault) error {
	marker, err := readOrCreatePrivate(filepath.Join(root, keyMarker), func() ([]byte, error) {
		if err := checkBootstrapDirectory(root, ""); err != nil {
			return nil, err
		}
		return vault.Protect([]byte(keyMarker), keyMarker)
	})
	if err != nil {
		return err
	}
	raw, err := vault.Unprotect(marker, keyMarker)
	defer clear(raw)
	if err != nil || !bytes.Equal(raw, []byte(keyMarker)) {
		return errors.New("实例密钥不匹配，请恢复原密钥。")
	}
	return nil
}
