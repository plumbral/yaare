package ports

import (
	"context"

	"yaare/internal/domain/workspace"
)

type KeyRepository interface {
	Key(context.Context, string) (workspace.SavedKey, bool, error)
	KeyMetadata(context.Context) ([]workspace.KeyMeta, error)
	RegisterKey(context.Context, workspace.SavedKey) (workspace.SavedKey, error)
	ForgetKey(context.Context, string) error
}

type IdentityRepository interface {
	Identities(context.Context) ([]workspace.Identity, error)
	Identity(context.Context, string) (workspace.SealedIdentity, error)
	SaveIdentity(context.Context, workspace.SealedIdentity, bool) error
	SaveIdentityWithKey(context.Context, workspace.SealedIdentity, workspace.SavedKey) error
}
