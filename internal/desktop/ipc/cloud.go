package ipc

import (
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"

	"context"

	"encoding/json"

	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
)

type CloudSummary struct {
	Hosts          int `json:"hosts"`
	Groups         int `json:"groups"`
	Commands       int `json:"commands"`
	Keys           int `json:"keys"`
	Identities     int `json:"identities"`
	FileReferences int `json:"fileReferences"`
	Preferences    int `json:"preferences"`
}

type CloudResult struct {
	State   string `json:"state"`
	Message string `json:"message"`
	Pulled  bool   `json:"pulled"`
}

type CloudService struct {
	protector secrets.Protector
	store     *sqlite.Store
}

func NewCloudService(store *sqlite.Store, p secrets.Protector) *CloudService {
	if p == nil {
		p = secrets.CurrentUser{}
	}
	return &CloudService{protector: p, store: store}
}

func summary(p syncmodel.Payload) CloudSummary {
	refs := 0
	for _, k := range p.Keys {
		if len(k.Material) == 0 {
			refs++
		}
	}
	var preferences map[string]json.RawMessage
	_ = json.Unmarshal(p.Preferences, &preferences)
	return CloudSummary{len(p.Snapshot.Hosts), len(p.Snapshot.Groups) - 1, len(p.Snapshot.Commands), len(p.Keys), len(p.Identities), refs, len(preferences)}
}
func (s *CloudService) materialize(ctx context.Context, p syncmodel.Payload) (syncmodel.LocalData, error) {
	return (syncapp.NewPortableWorkspace(s.store, s.protector)).Materialize(ctx, p)
}

func (s *CloudService) validate(ctx context.Context, p syncmodel.Payload) (syncmodel.LocalData, error) {
	return (syncapp.NewPortableWorkspace(s.store, s.protector)).Validate(ctx, p)
}
