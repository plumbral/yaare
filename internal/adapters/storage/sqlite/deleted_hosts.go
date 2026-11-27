package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"yaare/internal/domain/workspace"
)

func (s *Store) DeletedHosts(ctx context.Context) ([]workspace.DeletedHost, error) {
	s.mu.Lock()
	if s.db == nil {
		err := s.unavailable()
		s.mu.Unlock()
		return nil, err
	}
	dir := s.dir
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM hosts")
	if err != nil {
		s.mu.Unlock()
		return nil, workspace.Fail("STORAGE_READ_FAILED", "无法读取现有主机配置。")
	}
	seen := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		seen[id] = true
	}
	if rowErr := rows.Err(); err == nil {
		err = rowErr
	}
	rows.Close()
	s.mu.Unlock()
	if err != nil {
		return nil, workspace.Fail("STORAGE_READ_FAILED", "无法读取现有主机配置。")
	}
	files, err := backupFilesIn(dir)
	if err != nil {
		return nil, workspace.Fail("BACKUP_UNAVAILABLE", "无法读取本机备份。")
	}
	result := []workspace.DeletedHost{}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cached, ok := s.verifyBackupCached(ctx, file.path, true, true)
		if !ok {
			continue
		}
		for _, host := range cached.hosts {
			if !seen[host.ID] {
				seen[host.ID] = true
				result = append(result, workspace.DeletedHost{Host: host, BackupID: file.id, Created: file.created.Format(time.RFC3339Nano)})
			}
		}
	}
	return result, ctx.Err()
}

func (s *Store) RestoreDeletedHost(ctx context.Context, backupID, hostID string, revision int64) (workspace.Result, error) {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.Result{}, s.unavailable()
	}
	current, keysMeta, totalKeyCipher, err := readSnapshotFast(ctx, s.db)
	if err != nil {
		return workspace.Result{}, workspace.Fail("STORAGE_READ_FAILED", "无法读取现有主机配置，未恢复。")
	}
	if current.Revision != revision {
		return workspace.Result{}, workspace.Fail("CONFLICT", "配置已变更，请刷新后再恢复。")
	}
	if findHost(current.Hosts, hostID).ID != "" {
		return workspace.Result{}, workspace.Fail("HOST_EXISTS", "此主机已存在，不会覆盖当前配置。")
	}
	files, err := s.backupFiles()
	if err != nil {
		return workspace.Result{}, workspace.Fail("BACKUP_UNAVAILABLE", "无法读取本机备份，未恢复。")
	}
	for _, file := range files {
		if file.id == backupID {
			return s.restoreFromBackup(ctx, file, hostID, current, keysMeta, totalKeyCipher)
		}
	}
	return workspace.Result{}, workspace.Fail("BACKUP_NOT_FOUND", "该备份已不在保留列表中，请刷新后重试。")
}

func (s *Store) restoreFromBackup(ctx context.Context, file backupFile, hostID string, current workspace.Snapshot, keysMeta []keyMeta, totalKeyCipher int) (workspace.Result, error) {
	invalid := workspace.Fail("INVALID_BACKUP", "备份未通过校验，未恢复主机。")
	backup, err := openBackup(file.path)
	if err != nil {
		return workspace.Result{}, invalid
	}
	defer backup.Close()
	snapshot, _, err := verifyBackup(ctx, backup, true)
	if err != nil {
		return workspace.Result{}, invalid
	}
	host := findHost(snapshot.Hosts, hostID)
	if host.ID == "" {
		return workspace.Result{}, workspace.Fail("HOST_NOT_IN_BACKUP", "备份中没有此主机，请刷新后重试。")
	}
	if !slices.Contains(current.Groups, host.Group) {
		return workspace.Result{}, workspace.Fail("RESTORE_GROUP_UNAVAILABLE", "原分组不存在，请先恢复该分组。")
	}
	if err := checkRecoveryAuth(ctx, s.db, backup, host); err != nil {
		return workspace.Result{}, err
	}
	for _, id := range host.JumpHostIDs {
		jump := findHost(current.Hosts, id)
		prior := findHost(snapshot.Hosts, id)
		if jump.ID == "" || workspace.CredentialScope(jump) != workspace.CredentialScope(prior) {
			return workspace.Result{}, workspace.Fail("RESTORE_JUMP_UNAVAILABLE", "原跳板机不存在或连接设置已改变，未恢复主机。")
		}
		if err := checkRecoveryAuth(ctx, s.db, backup, jump); err != nil {
			return workspace.Result{}, err
		}
	}
	change := workspace.HostCredentialChange{HostID: host.ID, Action: "keep"}
	if host.HasCredential {
		change.Action = "replace"
		change.Credential, err = readRecoveryCredential(ctx, backup, "SELECT scope,cipher FROM host_credentials WHERE host_id=?", host.ID)
		if err != nil || !workspace.CredentialMatches(host, change.Credential) {
			return workspace.Result{}, invalid
		}
	}
	proxy, err := readRecoveryCredential(ctx, backup, "SELECT scope,cipher FROM proxy_credentials WHERE host_id=?", host.ID)
	if err == nil {
		if !workspace.ProxyCredentialMatches(host, proxy) {
			return workspace.Result{}, invalid
		}
		change.Proxy = &proxy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return workspace.Result{}, invalid
	}
	if err := backup.Close(); err != nil {
		return workspace.Result{}, invalid
	}
	next := current
	next.Hosts = append(append([]workspace.Host(nil), current.Hosts...), host)
	result, err := s.commitPreparedLocked(ctx, current, next, keysMeta, totalKeyCipher, &change, false, false)
	if err != nil {
		var known *workspace.Error
		if errors.As(err, &known) {
			return workspace.Result{}, known
		}
		return workspace.Result{}, workspace.Fail("SAVE_FAILED", "无法恢复主机，原有配置未更改。")
	}
	result.EntityID = host.ID
	return result, nil
}

func findHost(hosts []workspace.Host, id string) workspace.Host {
	for _, host := range hosts {
		if host.ID == id {
			return host
		}
	}
	return workspace.Host{}
}

func checkRecoveryAuth(ctx context.Context, current, backup queryer, host workspace.Host) error {
	keyIDs := []string{host.KeyID}
	if host.IdentityID != "" {
		live, liveErr := readRecoveryIdentity(ctx, current, host.IdentityID)
		prior, priorErr := readRecoveryIdentity(ctx, backup, host.IdentityID)
		if liveErr != nil || priorErr != nil || live.Username != prior.Username || live.Authentication != prior.Authentication ||
			live.KeyID != prior.KeyID || live.UseAgent != prior.UseAgent || !bytes.Equal(live.Cipher, prior.Cipher) {
			return workspace.Fail("RESTORE_IDENTITY_UNAVAILABLE", "原认证身份不存在或认证资料已改变，未恢复主机。")
		}
		keyIDs = append(keyIDs, live.KeyID)
	}
	for _, id := range keyIDs {
		if id != "" && !sameRecoveryKey(ctx, current, backup, id) {
			return workspace.Fail("RESTORE_KEY_UNAVAILABLE", "原密钥引用不存在或密钥资料已改变，未恢复主机。")
		}
	}
	return nil
}

func sameRecoveryKey(ctx context.Context, current, backup queryer, id string) bool {
	var live, prior workspace.SavedKey
	if err := current.QueryRowContext(ctx, "SELECT id,name,format,path,cipher FROM keys WHERE id=?", id).Scan(&live.ID, &live.Name, &live.Format, &live.Path, &live.Cipher); err != nil {
		return false
	}
	if err := backup.QueryRowContext(ctx, "SELECT id,name,format,path,cipher FROM keys WHERE id=?", id).Scan(&prior.ID, &prior.Name, &prior.Format, &prior.Path, &prior.Cipher); err != nil {
		return false
	}
	return live.Path == prior.Path && live.Format == prior.Format && bytes.Equal(live.Cipher, prior.Cipher)
}

func readRecoveryIdentity(ctx context.Context, db queryer, id string) (workspace.SealedIdentity, error) {
	var result workspace.SealedIdentity
	err := db.QueryRowContext(ctx, "SELECT id,name,username,authentication,COALESCE(key_id,''),version,cipher,use_agent FROM identities WHERE id=?", id).
		Scan(&result.ID, &result.Name, &result.Username, &result.Authentication, &result.KeyID, &result.Version, &result.Cipher, &result.UseAgent)
	if err == nil && (!workspace.ValidIdentity(result.Identity) || len(result.Cipher) == 0 || len(result.Cipher) > 2<<20) {
		err = workspace.Fail("IDENTITY_INVALID", "身份记录无效。")
	}
	return result, err
}

func readRecoveryCredential(ctx context.Context, db queryer, query, id string) (workspace.HostCredential, error) {
	result := workspace.HostCredential{HostID: id}
	err := db.QueryRowContext(ctx, query, id).Scan(&result.Scope, &result.Cipher)
	return result, err
}
