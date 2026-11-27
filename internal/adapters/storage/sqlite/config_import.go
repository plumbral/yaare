package sqlite

import (
	"context"
	"strings"
	"yaare/internal/adapters/keyfile"
	"yaare/internal/domain/workspace"
)

func (s *Store) ImportHosts(ctx context.Context, revision int64, hosts []workspace.Host, added []workspace.SavedKey) (workspace.Result, error) {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.Result{}, s.unavailable()
	}
	old, keys, _, err := readSnapshotFast(ctx, s.db)
	if err != nil {
		return workspace.Result{}, workspace.Fail("STORAGE_READ_FAILED", "无法读取现有配置，未导入。")
	}
	if old.Revision != revision {
		return workspace.Result{}, workspace.Fail("CONFLICT", "配置在预览后已变化，请重新读取 SSH Config。")
	}
	if len(hosts) == 0 {
		return workspace.Result{}, workspace.Fail("IMPORT_EMPTY", "请选择至少一个可导入的主机。")
	}
	remap := map[string]string{}
	fresh := []workspace.SavedKey{}
	for _, key := range added {
		if !workspace.ValidKey(key) || key.Path == "" || len(key.Cipher) > 0 {
			return workspace.Result{}, workspace.Fail("INVALID_KEY_REFERENCE", "导入的密钥引用无效。")
		}
		for _, existing := range keys {
			if keyfile.SamePath(existing.Path, key.Path) {
				remap[key.ID] = existing.ID
				break
			}
		}
		if remap[key.ID] == "" {
			fresh = append(fresh, key)
			keys = append(keys, keyMeta{ID: key.ID, Name: key.Name, Format: key.Format, Path: key.Path})
		}
	}
	if len(keys) > workspace.MaxSavedKeys {
		return workspace.Result{}, workspace.Fail("KEY_LIMIT", "最多保存 256 个密钥引用。")
	}
	next := old
	next.Hosts = append([]workspace.Host{}, old.Hosts...)
	for _, host := range hosts {
		for _, existing := range next.Hosts {
			if strings.EqualFold(existing.Name, host.Name) {
				return workspace.Result{}, workspace.Fail("IMPORT_DUPLICATE", "存在同名主机，请重新预览后跳过重复项。")
			}
		}
		if id := remap[host.KeyID]; id != "" {
			host.KeyID = id
		}
		next.Hosts = append(next.Hosts, host)
	}
	keyIDs := make(map[string]bool, len(keys))
	for _, key := range keys {
		keyIDs[key.ID] = true
	}
	if err = workspace.Validate(next, keyIDs); err != nil {
		return workspace.Result{}, err
	}
	now := s.now().UTC()
	if err = s.checkpointLocked(ctx, now); err != nil {
		return workspace.Result{}, workspace.Fail("BACKUP_FAILED", "无法创建安全快照，未导入。")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.Result{}, workspace.Fail("SAVE_FAILED", "无法开始导入。")
	}
	defer tx.Rollback()
	for _, key := range fresh {
		if _, err = tx.ExecContext(ctx, "INSERT INTO keys(id,name,format,path,cipher) VALUES(?,?,?,?,NULL)", key.ID, key.Name, key.Format, key.Path); err != nil {
			return workspace.Result{}, storageWriteError(err, workspace.Fail("SAVE_FAILED", "密钥引用保存失败，未导入主机。"))
		}
	}
	if _, err = writeDiff(ctx, tx, old, next); err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE meta SET revision=revision+1 WHERE id=1")
	}
	if err != nil {
		return workspace.Result{}, storageWriteError(err, workspace.Fail("SAVE_FAILED", "导入失败，原有配置未更改。"))
	}
	if err = tx.Commit(); err != nil {
		return workspace.Result{}, storageWriteError(err, workspace.Fail("SAVE_FAILED", "导入提交未确认，请重新读取配置核对。"))
	}
	next.Revision++
	s.notifyLocalChangeLocked()
	s.scheduleRoutineBackup(now)
	return workspace.Result{Snapshot: next, Warning: s.warning}, nil
}
