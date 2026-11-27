package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"yaare/internal/domain/workspace"
)

func readHostCredentialMetadata(ctx context.Context, q queryer) ([]workspace.HostCredential, error) {
	return readCredentialMetadata(ctx, q, "SELECT host_id,scope,length(cipher) FROM host_credentials ORDER BY host_id")
}

func readCredentialMetadata(ctx context.Context, q queryer, query string) ([]workspace.HostCredential, error) {
	rows, e := q.QueryContext(ctx, query)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []workspace.HostCredential{}
	for rows.Next() {
		var c workspace.HostCredential
		var length int
		if e = rows.Scan(&c.HostID, &c.Scope, &length); e != nil {
			return nil, e
		}
		if length < 1 || length > 65536 || len(out) >= 10000 {
			return nil, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
		}
		c.Cipher = []byte{1}
		out = append(out, c)
	}
	return out, rows.Err()
}

func readHostCredentials(ctx context.Context, q queryer) ([]workspace.HostCredential, error) {
	return readCredentials(ctx, q, "SELECT host_id,scope,cipher FROM host_credentials ORDER BY host_id")
}

func readCredentials(ctx context.Context, q queryer, query string) ([]workspace.HostCredential, error) {
	rows, e := q.QueryContext(ctx, query)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []workspace.HostCredential{}
	for rows.Next() {
		var c workspace.HostCredential
		if e = rows.Scan(&c.HostID, &c.Scope, &c.Cipher); e != nil {
			return nil, e
		}
		out = append(out, c)
		if len(out) > 10000 {
			return nil, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
		}
	}
	return out, rows.Err()
}

func (s *Store) HostCredential(ctx context.Context, id string) (workspace.HostCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.HostCredential{}, s.unavailable()
	}
	var c workspace.HostCredential
	e := s.db.QueryRowContext(ctx, "SELECT host_id,scope,cipher FROM host_credentials WHERE host_id=?", id).Scan(&c.HostID, &c.Scope, &c.Cipher)
	if errors.Is(e, sql.ErrNoRows) {
		return workspace.HostCredential{}, workspace.Fail("CREDENTIAL_UNAVAILABLE", "此连接未保存密码，请重新输入。")
	}
	if e != nil {
		return workspace.HostCredential{}, workspace.Fail("STORAGE_READ_FAILED", "无法读取已保存的认证，请检查工作区存储后重试。")
	}
	return c, nil
}

func (s *Store) ProxyCredential(ctx context.Context, id string) (workspace.HostCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.HostCredential{}, s.unavailable()
	}
	var c workspace.HostCredential
	err := s.db.QueryRowContext(ctx, "SELECT host_id,scope,cipher FROM proxy_credentials WHERE host_id=?", id).Scan(&c.HostID, &c.Scope, &c.Cipher)
	if err == sql.ErrNoRows {
		return workspace.HostCredential{}, nil
	}
	return c, err
}

func (s *Store) CommitConnection(ctx context.Context, revision int64, next workspace.Snapshot, c workspace.HostCredentialChange) (workspace.Result, error) {
	return s.commit(ctx, revision, next, &c, false)
}
