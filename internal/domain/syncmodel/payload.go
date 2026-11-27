package syncmodel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"yaare/internal/domain/workspace"
)

type Key struct {
	ID, Name, Format string
	Material         json.RawMessage `json:",omitempty"`
}
type Identity struct {
	Metadata workspace.Identity
	Material json.RawMessage
}

const Version = 2

func SupportsVersion(version, minReader int) bool {
	return version == Version && minReader == Version
}

type Payload struct {
	IconAssets       map[string]string `json:",omitempty"`
	ProxyCredentials []HostCredential  `json:",omitempty"`
	HostCredentials  []HostCredential  `json:",omitempty"`
	Version          int
	Snapshot         workspace.Snapshot
	Keys             []Key
	Identities       []Identity
	Preferences      json.RawMessage              `json:",omitempty"`
	Extensions       workspace.PortableExtensions `json:"extensions,omitempty"`
}

func ClearPayload(p Payload) {
	for _, c := range p.ProxyCredentials {
		clear(c.Material)
	}
	for _, c := range p.HostCredentials {
		clear(c.Material)
	}
	for _, k := range p.Keys {
		clear(k.Material)
	}
	for _, i := range p.Identities {
		clear(i.Material)
	}
}

type HostCredential struct {
	HostID   string
	Scope    string
	Material json.RawMessage
}

func PayloadHash(p Payload) string {
	raw, _ := json.Marshal(p)
	defer clear(raw)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func Differences(local, remote Payload) (added, removed, changed []string) {
	added = []string{}
	removed = []string{}
	changed = []string{}
	before := map[string]workspace.Host{}
	for _, h := range local.Snapshot.Hosts {
		before[h.ID] = h
	}
	for _, h := range remote.Snapshot.Hosts {
		old, ok := before[h.ID]
		if !ok {
			added = append(added, h.Name)
		} else {
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(h)
			if !bytes.Equal(a, b) {
				changed = append(changed, h.Name)
			}
		}
		delete(before, h.ID)
	}
	for _, h := range before {
		removed = append(removed, h.Name)
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	return
}
