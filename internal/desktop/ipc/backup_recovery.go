package ipc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"
)

type RecoveredWorkspace struct {
	Directory string `json:"directory"`
	Launched  bool   `json:"launched"`
}

func (s *BackupService) RestoreNewWorkspace(ctx context.Context, fileID, password string) (RecoveredWorkspace, error) {
	if err := nativeBackupCaller(ctx); err != nil {
		return RecoveredWorkspace{}, err
	}
	if err := s.begin(); err != nil {
		return RecoveredWorkspace{}, err
	}
	defer s.end()
	pass := []byte(password)
	password = ""
	defer clear(pass)
	envelope, selected, err := s.unlockSelection(ctx, fileID, pass)
	if err != nil {
		return RecoveredWorkspace{}, err
	}
	defer syncmodel.ClearPayload(envelope.Workspace)
	parents, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: uiText(callerPage(ctx), "选择新工作区的保存位置"),
		ButtonLabel: uiText(callerPage(ctx), "恢复到新工作区"), Directory: true, CreateDirectories: true,
	})
	if err != nil || len(parents) == 0 {
		return RecoveredWorkspace{}, err
	}
	s.mu.Lock()
	active := s.selections[callerPage(ctx)] == selected && len(selected.blob) != 0 && time.Now().Before(selected.expires)
	s.mu.Unlock()
	if !active {
		return RecoveredWorkspace{}, errors.New("所选配置已失效，请重新选择。")
	}
	dir, err := restoreNewWorkspace(ctx, parents[0], envelope.Workspace, secrets.CurrentUser{})
	if err != nil {
		return RecoveredWorkspace{}, err
	}
	s.release(callerPage(ctx))
	result := RecoveredWorkspace{Directory: dir}
	if executable, err := os.Executable(); err == nil {
		cmd := exec.Command(executable, "--yaare-data-dir="+dir)
		if cmd.Start() == nil {
			result.Launched = true
			_ = cmd.Process.Release()
		}
	}
	return result, nil
}

func restoreNewWorkspace(ctx context.Context, parent string, payload syncmodel.Payload, base secrets.Protector) (directory string, err error) {
	if !filepath.IsAbs(parent) {
		return "", errors.New("无法选择保存位置。")
	}
	dir, err := os.MkdirTemp(parent, "Yaare-Recovery-")
	if err != nil {
		return "", err
	}
	root := func() (string, error) { return dir, nil }
	store := sqlite.New(root)
	defer func() {
		closeErr := store.Close()
		if err == nil && closeErr != nil {
			err = closeErr
			directory = ""
		}
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	if err = store.Initialize(ctx); err != nil {
		return "", err
	}
	identity := workspaceProtector(store, root, base)
	portable := syncapp.NewPortableWorkspace(store, identity)
	probe, err := portable.Validate(ctx, payload)
	if err != nil {
		return "", err
	}
	if err = sqlite.ValidateCloudData(probe); err != nil {
		return "", err
	}
	data, err := portable.Materialize(ctx, payload)
	if err != nil {
		return "", err
	}
	if err = store.RestoreCloud(ctx, 0, data, sqlite.IntegrationPause{}); err != nil {
		return "", err
	}
	return dir, nil
}
