package webserver

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"yaare/internal/adapters/secrets"
)

func Initialize(c Config, prompt func() ([]byte, error)) error {
	_, _, err := instanceCredentials(c, true, prompt)
	return err
}

func instanceCredentials(c Config, generateKey bool, prompt func() ([]byte, error)) (*secrets.ServerVault, *authentication, error) {
	if err := os.MkdirAll(c.Root, 0700); err != nil {
		return nil, nil, err
	}
	key, err := readOrCreatePrivate(c.KeyFile, func() ([]byte, error) {
		if err := checkBootstrapDirectory(c.Root, ""); err != nil {
			return nil, err
		}
		if !generateKey {
			return nil, errors.New("请先运行 docker compose run --rm yaare init 初始化实例，或提供原实例密钥。")
		}
		if err := os.MkdirAll(filepath.Dir(c.KeyFile), 0700); err != nil {
			return nil, err
		}
		key := make([]byte, 32)
		rand.Read(key)
		return key, nil
	})
	if err != nil {
		return nil, nil, err
	}
	defer clear(key)
	vault, err := secrets.NewServerVault(key)
	if err != nil {
		return nil, nil, err
	}
	if err := verifyInstanceKey(c.Root, vault); err != nil {
		return nil, nil, err
	}
	if c.PasswordFile != "" {
		prompt = func() ([]byte, error) { return readPasswordFile(c.PasswordFile) }
	}
	auth, err := loadAuthentication(c.Root, c.User, prompt)
	return vault, auth, err
}

const stagingPrefix = ".yaare-init-"

func checkBootstrapDirectory(root, allowed string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "lost+found" || name == allowed || (entry.Type().IsRegular() && strings.HasPrefix(name, stagingPrefix)) {
			continue
		}
		return errors.New("已有实例数据缺少密钥或登录配置，请恢复配套的原密钥和数据备份。")
	}
	return nil
}

func readOrCreatePrivate(path string, create func() ([]byte, error)) ([]byte, error) {
	data, err := os.ReadFile(path)
	if !errors.Is(err, os.ErrNotExist) {
		return data, err
	}
	data, err = create()
	if err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil {
			return existing, nil
		}
		return nil, err
	}
	defer clear(data)
	tmp, err := os.CreateTemp(filepath.Dir(path), stagingPrefix+"*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		return nil, errors.Join(err, closeErr)
	}
	if err = os.Link(tmp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return os.ReadFile(path)
}
