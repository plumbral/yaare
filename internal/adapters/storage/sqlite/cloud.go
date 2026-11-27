package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"yaare/internal/domain/preferences"
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
)

func (s *Store) CloudRead(ctx context.Context) (syncmodel.LocalData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out syncmodel.LocalData
	if s.db == nil {
		return out, s.unavailable()
	}
	var e error
	out.Snapshot, _, e = readSnapshot(ctx, s.db)
	if e != nil {
		return out, e
	}
	out.Keys, e = readKeys(ctx, s.db)
	if e != nil {
		return out, e
	}
	out.HostCredentials, e = readHostCredentials(ctx, s.db)
	if e != nil {
		return out, e
	}
	out.ProxyCredentials, e = readCredentials(ctx, s.db, "SELECT host_id,scope,cipher FROM proxy_credentials ORDER BY host_id")
	if e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, "SELECT id,name,username,authentication,COALESCE(key_id,''),version,cipher,use_agent FROM identities ORDER BY id")
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var i workspace.SealedIdentity
		if e = rows.Scan(&i.ID, &i.Name, &i.Username, &i.Authentication, &i.KeyID, &i.Version, &i.Cipher, &i.UseAgent); e != nil {
			break
		}
		out.Identities = append(out.Identities, i)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return out, e
	}
	out.Preferences, e = readPortablePreferences(ctx, s.db)
	if e != nil {
		return out, e
	}
	var raw string
	e = s.db.QueryRowContext(ctx, "SELECT data FROM portable_extensions WHERE id=1").Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e == nil {
		if len(raw) > 128<<10 || json.Unmarshal([]byte(raw), &out.Extensions) != nil || out.Extensions == nil {
			return out, errors.New("本机配置扩展无法读取，原数据已保留。")
		}
		e = workspace.ValidatePortableExtensions(out.Extensions)
	}
	return out, e
}

func ValidateCloudData(next syncmodel.LocalData) error {
	if len(next.Keys) > workspace.MaxSavedKeys {
		return workspace.Fail("KEY_LIMIT", "最多保存 256 个密钥引用。")
	}
	if err := workspace.ValidatePortableExtensions(next.Extensions); err != nil {
		return err
	}
	if next.Preferences != nil {
		if _, err := preferences.DecodePortable(next.Preferences); err != nil {
			return err
		}
	}
	ids := map[string]bool{}
	total := 0
	for _, k := range next.Keys {
		if ids[k.ID] || !workspace.ValidKey(k) {
			return errors.New("同步密钥记录无效。")
		}
		ids[k.ID] = true
		total += len(k.Cipher)
	}
	if total > workspace.MaxTotalKeyCipherBytes {
		return errors.New("同步密钥超过 16 MiB。")
	}
	if e := workspace.Validate(next.Snapshot, ids); e != nil {
		return e
	}
	identities := map[string]bool{}
	for _, i := range next.Identities {
		if identities[i.ID] || !workspace.ValidIdentity(i.Identity) || len(i.Cipher) == 0 || len(i.Cipher) > 2<<20 || i.KeyID != "" && !ids[i.KeyID] {
			return errors.New("同步身份记录无效。")
		}
		identities[i.ID] = true
	}
	if len(identities) > 256 {
		return errors.New("同步身份过多。")
	}
	for _, h := range next.Snapshot.Hosts {
		if h.IdentityID != "" && !identities[h.IdentityID] {
			return errors.New("同步主机引用了不存在的身份。")
		}
	}
	hosts := map[string]workspace.Host{}
	for _, h := range next.Snapshot.Hosts {
		hosts[h.ID] = h
	}
	seenCredentials := map[string]bool{}
	credentialBytes := 0
	for _, c := range next.HostCredentials {
		if seenCredentials[c.HostID] || !workspace.CredentialMatches(hosts[c.HostID], c) {
			return errors.New("连接认证记录无效。")
		}
		seenCredentials[c.HostID] = true
		credentialBytes += len(c.Cipher)
	}
	if credentialBytes > 16<<20 {
		return errors.New("连接认证记录无效。")
	}
	proxyIDs := map[string]bool{}
	proxyBytes := 0
	for _, c := range next.ProxyCredentials {
		if proxyIDs[c.HostID] || !workspace.ProxyCredentialMatches(hosts[c.HostID], c) {
			return errors.New("连接认证记录无效。")
		}
		proxyIDs[c.HostID] = true
		proxyBytes += len(c.Cipher)
	}
	if proxyBytes > 16<<20 {
		return errors.New("连接认证记录无效。")
	}
	return nil
}

type IntegrationPause struct {
	Version int64
}

func (s *Store) CloudReplace(ctx context.Context, expected int64, next syncmodel.LocalData) error {
	return storageWriteError(s.replaceCloud(ctx, expected, next, nil, nil), nil)
}
func (s *Store) RestoreCloud(ctx context.Context, expected int64, next syncmodel.LocalData, pause IntegrationPause) error {
	return storageWriteError(s.replaceCloud(ctx, expected, next, &pause, nil), nil)
}
func (s *Store) replaceCloud(ctx context.Context, expected int64, next syncmodel.LocalData, pause *IntegrationPause, record *SyncRecord) error {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return s.unavailable()
	}
	if err := ValidateCloudData(next); err != nil {
		return err
	}
	old, _, _, e := readSnapshotFast(ctx, s.db)
	if e != nil {
		return e
	}
	if old.Revision != expected {
		return errors.New("本机配置在预览后有变化，请重新检查差异。")
	}
	var stateHash string
	if record != nil {
		if len(record.Settings) == 0 || len(record.Settings) > 65536 || len(record.State) > 24<<20 {
			return errors.New("同步配置超过大小限制。")
		}
		stateHash, e = s.writeBaselineFile(ctx, record.State)
		if e != nil {
			return e
		}
	}
	now := s.now().UTC()
	if e = s.checkpointLocked(ctx, now); e != nil {
		return workspace.Fail("BACKUP_FAILED", "无法创建替换前的安全备份，配置未更改。")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if record != nil {
		if e = writeSync(ctx, tx, *record, stateHash); e != nil {
			return e
		}
	}
	if pause != nil && record == nil {
		var version int64
		err := tx.QueryRowContext(ctx, "SELECT version FROM config_sync WHERE id=1").Scan(&version)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if version != pause.Version {
			return errors.New("同步设置在导入预览后改变，请重新检查。")
		}
		if _, e = tx.ExecContext(ctx, "UPDATE config_sync SET paused=1,version=version+1 WHERE id=1"); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); e != nil {
		return e
	}
	for _, query := range []string{"DELETE FROM proxy_credentials", "DELETE FROM host_credentials", "DELETE FROM identities", "DELETE FROM keys"} {
		if _, e = tx.ExecContext(ctx, query); e != nil {
			return e
		}
	}
	for _, k := range next.Keys {
		var cipher any
		if len(k.Cipher) > 0 {
			cipher = k.Cipher
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO keys(id,name,format,path,cipher) VALUES(?,?,?,?,?)", k.ID, k.Name, k.Format, k.Path, cipher); e != nil {
			return e
		}
	}
	for _, i := range next.Identities {
		var key any
		if i.KeyID != "" {
			key = i.KeyID
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO identities(id,name,username,authentication,key_id,version,cipher,use_agent) VALUES(?,?,?,?,?,?,?,?)", i.ID, i.Name, i.Username, i.Authentication, key, i.Version, i.Cipher, i.UseAgent); e != nil {
			return e
		}
	}
	if _, e = writeDiff(ctx, tx, old, next.Snapshot); e != nil {
		return e
	}
	for _, c := range next.HostCredentials {
		if _, e = tx.ExecContext(ctx, "INSERT INTO host_credentials(host_id,scope,cipher) VALUES(?,?,?)", c.HostID, c.Scope, c.Cipher); e != nil {
			return e
		}
	}
	for _, c := range next.ProxyCredentials {
		if _, e = tx.ExecContext(ctx, "INSERT INTO proxy_credentials VALUES(?,?,?)", c.HostID, c.Scope, c.Cipher); e != nil {
			return e
		}
	}
	if e = writePortablePreferences(ctx, tx, next.Preferences); e != nil {
		return e
	}
	if next.Extensions != nil {
		raw, _ := json.Marshal(next.Extensions)
		if _, e = tx.ExecContext(ctx, "INSERT INTO portable_extensions(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", string(raw)); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE meta SET revision=revision+1 WHERE id=1"); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if record != nil {
		s.pruneBaselines(stateHash)
	}
	s.scheduleRoutineBackup(now)
	s.notifySyncLocked(true)
	return nil
}

var _ queryer = (*sql.Tx)(nil)
