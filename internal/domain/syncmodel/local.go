package syncmodel

import (
	"encoding/json"

	"yaare/internal/domain/workspace"
)

type LocalData struct {
	Extensions       workspace.PortableExtensions
	Preferences      json.RawMessage
	HostCredentials  []workspace.HostCredential
	ProxyCredentials []workspace.HostCredential
	Snapshot         workspace.Snapshot
	Keys             []workspace.SavedKey
	Identities       []workspace.SealedIdentity
}
