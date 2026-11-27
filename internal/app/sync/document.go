package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/syncmodel"
)

type Document struct {
	Format             string
	Version            int
	MinReaderVersion   int `json:"minReaderVersion"`
	Generation         int64
	IncludeCredentials bool
	Created            string
	Workspace          syncmodel.Payload
}

func NewDocument(generation int64, include bool, at time.Time, payload syncmodel.Payload) Document {
	return Document{Format: "yaare-configuration", Version: syncmodel.Version, MinReaderVersion: syncmodel.Version, Generation: generation, IncludeCredentials: include, Created: at.UTC().Format(time.RFC3339), Workspace: payload}
}

func SealDocument[P ~string | ~[]byte](d Document, password P) ([]byte, error) {
	sealer, err := cloudsync.NewSealer([]byte(password), nil)
	if err != nil {
		return nil, err
	}
	defer sealer.Close()
	return sealDocument(d, sealer, configsync.CurrentObject)
}

func sealDocument(d Document, sealer *cloudsync.Sealer, name string) ([]byte, error) {
	d.Workspace = syncmodel.PackIcons(d.Workspace)
	raw, e := json.Marshal(d)
	if e != nil {
		return nil, e
	}
	defer clear(raw)
	return sealer.Seal(raw, name)
}
func DecodeDocument(raw []byte) (Document, error) {
	var d Document
	defer func() { syncmodel.ClearPayload(d.Workspace) }()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || len(bytes.Trim(raw[decoder.InputOffset():], " \t\r\n")) != 0 || d.Format != "yaare-configuration" || !syncmodel.SupportsVersion(d.Version, d.MinReaderVersion) || d.Generation < 0 || d.Workspace.Version != syncmodel.Version {
		return Document{}, errors.New("同步配置格式或版本不受支持。")
	}
	if _, e := time.Parse(time.RFC3339, d.Created); e != nil {
		return Document{}, errors.New("同步配置创建时间无效。")
	}
	if !d.IncludeCredentials && (len(d.Workspace.Keys) > 0 || len(d.Workspace.Identities) > 0 || len(d.Workspace.HostCredentials) > 0 || len(d.Workspace.ProxyCredentials) > 0) {
		return Document{}, errors.New("同步认证范围与远端配置不一致。")
	}
	if e := syncmodel.UnpackIcons(&d.Workspace); e != nil {
		return Document{}, e
	}
	p, e := syncmodel.Canonicalize(d.Workspace)
	if e != nil {
		syncmodel.ClearPayload(p)
		return Document{}, e
	}
	out := d
	out.Workspace = p
	return out, nil
}
func OpenDocument[P ~string | ~[]byte](blob []byte, password P) (Document, error) {
	return openDocument(blob, []byte(password), configsync.CurrentObject, nil)
}

func openDocument(blob, password []byte, name string, keys *cloudsync.KeyCache) (Document, error) {
	raw, e := cloudsync.OpenObject(blob, password, name, keys)
	if e != nil {
		return Document{}, e
	}
	defer clear(raw)
	return DecodeDocument(raw)
}
