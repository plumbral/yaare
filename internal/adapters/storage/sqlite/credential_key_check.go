package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"

	"yaare/internal/adapters/secrets"
	"yaare/internal/app/vault"
	"yaare/internal/domain/workspace"
)

const keyCheckPurpose = "workspace-credential-key-v1"

func (s *Store) VerifyCredentialKey(p secrets.Protector, checkFiles func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return s.unavailable()
	}
	ctx := context.Background()
	var marker []byte
	err := s.db.QueryRowContext(ctx, "SELECT cipher FROM credential_key_check WHERE id=1").Scan(&marker)
	if err == nil {
		raw, err := p.Unprotect(marker, keyCheckPurpose)
		defer clear(raw)
		if err != nil || !bytes.Equal(raw, []byte(keyCheckPurpose)) {
			return secrets.ErrWorkspaceKey
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = verifyLegacyCredentials(ctx, s.db, p); err != nil {
		return err
	}
	if checkFiles != nil {
		if err = checkFiles(); err != nil {
			return secrets.ErrWorkspaceKey
		}
	}
	marker, err = p.Protect([]byte(keyCheckPurpose), keyCheckPurpose)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO credential_key_check(id,cipher) VALUES(1,?)", marker)
	return err
}

func verifyLegacyCredentials(ctx context.Context, q queryer, p secrets.Protector) error {
	rows, err := q.QueryContext(ctx, `SELECT 'key',id,'',cipher FROM keys WHERE cipher IS NOT NULL
		UNION ALL SELECT 'identity',id,'',cipher FROM identities
		UNION ALL SELECT 'host',host_id,scope,cipher FROM host_credentials
		UNION ALL SELECT 'proxy',host_id,scope,cipher FROM proxy_credentials
		UNION ALL SELECT 'sync','','',settings FROM config_sync`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id, scope string
		var cipher []byte
		if err = rows.Scan(&kind, &id, &scope, &cipher); err != nil {
			return err
		}
		purpose := id
		switch kind {
		case "key":
			purpose = "key-v1:" + id
		case "host":
			purpose = vault.HostCredentialPurpose(workspace.HostCredential{HostID: id, Scope: scope})
		case "proxy":
			purpose = vault.ProxyCredentialPurpose(workspace.HostCredential{HostID: id, Scope: scope})
		case "sync":
			purpose = "config-sync-v1"
		}
		raw, err := p.Unprotect(cipher, purpose)
		clear(raw)
		if err != nil {
			return secrets.ErrWorkspaceKey
		}
	}
	return rows.Err()
}
