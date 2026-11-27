package ports

import (
	"context"
	"yaare/internal/domain/syncmodel"
)

type PortableWorkspaceRepository interface {
	CloudRead(context.Context) (syncmodel.LocalData, error)
}
