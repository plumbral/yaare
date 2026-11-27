package ipc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

func TestWorkspaceRequiresCallingPageAndConstructorsDoNoIO(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uninitialized")
	repository := sqlite.New(func() (string, error) { return dir, nil })
	defer repository.Close()
	service := NewWorkspaceService(app.NewWorkspace(repository))
	ctx := context.Background()
	if _, err := service.Load(ctx); err == nil {
		t.Fatal("unowned read accepted")
	}
	if _, err := service.Apply(ctx, workspace.Change{Kind: "addGroup", Name: "unauthorized"}); err == nil {
		t.Fatal("unowned write accepted")
	}
	if _, err := service.Retry(ctx); err == nil {
		t.Fatal("unowned retry accepted")
	}
	if _, err := service.Recover(ctx); err == nil {
		t.Fatal("unowned recovery accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("constructor/generator performed IO")
	}
}
