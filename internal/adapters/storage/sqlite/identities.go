package sqlite

import (
	"context"
	"database/sql"

	"yaare/internal/domain/workspace"
)

func (s *Store) Identities(ctx context.Context) ([]workspace.Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, s.unavailable()
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,username,authentication,COALESCE(key_id,''),version,use_agent FROM identities ORDER BY name,id")
	if err != nil {
		return nil, workspace.Fail("IDENTITY_READ", "无法读取身份。")
	}
	defer rows.Close()
	result := []workspace.Identity{}
	for rows.Next() {
		var i workspace.Identity
		if err = rows.Scan(&i.ID, &i.Name, &i.Username, &i.Authentication, &i.KeyID, &i.Version, &i.UseAgent); err != nil {
			return nil, err
		}
		if !workspace.ValidIdentity(i) || len(result) >= 256 {
			return nil, workspace.Fail("IDENTITY_INVALID", "身份记录无效。")
		}
		result = append(result, i)
	}
	return result, rows.Err()
}

func (s *Store) Identity(ctx context.Context, id string) (workspace.SealedIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var i workspace.SealedIdentity
	if s.db == nil {
		return i, s.unavailable()
	}
	err := s.db.QueryRowContext(ctx, "SELECT id,name,username,authentication,COALESCE(key_id,''),version,cipher,use_agent FROM identities WHERE id=?", id).Scan(&i.ID, &i.Name, &i.Username, &i.Authentication, &i.KeyID, &i.Version, &i.Cipher, &i.UseAgent)
	if err != nil || !workspace.ValidIdentity(i.Identity) || len(i.Cipher) > 2*1024*1024 {
		return workspace.SealedIdentity{}, workspace.Fail("IDENTITY_UNAVAILABLE", "身份不可用，请重新选择或编辑。")
	}
	return i, nil
}

func (s *Store) SaveIdentity(ctx context.Context, i workspace.SealedIdentity, remove bool) error {
	return s.saveIdentity(ctx, i, remove, nil)
}

func (s *Store) SaveIdentityWithKey(ctx context.Context, i workspace.SealedIdentity, key workspace.SavedKey) error {
	return s.saveIdentity(ctx, i, false, &key)
}

func (s *Store) saveIdentity(ctx context.Context, i workspace.SealedIdentity, remove bool, addedKey *workspace.SavedKey) error {
	if remove || !s.hasUnchangedBackup() {
		s.vacuumMu.Lock()
		defer s.vacuumMu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return s.unavailable()
	}
	if !workspace.ValidIdentity(i.Identity) || (!remove && (len(i.Cipher) == 0 || len(i.Cipher) > 2*1024*1024)) {
		return workspace.Fail("INVALID_IDENTITY", "身份内容无效。")
	}
	if addedKey != nil {
		keys, totalCipher, err := readKeyMetadata(ctx, s.db)
		if err != nil {
			return err
		}
		if !workspace.ValidKey(*addedKey) || addedKey.ID != i.KeyID || addedKey.Path != "" || len(keys) >= workspace.MaxSavedKeys {
			return workspace.Fail("KEY_LIMIT", "无法保存密钥。")
		}
		if totalCipher+len(addedKey.Cipher) > workspace.MaxTotalKeyCipherBytes {
			return workspace.Fail("KEY_LIMIT", "已保存密钥内容达到容量上限，请先删除不用的密钥。")
		}
	}
	var version int64
	err := s.db.QueryRowContext(ctx, "SELECT version FROM identities WHERE id=?", i.ID).Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		return workspace.Fail("IDENTITY_READ", "无法读取身份。")
	}
	if version != i.Version || (remove && err == sql.ErrNoRows) {
		return workspace.Fail("IDENTITY_CONFLICT", "身份已变更，请重新载入后再编辑。")
	}
	if remove {
		var inUse bool
		if err = s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM hosts WHERE identity_id=?)", i.ID).Scan(&inUse); err != nil {
			return err
		}
		if inUse {
			return workspace.Fail("IDENTITY_IN_USE", "请先解除主机与此身份的关联。")
		}
	}
	var count int
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM identities").Scan(&count); err != nil {
		return err
	}
	if version == 0 && count >= 256 {
		return workspace.Fail("IDENTITY_LIMIT", "最多保存 256 个身份。")
	}
	now := s.now().UTC()
	if remove {
		err = s.checkpointLocked(ctx, now)
	} else {
		err = s.prepareBackupLocked(ctx, now)
	}
	if err != nil {
		return workspace.Fail("BACKUP_FAILED", "身份备份失败，未保存。")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if addedKey != nil {
		if _, err = tx.ExecContext(ctx, "INSERT INTO keys(id,name,format,path,cipher) VALUES(?,?,?,?,?)", addedKey.ID, addedKey.Name, addedKey.Format, "", addedKey.Cipher); err != nil {
			return storageWriteError(err, workspace.Fail("KEY_SAVE_FAILED", "无法保存密钥。"))
		}
	}
	var key any
	if i.KeyID != "" {
		key = i.KeyID
	}
	if remove {
		_, err = tx.ExecContext(ctx, "DELETE FROM identities WHERE id=?", i.ID)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO identities(id,name,username,authentication,key_id,version,cipher,use_agent) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,username=excluded.username,authentication=excluded.authentication,key_id=excluded.key_id,version=excluded.version,cipher=excluded.cipher,use_agent=excluded.use_agent", i.ID, i.Name, i.Username, i.Authentication, key, i.Version+1, i.Cipher, i.UseAgent)
	}
	if err != nil {
		return storageWriteError(err, workspace.Fail("IDENTITY_SAVE", "保存身份失败，请检查密钥引用。"))
	}
	if _, err = tx.ExecContext(ctx, "UPDATE meta SET revision=revision+1 WHERE id=1"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return storageWriteError(err, workspace.Fail("IDENTITY_SAVE", "身份提交失败，请重新载入确认。"))
	}
	s.notifyLocalChangeLocked()
	s.scheduleRoutineBackup(now)
	return nil
}
