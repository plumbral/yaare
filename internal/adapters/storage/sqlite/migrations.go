package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"sync"

	"yaare/internal/domain/workspace"
)

const appID = 0x59414152

//go:embed schema/0001_initial.sql
var initialSchema string

//go:embed schema/0002_credential_and_sync_recovery.sql
var recoverySchema string

var migrations = [...]string{initialSchema, recoverySchema}

const schemaVersion = len(migrations)

type schemaSignature struct{ checksum, catalog string }

func catalogDefinitions(ctx context.Context, q queryer) (string, error) {
	var definitions string
	err := q.QueryRowContext(ctx, `SELECT coalesce(group_concat(sql, char(0)), '') FROM (
		SELECT sql FROM sqlite_schema WHERE sql IS NOT NULL AND name NOT GLOB 'sqlite_*' ORDER BY type, name
	)`).Scan(&definitions)
	return definitions, err
}

var schemaSignatures = sync.OnceValues(func() ([schemaVersion]schemaSignature, error) {
	var signatures [schemaVersion]schemaSignature
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return signatures, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	hash := sha256.New()
	for i, script := range migrations {
		if _, err = db.Exec(script); err != nil {
			return signatures, err
		}
		catalog, err := catalogDefinitions(context.Background(), db)
		if err != nil {
			return signatures, err
		}
		hash.Write([]byte(script))
		signatures[i] = schemaSignature{hex.EncodeToString(hash.Sum(nil)), catalog}
	}
	return signatures, nil
})

func databaseVersion(ctx context.Context, db queryer) (int, error) {
	var id, version int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&id); err != nil {
		return 0, err
	}
	if id == appID {
		if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return 0, err
		}
		if version >= 1 && version <= schemaVersion {
			return version, nil
		}
		if version > schemaVersion {
			return 0, workspace.Fail("NEWER_VERSION", "配置由更高版本的 Yaare 创建，请使用新版程序，当前版本不会写入。")
		}
	}
	return 0, workspace.Fail("UNSUPPORTED_FORMAT", "配置数据库格式不受支持，原文件已保留。")
}

func validateCatalog(ctx context.Context, db queryer, version int) error {
	signatures, err := schemaSignatures()
	if err != nil {
		return err
	}
	want := signatures[version-1]
	var checksum string
	if err := db.QueryRowContext(ctx, "SELECT checksum FROM schema_info WHERE id=1").Scan(&checksum); err != nil || checksum != want.checksum {
		return workspace.Fail("CONFIG_CORRUPT", "配置数据库结构校验失败。")
	}
	if catalog, err := catalogDefinitions(ctx, db); err != nil || catalog != want.catalog {
		return workspace.Fail("CONFIG_CORRUPT", "配置数据库结构校验失败。")
	}
	return nil
}

func (s *Store) prepareSchema(ctx context.Context, db *sql.DB, fresh bool) error {
	if fresh {
		return applyMigrations(ctx, db, 0)
	}
	version, err := databaseVersion(ctx, db)
	if err != nil {
		return err
	}
	if version == schemaVersion {
		_, _, err = validateDB(ctx, db, false)
		return err
	}
	if err = validateCatalog(ctx, db, version); err != nil {
		return err
	}
	if err = s.createBackupFromLive(ctx, db, s.dir, s.now().UTC(), true); err != nil {
		return workspace.Fail("BACKUP_FAILED", "无法保留升级前的数据库，原文件已保留。")
	}
	if err = applyMigrations(ctx, db, version); err != nil {
		return workspace.Fail("MIGRATION_FAILED", "配置数据库升级未完成，原数据和升级前备份已保留。")
	}
	return nil
}

func applyMigrations(ctx context.Context, db *sql.DB, from int) error {
	signatures, err := schemaSignatures()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, script := range migrations[from:] {
		if _, err = tx.ExecContext(ctx, script); err != nil {
			return err
		}
	}
	if from == 0 {
		if _, err = writeDiff(ctx, tx, workspace.Snapshot{}, workspace.Empty()); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO schema_info(id,checksum) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET checksum=excluded.checksum", signatures[schemaVersion-1].checksum); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", appID, schemaVersion)); err != nil {
		return err
	}
	if _, _, err = validateDB(ctx, tx, false); err != nil {
		return err
	}
	return tx.Commit()
}
