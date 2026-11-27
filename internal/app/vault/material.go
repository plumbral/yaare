package vault

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	cryptossh "golang.org/x/crypto/ssh"
	"yaare/internal/adapters/secrets"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/domain/secret"
	"yaare/internal/domain/workspace"
)

type KeyInput struct {
	Name, Source, Token    string
	PrivateKey, Passphrase []byte
}

type KeySecret struct {
	PrivateKey secret.Bytes
	Passphrase secret.Bytes
}

type IdentitySecret struct {
	Password   secret.Bytes
	PrivateKey secret.Bytes
	Passphrase secret.Bytes
}

type HostPassword struct{ Password secret.Bytes }

func (s *KeySecret) Clear() {
	s.PrivateKey.Clear()
	s.Passphrase.Clear()
}

func (s *IdentitySecret) Clear() {
	s.Password.Clear()
	s.PrivateKey.Clear()
	s.Passphrase.Clear()
}

func HostCredentialPurpose(c workspace.HostCredential) string {
	return "host-password-v1:" + c.HostID + ":" + c.Scope
}
func ProxyCredentialPurpose(c workspace.HostCredential) string {
	return "host-proxy-v1:" + c.HostID + ":" + c.Scope
}

func FileAvailable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() <= 1024*1024
}

func InspectFile(path string) (string, string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", errors.New("私钥文件不可用，请重新选择。")
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil || !FileAvailable(canonical) {
		return "", "", errors.New("请选择不超过 1 MB 的普通私钥文件。")
	}
	file, err := os.Open(canonical)
	if err != nil {
		return "", "", errors.New("无法读取私钥文件。")
	}
	defer file.Close()
	header, more, err := bufio.NewReaderSize(file, 256).ReadLine()
	if err != nil || more {
		return "", "", errors.New("无法识别私钥文件头。")
	}
	if strings.TrimSpace(string(header)) == sshclient.EncryptedPKCS8Header {
		return "", "", ErrEncryptedPKCS8
	}
	format := Format(header)
	if format == "" {
		return "", "", errors.New("请选择 OpenSSH 或 PEM 私钥，不要选择 .pub 公钥。")
	}
	return canonical, format, nil
}
func Format(data []byte) string {
	header, _, _ := bytes.Cut(data, []byte("\n"))
	switch string(bytes.TrimSpace(header)) {
	case "-----BEGIN OPENSSH PRIVATE KEY-----":
		return "OpenSSH"
	case "-----BEGIN RSA PRIVATE KEY-----", "-----BEGIN EC PRIVATE KEY-----", "-----BEGIN PRIVATE KEY-----":
		return "PEM"
	}
	return ""
}

var ErrPassphraseMissing = errors.New("私钥内容或解密口令无效；加密私钥需要正确口令。")
var ErrEncryptedPKCS8 = errors.New(sshclient.EncryptedPKCS8Message)

func ValidateMaterial(data, passphrase []byte) error {
	if len(data) == 0 || len(data) > 1048576 {
		return errors.New("请输入有效私钥，最大 1 MB。")
	}
	if sshclient.IsEncryptedPKCS8(data) {
		return ErrEncryptedPKCS8
	}
	var err error
	if len(passphrase) == 0 {
		_, err = cryptossh.ParsePrivateKey(data)
	} else {
		_, err = cryptossh.ParsePrivateKeyWithPassphrase(data, passphrase)
	}
	if err != nil {
		var missing *cryptossh.PassphraseMissingError
		if errors.As(err, &missing) {
			return ErrPassphraseMissing
		}
		if errors.Is(err, x509.IncorrectPasswordError) {
			return errors.New("私钥口令错误。")
		}
		return errors.New("私钥内容或解密口令无效；加密私钥需要正确口令。")
	}
	return nil
}
func PrepareKey(protector secrets.Protector, r KeyInput, path string) (workspace.SavedKey, error) {
	if len(r.PrivateKey) > 1<<20 || len(r.Passphrase) > 16384 {
		return workspace.SavedKey{}, errors.New("私钥或口令超过支持长度。")
	}
	switch r.Source {
	case "file":
		if path == "" || len(r.PrivateKey) != 0 {
			return workspace.SavedKey{}, errors.New("请选择文件或粘贴私钥，不能同时指定。")
		}
		file, err := os.Open(path)
		if err != nil {
			return workspace.SavedKey{}, workspace.Fail("KEY_UNAVAILABLE", "无法读取私钥文件。")
		}
		defer file.Close()
		if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return workspace.SavedKey{}, workspace.Fail("KEY_INVALID", "请选择不超过 1 MB 的普通私钥文件。")
		}
		data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		defer clear(data)
		if err != nil || len(data) == 0 || len(data) > 1<<20 {
			return workspace.SavedKey{}, workspace.Fail("KEY_UNAVAILABLE", "无法读取私钥文件。")
		}
		return PrepareKeyMaterial(protector, r.Name, data, r.Passphrase)
	case "text":
		if path != "" || r.Token != "" {
			return workspace.SavedKey{}, errors.New("请选择文件或粘贴私钥，不能同时指定。")
		}
		return PrepareKeyMaterial(protector, r.Name, bytes.TrimSpace(r.PrivateKey), r.Passphrase)
	default:
		return workspace.SavedKey{}, errors.New("请选择有效的密钥来源。")
	}
}

func PrepareKeyMaterial(protector secrets.Protector, name string, material, passphrase []byte) (workspace.SavedKey, error) {
	if len(material) > 1<<20 || len(passphrase) > 16384 {
		return workspace.SavedKey{}, errors.New("私钥或口令超过支持长度。")
	}
	format := Format(material)
	if format == "" {
		if sshclient.IsEncryptedPKCS8(material) {
			return workspace.SavedKey{}, ErrEncryptedPKCS8
		}
		return workspace.SavedKey{}, errors.New("请粘贴 OpenSSH 或 PEM 私钥，不要粘贴公钥。")
	}
	if err := ValidateMaterial(material, passphrase); err != nil {
		return workspace.SavedKey{}, err
	}
	key := workspace.SavedKey{ID: workspace.NewID(), Name: strings.TrimSpace(name), Format: format}
	raw, err := json.Marshal(KeySecret{PrivateKey: material, Passphrase: passphrase})
	if err != nil {
		return workspace.SavedKey{}, errors.New("无法保存密钥。")
	}
	defer clear(raw)
	key.Cipher, err = protector.Protect(raw, "key-v1:"+key.ID)
	if err != nil {
		return workspace.SavedKey{}, workspace.Fail("CREDENTIAL_SAVE", err.Error())
	}
	if !workspace.ValidKey(key) {
		return workspace.SavedKey{}, errors.New("请输入有效密钥名称，最多 255 个字符。")
	}
	return key, nil
}

func OpenMaterial(protector secrets.Protector, id, path string, cipher []byte) (material, passphrase []byte, err error) {
	if path != "" && !FileAvailable(path) {
		return nil, nil, errors.New("密钥不可用，请重新添加。")
	}
	if len(cipher) > 0 {
		data, err := protector.Unprotect(cipher, "key-v1:"+id)
		defer clear(data)
		if err != nil {
			return nil, nil, err
		}
		var secret KeySecret
		if json.Unmarshal(data, &secret) != nil {
			secret.Clear()
			return nil, nil, errors.New("保存的密钥无法解锁，请重新添加。")
		}
		material = secret.PrivateKey
		passphrase = secret.Passphrase
	}
	if path == "" && len(material) == 0 {
		clear(passphrase)
		return nil, nil, errors.New("保存的密钥无效。")
	}
	return material, passphrase, nil
}
