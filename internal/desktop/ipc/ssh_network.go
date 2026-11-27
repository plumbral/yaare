package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"yaare/internal/adapters/ssh"
	"yaare/internal/app/vault"
	"yaare/internal/domain/workspace"
)

func (s *SSHService) resolveSSHConfig(ctx context.Context, r SSHConnectRequest) (sshclient.Config, func(), error) {
	if r.TimeoutSeconds == 0 {
		r.TimeoutSeconds = 15
	}
	if len(r.PrivateKey) > 1<<20 {
		return sshclient.Config{}, func() {}, &sshclient.Fault{Code: "KEY_INVALID", Message: "私钥内容超过 1 MB，或同时指定了文件和私钥内容。"}
	}
	if len(r.Password) > 16384 || len(r.Passphrase) > 16384 {
		return sshclient.Config{}, func() {}, &sshclient.Fault{Code: "INVALID_CONFIG", Message: "请检查链路中的服务器地址、端口、用户名和连接超时。"}
	}
	cfg := sshclient.Config{Host: r.Address, Name: r.Name, Port: r.Port, Username: r.Username, Authentication: r.Authentication, Password: []byte(r.Password), Passphrase: []byte(r.Passphrase), PrivateKey: []byte(r.PrivateKey), Timeout: time.Duration(r.TimeoutSeconds) * time.Second, Proxy: r.Proxy}
	cfg.UseAgent = r.UseAgent
	r.Password = ""
	r.Passphrase = ""
	r.PrivateKey = ""
	release := func() {}
	fail := func(code, message string) (sshclient.Config, func(), error) {
		cfg.ClearSecrets()
		release()
		return sshclient.Config{}, func() {}, &sshclient.Fault{Code: code, Message: message}
	}
	if s.workspace != nil && r.HostID != "" && r.Proxy.UseAuthentication {
		host, _, found, err := s.workspace.Host(ctx, r.HostID)
		if err != nil {
			return fail("STORAGE_READ_FAILED", "无法读取已保存的认证，请检查工作区存储后重试。")
		}
		if found && host.Proxy == r.Proxy {
			c, err := s.workspace.ProxyCredential(ctx, host.ID)
			if err != nil {
				return fail("STORAGE_READ_FAILED", "无法读取已保存的认证，请检查工作区存储后重试。")
			}
			if workspace.ProxyCredentialMatches(host, c) {
				raw, err := s.credentialProtector.Unprotect(c.Cipher, vault.ProxyCredentialPurpose(c))
				cfg.ProxyPassword = raw
				if err != nil || !workspace.ValidProxyPassword(r.Proxy, cfg.ProxyPassword) {
					return fail("CREDENTIAL_UNAVAILABLE", "无法解锁代理密码。")
				}
			} else {
				cfg.SaveProxyPassword = func(password []byte) error {
					return s.savePromptedProxyPassword(ctx, host.ID, host.Proxy, password)
				}
			}
		}
	}
	if r.UseSavedCredential {
		if s.workspace == nil || r.HostID == "" || len(cfg.Password) != 0 || len(cfg.PrivateKey) != 0 || len(cfg.Passphrase) != 0 {
			return fail("INVALID_AUTHENTICATION", "已保存密码请求无效。")
		}
		h, c, e := s.workspace.HostCredential(ctx, r.HostID)
		expected := workspace.Host{ID: r.HostID, Address: r.Address, Port: r.Port, Username: r.Username, Authentication: r.Authentication, KeyID: r.KeyID, IdentityID: r.IdentityID, UseAgent: r.UseAgent}
		if e != nil {
			var known *workspace.Error
			if errors.As(e, &known) {
				return fail(known.Code, known.Message)
			}
			return fail("STORAGE_READ_FAILED", "无法读取已保存的认证，请检查工作区存储后重试。")
		}
		if workspace.CredentialScope(h) != workspace.CredentialScope(expected) {
			return fail("CREDENTIAL_UNAVAILABLE", "此连接的已保存认证已变化，请重新打开连接。")
		}
		raw, e := s.credentialProtector.Unprotect(c.Cipher, vault.HostCredentialPurpose(c))
		if e != nil {
			clear(raw)
			return fail("CREDENTIAL_UNAVAILABLE", "无法解锁连接密码，请重新输入或检查系统密钥环。")
		}
		var secret vault.HostPassword
		e = json.Unmarshal(raw, &secret)
		clear(raw)
		if e != nil || len(secret.Password) == 0 || len(secret.Password) > 16384 {
			clear(secret.Password)
			return fail("CREDENTIAL_UNAVAILABLE", "已保存密码无效，请重新输入。")
		}
		if cfg.Authentication == "key" {
			cfg.Passphrase = secret.Password
		} else {
			cfg.Password = secret.Password
		}
	}
	if r.IdentityID != "" {
		if s.identities == nil || len(cfg.Password) != 0 || len(cfg.PrivateKey) != 0 || r.KeyID != "" {
			return fail("INVALID_IDENTITY", "身份认证请求无效。")
		}
		identity, secret, err := s.identities.resolve(ctx, r.IdentityID)
		if err != nil {
			return fail("IDENTITY_UNAVAILABLE", "无法使用此身份，请在身份页更新认证内容。")
		}
		cfg.Username = identity.Username
		cfg.Authentication = identity.Authentication
		cfg.UseAgent = identity.UseAgent
		r.KeyID = identity.KeyID
		cfg.Password = secret.Password
		cfg.PrivateKey = secret.PrivateKey
		if len(cfg.Passphrase) == 0 {
			cfg.Passphrase = secret.Passphrase
		} else {
			clear(secret.Passphrase)
		}
	}
	if cfg.UseAgent && (cfg.Authentication != "key" || r.KeyID != "" || len(cfg.PrivateKey) != 0 || len(cfg.Password) != 0 || len(cfg.Passphrase) != 0) {
		return fail("INVALID_AUTHENTICATION", "SSH Agent 不能同时填写密码、私钥或口令。")
	}
	if len(cfg.PrivateKey) > 1024*1024 || (len(cfg.PrivateKey) != 0 && (cfg.Authentication != "key" || r.KeyID != "")) {
		return fail("KEY_INVALID", "私钥内容超过 1 MB，或同时指定了文件和私钥内容。")
	}
	if r.TemporaryKeyToken != "" {
		if cfg.Authentication != "key" || r.UseSavedCredential || r.IdentityID != "" || r.UseAgent || r.KeyID != "" || len(cfg.PrivateKey) != 0 {
			return fail("KEY_INVALID", "保存认证请求无效。")
		}
		cfg.PrivateKeyPath = s.keys.takeTemporaryFile(ctx, r.TemporaryKeyToken)
		if cfg.PrivateKeyPath == "" {
			return fail("KEY_UNAVAILABLE", "请选择私钥文件，原选择可能已过期。")
		}
	}
	if cfg.Authentication == "key" && len(cfg.PrivateKey) == 0 && r.KeyID != "" {
		var saved []byte
		var err error
		cfg.PrivateKeyPath, cfg.PrivateKey, saved, release, err = s.keys.acquireMaterial(r.KeyID)
		if err != nil {
			release = func() {}
			return fail("KEY_UNAVAILABLE", "密钥不可用或无法解锁，请在密钥页重新添加。")
		}
		if len(cfg.Passphrase) == 0 {
			cfg.Passphrase = saved
		} else {
			clear(saved)
		}
	}
	return cfg, release, nil
}

func (s *SSHService) savePromptedProxyPassword(ctx context.Context, id string, proxy workspace.ProxyConfig, password []byte) error {
	host, revision, found, err := s.workspace.Host(ctx, id)
	if err != nil || !found || host.Proxy != proxy {
		return nil
	}
	current, err := s.workspace.ProxyCredential(ctx, id)
	if err != nil || workspace.ProxyCredentialMatches(host, current) {
		return err
	}
	credential, err := vault.SealProxyCredential(s.credentialProtector, host, password)
	if err != nil {
		return err
	}
	_, err = s.workspace.SaveConnection(ctx, workspace.Change{
		Kind: "saveHost", Host: host, Revision: revision,
	}, workspace.HostCredentialChange{HostID: id, Action: "keep", Proxy: credential})
	return err
}

func (s *SSHService) SetForward(ctx context.Context, id string, attempt int, ruleID string, start bool) bool {
	run := s.owned(ctx, id, attempt)
	return run != nil && run.SetForward(ruleID, start)
}
