package sync

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"yaare/internal/adapters/secrets"
	"yaare/internal/app/vault"
	"yaare/internal/domain/secret"
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
	"yaare/internal/ports"
)

type PortableWorkspace struct {
	store     ports.PortableWorkspaceRepository
	protector secrets.Protector
}

func NewPortableWorkspace(store ports.PortableWorkspaceRepository, protector secrets.Protector) PortableWorkspace {
	return PortableWorkspace{store: store, protector: protector}
}

func (s PortableWorkspace) Capture(ctx context.Context, includeSecrets bool) (syncmodel.Payload, int64, error) {
	data, e := s.store.CloudRead(ctx)
	if e != nil {
		return syncmodel.Payload{}, 0, e
	}
	p := syncmodel.Payload{Version: syncmodel.Version, Snapshot: data.Snapshot, Keys: []syncmodel.Key{}, Identities: []syncmodel.Identity{}, Preferences: data.Preferences}
	if p.Preferences == nil {
		p.Preferences = json.RawMessage(`{}`)
	}
	p.Extensions = data.Extensions
	revision := p.Snapshot.Revision
	p.Snapshot.Revision = 0
	for i := range p.Snapshot.Hosts {
		p.Snapshot.Hosts[i].LastUsed = 0
	}
	if !includeSecrets {
		users := map[string]string{}
		identities := map[string]workspace.Identity{}
		for _, identity := range data.Identities {
			users[identity.ID] = identity.Username
			identities[identity.ID] = identity.Identity
		}
		for i := range p.Snapshot.Hosts {
			h := &p.Snapshot.Hosts[i]
			h.HasCredential = false
			if user := users[h.IdentityID]; user != "" {
				h.Username = user
			}
			if identity, ok := identities[h.IdentityID]; ok {
				h.Authentication = identity.Authentication
				h.UseAgent = identity.UseAgent
			}
			h.IdentityID = ""
			h.KeyID = ""
		}
		return p, revision, nil
	}
	protector, releaseScope := secrets.WithScope(s.protector)
	defer releaseScope()
	for _, c := range data.HostCredentials {
		raw, e := protector.Unprotect(c.Cipher, vault.HostCredentialPurpose(c))
		if e != nil {
			clear(raw)
			syncmodel.ClearPayload(p)
			return syncmodel.Payload{}, 0, e
		}
		p.HostCredentials = append(p.HostCredentials, syncmodel.HostCredential{HostID: c.HostID, Scope: c.Scope, Material: raw})
	}
	for _, c := range data.ProxyCredentials {
		raw, e := protector.Unprotect(c.Cipher, vault.ProxyCredentialPurpose(c))
		if e != nil {
			clear(raw)
			syncmodel.ClearPayload(p)
			return syncmodel.Payload{}, 0, e
		}
		material, e := json.Marshal(secret.Bytes(raw))
		clear(raw)
		if e != nil {
			syncmodel.ClearPayload(p)
			return syncmodel.Payload{}, 0, e
		}
		p.ProxyCredentials = append(p.ProxyCredentials, syncmodel.HostCredential{HostID: c.HostID, Scope: c.Scope, Material: material})
	}
	for _, k := range data.Keys {
		key := syncmodel.Key{ID: k.ID, Name: k.Name, Format: k.Format}
		if k.Path == "" {
			key.Material, e = protector.Unprotect(k.Cipher, "key-v1:"+k.ID)
			if e != nil {
				clear(key.Material)
				syncmodel.ClearPayload(p)
				return syncmodel.Payload{}, 0, e
			}
		}
		p.Keys = append(p.Keys, key)
	}
	for _, i := range data.Identities {
		material, e := protector.Unprotect(i.Cipher, i.ID)
		if e != nil {
			clear(material)
			syncmodel.ClearPayload(p)
			return syncmodel.Payload{}, 0, e
		}
		i.Version = 0
		p.Identities = append(p.Identities, syncmodel.Identity{Metadata: i.Identity, Material: material})
	}
	return p, revision, nil
}

func (s PortableWorkspace) Materialize(ctx context.Context, p syncmodel.Payload) (syncmodel.LocalData, error) {
	return s.materializeMode(ctx, p, true)
}

func (s PortableWorkspace) Validate(ctx context.Context, p syncmodel.Payload) (syncmodel.LocalData, error) {
	return s.materializeMode(ctx, p, false)
}

func (s PortableWorkspace) materializeMode(ctx context.Context, p syncmodel.Payload, seal bool) (syncmodel.LocalData, error) {
	if len(p.Keys) > workspace.MaxSavedKeys {
		return syncmodel.LocalData{}, workspace.Fail("KEY_LIMIT", "最多保存 256 个密钥引用。")
	}
	current, e := s.store.CloudRead(ctx)
	if e != nil {
		return syncmodel.LocalData{}, e
	}
	protector := s.protector
	if seal {
		var releaseScope func()
		protector, releaseScope = secrets.WithScope(s.protector)
		defer releaseScope()
	}
	localKeys := map[string]workspace.SavedKey{}
	localIdentities := map[string]int64{}
	recent := map[string]int64{}
	for _, k := range current.Keys {
		localKeys[k.ID] = k
	}
	for _, i := range current.Identities {
		localIdentities[i.ID] = i.Version
	}
	for _, h := range current.Snapshot.Hosts {
		recent[h.ID] = h.LastUsed
	}
	extensions := p.Extensions
	if extensions == nil {
		extensions = workspace.PortableExtensions{}
	}
	data := syncmodel.LocalData{Snapshot: p.Snapshot, Extensions: extensions, Preferences: p.Preferences}
	data.Snapshot.Hosts = slices.Clone(p.Snapshot.Hosts)
	hosts := map[string]workspace.Host{}
	for _, h := range p.Snapshot.Hosts {
		hosts[h.ID] = h
	}
	proxyPlainBytes := 0
	for _, c := range p.ProxyCredentials {
		h, ok := hosts[c.HostID]
		var password secret.Bytes
		if !ok || c.Scope != workspace.ProxyCredentialScope(h.ID, h.Proxy) || len(c.Material) > 24578 ||
			json.Unmarshal(c.Material, &password) != nil || !workspace.ValidProxyPassword(h.Proxy, password) {
			clear(password)
			return data, errors.New("连接认证记录无效。")
		}
		sealed := workspace.HostCredential{HostID: h.ID, Scope: c.Scope}
		if seal {
			sealed.Cipher, e = protector.Protect(password, vault.ProxyCredentialPurpose(sealed))
			clear(password)
			if e != nil {
				return data, e
			}
		} else {
			proxyPlainBytes += len(password)
			clear(password)
			if proxyPlainBytes > 16<<20 {
				return data, errors.New("连接认证记录无效。")
			}
			sealed.Cipher = []byte{1}
		}
		data.ProxyCredentials = append(data.ProxyCredentials, sealed)
	}
	seenCredentials := map[string]bool{}
	credPlainBytes := 0
	for _, c := range p.HostCredentials {
		h, ok := hosts[c.HostID]
		var secret vault.HostPassword
		if !ok || seenCredentials[c.HostID] || !workspace.SupportsHostCredential(h) || c.Scope != workspace.CredentialScope(h) || len(c.Material) > 65536 || json.Unmarshal(c.Material, &secret) != nil || len(secret.Password) == 0 || len(secret.Password) > 16384 {
			clear(secret.Password)
			return data, errors.New("连接认证记录无效。")
		}
		clear(secret.Password)
		sealed := workspace.HostCredential{HostID: c.HostID, Scope: c.Scope}
		if seal {
			sealed.Cipher, e = protector.Protect(c.Material, vault.HostCredentialPurpose(sealed))
			if e != nil {
				return data, e
			}
		} else {
			credPlainBytes += len(c.Material)
			if credPlainBytes > 16<<20 {
				return data, errors.New("连接认证记录无效。")
			}
			sealed.Cipher = []byte{1}
		}
		seenCredentials[c.HostID] = true
		data.HostCredentials = append(data.HostCredentials, sealed)
	}
	ids := map[string]bool{}
	unavailable := map[string]bool{}
	keyPlainBytes := 0
	for _, k := range p.Keys {
		if ids[k.ID] || unavailable[k.ID] {
			return data, errors.New("配置包密钥标识重复。")
		}
		key := workspace.SavedKey{ID: k.ID, Name: k.Name, Format: k.Format}
		if len(k.Material) == 0 {
			old, ok := localKeys[k.ID]
			if !ok || old.Path == "" {
				unavailable[k.ID] = true
				continue
			}
			key = old
		} else {
			if !validKeyMaterial(k.Material) {
				return data, errors.New("配置包密钥内容无效。")
			}
			if seal {
				key.Cipher, e = protector.Protect(k.Material, "key-v1:"+k.ID)
				if e != nil {
					return data, e
				}
			} else {
				keyPlainBytes += len(k.Material)
				if keyPlainBytes > workspace.MaxTotalKeyCipherBytes {
					return data, errors.New("同步密钥超过 16 MiB。")
				}
				key.Cipher = []byte{1}
			}
		}
		if !workspace.ValidKey(key) {
			return data, errors.New("配置包密钥元数据无效。")
		}
		ids[k.ID] = true
		data.Keys = append(data.Keys, key)
	}
	for _, i := range p.Identities {
		if err := validateIdentityMaterial(i); err != nil {
			return data, err
		}
		if i.Metadata.KeyID != "" && !ids[i.Metadata.KeyID] {
			i.Metadata.KeyID = ""
		}
		i.Metadata.Version = localIdentities[i.Metadata.ID] + 1
		var cipher []byte
		if seal {
			cipher, e = protector.Protect(i.Material, i.Metadata.ID)
			if e != nil {
				return data, e
			}
		} else {
			cipher = []byte{1}
		}
		data.Identities = append(data.Identities, workspace.SealedIdentity{Identity: i.Metadata, Cipher: cipher})
	}
	resolvedHosts := make(map[string]workspace.Host, len(data.Snapshot.Hosts))
	for i := range data.Snapshot.Hosts {
		h := &data.Snapshot.Hosts[i]
		h.HasCredential = seenCredentials[h.ID]
		if h.KeyID != "" && !ids[h.KeyID] {
			h.KeyID = ""
			h.HasCredential = false
		}
		h.LastUsed = recent[h.ID]
		resolvedHosts[h.ID] = *h
	}
	data.HostCredentials = slices.DeleteFunc(data.HostCredentials, func(c workspace.HostCredential) bool {
		if workspace.CredentialMatches(resolvedHosts[c.HostID], c) {
			return false
		}
		clear(c.Cipher)
		return true
	})
	return data, nil
}

func validKeyMaterial(raw []byte) bool {
	var key vault.KeySecret
	defer key.Clear()
	return len(raw) <= 2<<20 && json.Unmarshal(raw, &key) == nil && len(key.Passphrase) <= 16384 && vault.ValidateMaterial(key.PrivateKey, key.Passphrase) == nil
}

func validateIdentityMaterial(i syncmodel.Identity) error {
	var identity vault.IdentitySecret
	defer identity.Clear()
	if len(i.Material) > 2<<20 || json.Unmarshal(i.Material, &identity) != nil || len(identity.Password) > 16384 || len(identity.Passphrase) > 16384 || len(identity.PrivateKey) > 1<<20 || !workspace.ValidIdentity(i.Metadata) {
		return errors.New("配置包身份无效。")
	}
	if len(identity.PrivateKey) != 0 && vault.ValidateMaterial(identity.PrivateKey, identity.Passphrase) != nil {
		return errors.New("配置包身份私钥无效。")
	}
	return nil
}
