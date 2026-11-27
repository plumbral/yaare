package vault

import (
	"encoding/json"
	"errors"

	"yaare/internal/adapters/secrets"
	"yaare/internal/domain/workspace"
)

type ConnectionInput struct {
	RememberPassword bool
	Password         []byte
	PrivateKey       []byte
	KeyPath          string
	Passphrase       []byte
	ProxyPassword    []byte
}

func PrepareConnection(protector secrets.Protector, host workspace.Host, input ConnectionInput) (workspace.Host, workspace.HostCredentialChange, error) {
	h := workspace.NormalizeHost(host)
	change := workspace.HostCredentialChange{HostID: h.ID, Action: "clear"}
	if len(input.Password) > 16384 || len(input.Passphrase) > 16384 || len(input.PrivateKey) > 1<<20 {
		return workspace.Host{}, workspace.HostCredentialChange{}, workspace.Fail("CREDENTIAL_INVALID", "密码过长，请检查输入。")
	}
	if input.KeyPath != "" || len(input.PrivateKey) != 0 {
		if h.Authentication != "key" || h.UseAgent || h.IdentityID != "" || h.KeyID != "" || len(input.Password) != 0 || (input.KeyPath != "" && len(input.PrivateKey) != 0) {
			return workspace.Host{}, workspace.HostCredentialChange{}, workspace.Fail("CREDENTIAL_INVALID", "保存认证请求无效。")
		}
		var key workspace.SavedKey
		var err error
		if input.KeyPath != "" {
			key, err = PrepareKey(protector, KeyInput{Name: h.Name, Source: "file", Passphrase: input.Passphrase}, input.KeyPath)
		} else {
			key, err = PrepareKeyMaterial(protector, h.Name, input.PrivateKey, input.Passphrase)
		}
		if err != nil {
			var cause *workspace.Error
			if errors.As(err, &cause) && cause.Code == "CREDENTIAL_SAVE" {
				err = workspace.Fail("CREDENTIAL_SAVE", "无法保存密钥。")
			}
			return workspace.Host{}, workspace.HostCredentialChange{}, err
		}
		h.KeyID, change.Key = key.ID, &key
		input.Passphrase = nil
	}
	savePassword := input.RememberPassword && h.Authentication == "password" && h.IdentityID == ""
	savePassphrase := h.Authentication == "key" && workspace.SupportsHostCredential(h)
	if len(input.Password) != 0 && !savePassword || len(input.Passphrase) != 0 && !savePassphrase {
		return workspace.Host{}, workspace.HostCredentialChange{}, workspace.Fail("CREDENTIAL_INVALID", "保存认证请求无效。")
	}
	if len(input.ProxyPassword) != 0 {
		proxy, err := SealProxyCredential(protector, h, input.ProxyPassword)
		if err != nil {
			return workspace.Host{}, workspace.HostCredentialChange{}, err
		}
		change.Proxy = proxy
	}
	if savePassword || savePassphrase {
		change.Action = "keep"
		secret := input.Password
		if savePassphrase {
			secret = input.Passphrase
		}
		if len(secret) != 0 {
			change.Action = "replace"
			change.Credential = workspace.HostCredential{HostID: h.ID, Scope: workspace.CredentialScope(h)}
			raw, err := json.Marshal(HostPassword{Password: secret})
			if err != nil {
				return workspace.Host{}, workspace.HostCredentialChange{}, err
			}
			defer clear(raw)
			change.Credential.Cipher, err = protector.Protect(raw, HostCredentialPurpose(change.Credential))
			if err != nil {
				if errors.Is(err, secrets.ErrWorkspaceKey) {
					return workspace.Host{}, workspace.HostCredentialChange{}, err
				}
				return workspace.Host{}, workspace.HostCredentialChange{}, workspace.Fail("CREDENTIAL_SAVE", "无法加密保存连接密码，配置未更改。")
			}
		}
	}
	return h, change, nil
}

func SealProxyCredential(protector secrets.Protector, host workspace.Host, password []byte) (*workspace.HostCredential, error) {
	if !workspace.ValidProxyPassword(host.Proxy, password) {
		return nil, workspace.Fail("PROXY_AUTH_INVALID", "代理密码长度无效。")
	}
	c := workspace.HostCredential{HostID: host.ID, Scope: workspace.ProxyCredentialScope(host.ID, host.Proxy)}
	var err error
	c.Cipher, err = protector.Protect(password, ProxyCredentialPurpose(c))
	if err != nil {
		if errors.Is(err, secrets.ErrWorkspaceKey) {
			return nil, err
		}
		return nil, workspace.Fail("CREDENTIAL_SAVE", "无法加密保存代理密码。")
	}
	return &c, nil
}
