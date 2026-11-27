package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"yaare/internal/adapters/secrets"
	"yaare/internal/app/vault"
	"yaare/internal/domain/workspace"
	"yaare/internal/ports"
)

type IdentityService struct {
	repository ports.IdentityRepository
	keys       *KeyService
	protector  secrets.Protector
}

func NewIdentityService(repository ports.IdentityRepository, keys *KeyService) *IdentityService {
	return &IdentityService{repository: repository, keys: keys, protector: secrets.CurrentUser{}}
}

type SaveIdentityRequest struct {
	ID                string `json:"id"`
	Version           int64  `json:"version"`
	Name              string `json:"name"`
	Username          string `json:"username"`
	Authentication    string `json:"authentication"`
	KeyID             string `json:"keyId"`
	UseAgent          bool   `json:"useAgent"`
	ReplaceSecret     bool   `json:"replaceSecret"`
	Password          string `json:"password"`
	PrivateKey        string `json:"privateKey"`
	GeneratedKeyToken string `json:"generatedKeyToken,omitempty"`
	Passphrase        string `json:"passphrase"`
}

func (s *IdentityService) List(ctx context.Context) ([]workspace.Identity, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	return s.repository.Identities(ctx)
}
func (s *IdentityService) Save(ctx context.Context, r SaveIdentityRequest) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	return s.save(ctx, r)
}
func (s *IdentityService) save(ctx context.Context, r SaveIdentityRequest) error {
	i := workspace.SealedIdentity{Identity: workspace.Identity{ID: r.ID, Version: r.Version, Name: strings.TrimSpace(r.Name), Username: strings.TrimSpace(r.Username), Authentication: r.Authentication, KeyID: r.KeyID, UseAgent: r.UseAgent}}
	if !workspace.ValidIdentity(i.Identity) || r.Version < 0 || len(r.Password) > 16384 || len(r.Passphrase) > 16384 || len(r.PrivateKey) > 1048576 {
		return errors.New("请检查身份名称、用户名和认证内容。")
	}
	var generated *workspace.SavedKey
	if r.GeneratedKeyToken != "" {
		if !r.ReplaceSecret || r.Authentication != "key" || r.UseAgent || r.Password != "" || r.KeyID != "" || r.PrivateKey != "" || r.Passphrase != "" || s.keys == nil {
			return errors.New("保存认证请求无效。")
		}
		material, err := s.keys.generatedPrivateKey(ctx, r.GeneratedKeyToken)
		if err != nil {
			return err
		}
		defer clear(material)
		key, err := vault.PrepareKeyMaterial(s.keys.protector, i.Name, material, nil)
		if err != nil {
			return errors.New("无法保存密钥。")
		}
		generated, i.KeyID, r.KeyID = &key, key.ID, key.ID
	}
	if !r.ReplaceSecret {
		old, err := s.repository.Identity(ctx, r.ID)
		if err != nil {
			return err
		}
		if old.Authentication != i.Authentication || old.KeyID != i.KeyID || old.UseAgent != i.UseAgent {
			return errors.New("更改认证方式或密钥时，请重新填写认证内容。")
		}
		i.Cipher = old.Cipher
	} else {
		secret := vault.IdentitySecret{Password: []byte(r.Password), PrivateKey: []byte(r.PrivateKey), Passphrase: []byte(r.Passphrase)}
		defer secret.Clear()
		if r.UseAgent {
			if r.Password != "" || r.PrivateKey != "" || r.KeyID != "" || r.Passphrase != "" {
				return errors.New("SSH Agent 身份不保存密码或私钥。")
			}
		} else if r.Authentication == "password" {
			if r.Password == "" || r.PrivateKey != "" || r.KeyID != "" || r.Passphrase != "" {
				return errors.New("请输入登录密码。")
			}
		} else {
			if (r.PrivateKey == "") == (r.KeyID == "") {
				return errors.New("请选择私钥文件或粘贴私钥，不能同时指定。")
			}
			if r.KeyID != "" && generated == nil {
				found, err := s.keys.available(ctx, r.KeyID)
				if err != nil {
					return err
				}
				if !found {
					return errors.New("私钥文件不可用。")
				}
			}
			if r.PrivateKey != "" {
				err := vault.ValidateMaterial(secret.PrivateKey, secret.Passphrase)
				if err != nil {
					return err
				}
			}
			secret.Password.Clear()
		}
		data, err := json.Marshal(secret)
		if err != nil {
			return errors.New("身份编码失败。")
		}
		defer clear(data)
		i.Cipher, err = s.protector.Protect(data, i.ID)
		if err != nil {
			return err
		}
	}
	if generated != nil {
		if err := s.repository.SaveIdentityWithKey(ctx, i, *generated); err != nil {
			return err
		}
		if r.GeneratedKeyToken != "" {
			s.keys.Discard(ctx, r.GeneratedKeyToken)
		}
		return nil
	}
	return s.repository.SaveIdentity(ctx, i, false)
}
func (s *IdentityService) Remove(ctx context.Context, id string, version int64) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	i, err := s.repository.Identity(ctx, id)
	if err != nil {
		return err
	}
	i.Version = version
	return s.repository.SaveIdentity(ctx, i, true)
}

func (s *IdentityService) resolve(ctx context.Context, id string) (workspace.Identity, vault.IdentitySecret, error) {
	i, err := s.repository.Identity(ctx, id)
	if err != nil {
		return workspace.Identity{}, vault.IdentitySecret{}, err
	}
	data, err := s.protector.Unprotect(i.Cipher, id)
	defer clear(data)
	if err != nil {
		return workspace.Identity{}, vault.IdentitySecret{}, err
	}
	var secret vault.IdentitySecret
	if json.Unmarshal(data, &secret) != nil {
		secret.Clear()
		return workspace.Identity{}, vault.IdentitySecret{}, errors.New("身份解锁失败，请重新保存认证内容。")
	}
	return i.Identity, secret, nil
}
