package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
	"yaare/internal/adapters/keyfile"
	"yaare/internal/domain/workspace"
)

type Store struct {
	vacuumMu sync.Mutex
	mu       sync.Mutex

	root                 func() (string, error)
	dir                  string
	db                   *sql.DB
	lease                *os.File
	problem              *workspace.Error
	warning              string
	now                  func() time.Time
	systems              map[string]systemObservation
	syncListeners        map[chan struct{}]struct{}
	localChangeListeners map[chan struct{}]struct{}
	syncApplied          uint64
	dbEpoch              uint64
	baselineHash         string
	baselineInfo         os.FileInfo

	cacheMu     sync.Mutex
	lastBackup  verifiedBackup
	lastRoutine verifiedBackup
	backupCache map[string]cachedBackup

	backupStateMu sync.Mutex
	backupCond    *sync.Cond
	backupDone    chan struct{}
	backupTimer   *time.Timer
	backupDue     time.Time
	backupQueued  bool
	pruneQueued   bool
	backupRunning bool
	backupClosed  bool
	backupOnClose bool
	backupAt      time.Time
}

func New(root func() (string, error)) *Store {
	return &Store{root: root, now: time.Now, problem: &workspace.Error{Code: "NOT_READY", Message: "配置存储尚未初始化。"}}
}

func (s *Store) Initialize(ctx context.Context) error {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialize(ctx)
}

func (s *Store) Close() error {
	s.stopBackupWorker()
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.db != nil {
		s.backupStateMu.Lock()
		pending := s.backupOnClose
		s.backupStateMu.Unlock()
		if pending {
			_, err = s.runRoutineBackupInternal(context.Background(), s.now().UTC(), true, true)
		}
		err = errors.Join(err, s.db.Close())
		s.db = nil
	}
	if s.lease != nil {
		err = errors.Join(err, s.lease.Close())
		s.lease = nil
	}
	return err
}

func (s *Store) fail(code, message string) error {
	s.problem = &workspace.Error{Code: code, Message: message}
	return s.problem
}

const maxWorkspaceBytes = 1 << 30
const oversizedDatabaseMessage = "配置数据库超过 1 GiB，原文件已保留。"

func checkPageCapacity(ctx context.Context, db *sql.DB) error {
	var bytes int64
	if err := db.QueryRowContext(ctx, "SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()").Scan(&bytes); err != nil {
		return err
	}
	if bytes > maxWorkspaceBytes {
		return workspace.Fail("DATABASE_TOO_LARGE", oversizedDatabaseMessage)
	}
	return nil
}

func storageWriteError(cause, fallback error) error {
	var driverErr *sqlitedriver.Error
	if errors.As(cause, &driverErr) && driverErr.Code()&0xff == sqlite3.SQLITE_FULL {
		return workspace.Fail("STORAGE_FULL", "磁盘空间不足或配置数据库已达到 1 GiB 上限，请释放空间或减少配置后重试。")
	}
	if fallback != nil {
		return fallback
	}
	return cause
}

func workspacePageSize(path string) (int, error) {
	const defaultSize = 4096
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultSize, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var header [2]byte
	if _, err = f.ReadAt(header[:], 16); errors.Is(err, io.EOF) {
		return defaultSize, nil
	} else if err != nil {
		return 0, err
	}
	size := int(binary.BigEndian.Uint16(header[:]))
	if size == 1 {
		return 65536, nil
	}
	if size < 512 || size&(size-1) != 0 {
		return defaultSize, nil
	}
	return size, nil
}

func dbOpen(path, mode string) (*sql.DB, error) {
	q := url.Values{}
	if mode == "rw" || mode == "rwc" {
		size, err := workspacePageSize(path)
		if err != nil {
			return nil, err
		}
		q.Add("_pragma", "page_size("+strconv.Itoa(size)+")")
		q.Add("_pragma", "max_page_count("+strconv.Itoa(maxWorkspaceBytes/size)+")")
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path}
	if mode == "snapshot" {
		mode = "ro"
		q.Set("immutable", "1")
	}
	q.Set("mode", mode)
	q.Set("_busy_timeout", "3000")
	q.Set("_foreign_keys", "1")
	q.Set("_defensive", "1")
	q.Add("_pragma", "trusted_schema(0)")
	q.Add("_pragma", "cache_size(-2048)")
	q.Add("_pragma", "mmap_size(0)")
	if mode != "ro" {
		q.Add("_pragma", "synchronous(FULL)")
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func protectedFormat(ctx context.Context, path string) *workspace.Error {
	db, err := dbOpen(path, "ro")
	if err != nil {
		return nil
	}
	defer db.Close()
	var format *workspace.Error
	if _, err = databaseVersion(ctx, db); errors.As(err, &format) {
		return format
	}
	return nil
}

func validateDB(ctx context.Context, db queryer, full bool) (workspace.Snapshot, []keyMeta, error) {
	version, err := databaseVersion(ctx, db)
	if err != nil {
		return workspace.Snapshot{}, nil, err
	}
	var result string
	pragma := "PRAGMA quick_check(1)"
	if full {
		pragma = "PRAGMA integrity_check(1)"
	}
	if err := db.QueryRowContext(ctx, pragma).Scan(&result); err != nil || result != "ok" {
		return workspace.Snapshot{}, nil, workspace.Fail("CONFIG_CORRUPT", "配置数据库校验失败，原文件已保留。")
	}
	if err := validateCatalog(ctx, db, version); err != nil {
		return workspace.Snapshot{}, nil, err
	}
	if _, err := readSessionLayout(ctx, db); err != nil {
		return workspace.Snapshot{}, nil, err
	}
	if _, err := readPinOrder(ctx, db); err != nil {
		return workspace.Snapshot{}, nil, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return workspace.Snapshot{}, nil, err
	}
	bad := rows.Next()
	rowErr := rows.Err()
	rows.Close()
	if bad || rowErr != nil {
		return workspace.Snapshot{}, nil, workspace.Fail("CONFIG_CORRUPT", "配置关联关系损坏，原文件已保留。")
	}
	return readSnapshot(ctx, db)
}

func (s *Store) initialize(ctx context.Context) error {
	if s.db != nil {
		return nil
	}
	if s.dir == "" {
		root, err := s.root()
		if err != nil || !filepath.IsAbs(root) || filepath.Clean(root) == filepath.VolumeName(root)+string(filepath.Separator) {
			return s.fail("DATA_DIRECTORY", "无法定位配置目录。")
		}
		s.dir = filepath.Clean(root)
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return s.fail("DATA_DIRECTORY", "无法创建配置目录，请检查权限和磁盘空间。")
	}
	if s.lease == nil {
		file, err := acquire(filepath.Join(s.dir, "workspace.lock"))
		if err != nil {
			return s.fail("STORAGE_IN_USE", "此数据目录正在被另一个实例使用，或无法取得文件锁。")
		}
		s.lease = file
	}
	if _, err := os.Stat(filepath.Join(s.dir, "recovery.pending")); err == nil {
		return s.fail("RECOVERY_INTERRUPTED", "上次恢复未完成，请从备份继续恢复。")
	}
	path := filepath.Join(s.dir, "workspace.sqlite3")
	info, err := os.Lstat(path)
	fresh := errors.Is(err, os.ErrNotExist)
	if fresh {
		backups, backupErr := s.backupFiles()
		if backupErr != nil {
			return s.fail("BACKUP_UNAVAILABLE", "无法检查已有备份，当前不会创建空配置。")
		}
		if len(backups) > 0 {
			return s.fail("DATABASE_MISSING", "配置文件缺失，发现备份；请恢复备份，当前不会创建空配置。")
		}
	}
	if !fresh && (err != nil || !info.Mode().IsRegular()) {
		return s.fail("CONFIG_CORRUPT", "配置文件不可用，原文件已保留。")
	}
	mode := "rw"
	if fresh {
		mode = "rwc"
	} else if format := protectedFormat(ctx, path); format != nil {
		return s.fail(format.Code, format.Message)
	} else if info.Size() > maxWorkspaceBytes {
		return s.fail("DATABASE_TOO_LARGE", oversizedDatabaseMessage)
	}
	db, err := dbOpen(path, mode)
	if err != nil {
		return s.fail("STORAGE_OPEN_FAILED", "无法打开配置存储。")
	}
	if !fresh {
		err = checkPageCapacity(ctx, db)
	}
	if err == nil {
		err = s.prepareSchema(ctx, db, fresh)
	}
	if err != nil {
		db.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if fresh {
			_ = os.Remove(path)
		}
		var known *workspace.Error
		if errors.As(err, &known) {
			return s.fail(known.Code, known.Message)
		}
		return s.fail("CONFIG_CORRUPT", "无法读取配置，原文件已保留，请尝试从备份恢复。")
	}
	var journalMode string
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil || !strings.EqualFold(journalMode, "wal") {
		db.Close()
		return s.fail("STORAGE_OPEN_FAILED", "无法启用可靠的配置写入。")
	}
	s.db = db
	s.dbEpoch++
	s.problem = nil
	s.warning = ""
	s.cacheMu.Lock()
	s.lastBackup, s.lastRoutine = verifiedBackup{}, verifiedBackup{}
	s.backupCache = map[string]cachedBackup{}
	s.cacheMu.Unlock()
	s.startBackupWorker()
	now := s.now().UTC()
	if _, err = s.runRoutineBackupInternal(ctx, now, true, false); err != nil {
		s.warning = "配置可读取，但自动备份失败；保存前会重试备份。"
		s.deferRoutineBackup(now, now.Add(backupInterval))
	}
	return nil
}

func (s *Store) report(ctx context.Context) workspace.Report {
	report := workspace.Report{Snapshot: workspace.Snapshot{Hosts: []workspace.Host{}, Groups: []string{}, Commands: []workspace.Command{}}, Warning: s.warning}
	if s.db == nil {
		if s.problem != nil {
			report.Code = s.problem.Code
			report.Message = s.problem.Message
		}
		if report.Code != "NEWER_VERSION" && report.Code != "STORAGE_IN_USE" && report.Code != "UNSUPPORTED_FORMAT" {
			report.BackupCount = s.validBackupCount(ctx)
			report.CanRecover = report.BackupCount > 0
		}
		return report
	}
	snapshot, _, err := readSnapshot(ctx, s.db)
	if err != nil {
		if ctx.Err() != nil {
			report.Code = "CANCELLED"
			report.Message = "配置读取已取消。"
			return report
		}
		s.db.Close()
		s.db = nil
		_ = s.fail("CONFIG_CORRUPT", "配置读取失败，已暂停写入并保留原文件。")
		return s.report(ctx)
	}
	report.Ready = true
	report.Snapshot = snapshot
	report.Message = "配置已载入，修改会保存在本机。"
	return report
}

func (s *Store) Load(ctx context.Context) workspace.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.report(ctx)
}

func (s *Store) DBEpoch() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dbEpoch
}

func (s *Store) Retry(ctx context.Context) workspace.Report {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.initialize(ctx)
	s.notifySyncLocked(false)
	return s.report(ctx)
}

func (s *Store) unavailable() error {
	if s.problem != nil {
		return s.problem
	}
	return workspace.Fail("STORAGE_UNAVAILABLE", "配置存储不可用。")
}

func (s *Store) Host(ctx context.Context, id string) (workspace.Host, int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.Host{}, 0, false, s.unavailable()
	}
	var revision int64
	if err := s.db.QueryRowContext(ctx, "SELECT revision FROM meta WHERE id=1").Scan(&revision); err != nil {
		return workspace.Host{}, 0, false, err
	}
	row := s.db.QueryRowContext(ctx, "SELECT "+hostSelectColumns+" FROM hosts WHERE id=?", id)
	host, err := scanHost(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.Host{}, revision, false, nil
	}
	if err != nil {
		return workspace.Host{}, 0, false, err
	}
	var scope string
	var cipherLen int
	err = s.db.QueryRowContext(ctx, "SELECT scope,length(cipher) FROM host_credentials WHERE host_id=?", id).Scan(&scope, &cipherLen)
	if err == nil && cipherLen >= 1 && cipherLen <= 65536 {
		if workspace.CredentialMatches(host, workspace.HostCredential{HostID: id, Scope: scope, Cipher: []byte{1}}) {
			host.HasCredential = true
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return workspace.Host{}, 0, false, err
	}
	return host, revision, true, nil
}

func (s *Store) ApplyChange(ctx context.Context, change workspace.Change, credential *workspace.HostCredentialChange) (workspace.Result, error) {
	if change.Kind == "markHostUsed" && credential == nil {
		return s.recordHostUsed(ctx, change.ID)
	}
	activity := credential == nil && (change.Kind == "pinHost" || change.Kind == "reorderPinnedHosts" || change.Kind == "setHostSystemIcon")
	needsCheckpoint := change.Kind == "deleteHost" || change.Kind == "deleteHosts"
	if needsCheckpoint || (!activity && !s.hasUnchangedBackup()) {
		s.vacuumMu.Lock()
		defer s.vacuumMu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.Result{}, s.unavailable()
	}
	old, keys, totalKeyCipher, err := readSnapshotFast(ctx, s.db)
	if err != nil {
		return workspace.Result{}, workspace.Fail("STORAGE_READ_FAILED", "无法读取现有配置，未执行保存。")
	}
	if old.Revision != change.Revision {
		return workspace.Result{}, workspace.Fail("CONFLICT", "配置已变更，请重新载入后再保存。")
	}
	keyIDs := make(map[string]bool, len(keys)+1)
	for _, key := range keys {
		keyIDs[key.ID] = true
	}
	if credential != nil && credential.Key != nil {
		keyIDs[credential.Key.ID] = true
	}
	next, id, err := workspace.Next(old, change, keyIDs)
	if err != nil {
		return workspace.Result{}, err
	}
	if change.Kind == "setHostSystemIcon" && id == "" {
		return workspace.Result{Snapshot: old}, nil
	}
	result, err := s.commitPreparedLocked(ctx, old, next, keys, totalKeyCipher, credential, activity, true)
	result.EntityID = id
	return result, err
}

func (s *Store) recordHostUsed(ctx context.Context, id string) (workspace.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.Result{}, s.unavailable()
	}
	usedAt := s.now().UnixMilli()
	res, err := s.db.ExecContext(ctx, "UPDATE hosts SET last_used=? WHERE id=?", usedAt, id)
	if err != nil {
		return workspace.Result{}, storageWriteError(err, workspace.Fail("SAVE_FAILED", "配置保存失败，原有资料和编辑内容已保留。"))
	}
	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return workspace.Result{}, workspace.Fail("NOT_FOUND", "找不到要更新的连接。")
	}
	snapshot, _, _, err := readSnapshotFast(ctx, s.db)
	if err != nil {
		return workspace.Result{}, workspace.Fail("STORAGE_READ_FAILED", "无法读取现有配置。")
	}
	return workspace.Result{Snapshot: snapshot, EntityID: id, Warning: s.warning}, nil
}

func (s *Store) Commit(ctx context.Context, revision int64, next workspace.Snapshot) (workspace.Result, error) {
	return s.commit(ctx, revision, next, nil, false)
}

func (s *Store) CommitHostActivity(ctx context.Context, revision int64, next workspace.Snapshot) (workspace.Result, error) {
	return s.commit(ctx, revision, next, nil, true)
}

func (s *Store) commit(ctx context.Context, revision int64, next workspace.Snapshot, credential *workspace.HostCredentialChange, activity bool) (workspace.Result, error) {
	if !activity || !s.hasUnchangedBackup() {
		s.vacuumMu.Lock()
		defer s.vacuumMu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.Result{}, s.unavailable()
	}
	old, keys, totalKeyCipher, err := readSnapshotFast(ctx, s.db)
	if err != nil {
		return workspace.Result{}, workspace.Fail("STORAGE_READ_FAILED", "无法读取现有配置，未执行保存。")
	}
	if old.Revision != revision {
		return workspace.Result{}, workspace.Fail("CONFLICT", "配置已变更，请重新载入后再保存。")
	}
	return s.commitPreparedLocked(ctx, old, next, keys, totalKeyCipher, credential, activity, false)
}

func (s *Store) commitPreparedLocked(ctx context.Context, old, next workspace.Snapshot, keys []keyMeta, totalKeyCipher int, credential *workspace.HostCredentialChange, activity, alreadyValidated bool) (workspace.Result, error) {
	var err error
	ids := make(map[string]bool, len(keys)+1)
	for _, key := range keys {
		ids[key.ID] = true
	}
	if credential != nil && credential.Key != nil {
		key := credential.Key
		if !workspace.ValidKey(*key) || ids[key.ID] || len(keys) >= workspace.MaxSavedKeys || totalKeyCipher+len(key.Cipher) > workspace.MaxTotalKeyCipherBytes {
			return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "无法保存密钥。")
		}
		ids[key.ID] = true
	}
	if !alreadyValidated {
		if err = workspace.Validate(next, ids); err != nil {
			return workspace.Result{}, err
		}
	}
	now := s.now().UTC()
	if removesHost(old.Hosts, next.Hosts) {
		activity = false
		err = s.checkpointLocked(ctx, now)
	} else if !activity {
		err = s.prepareBackupLocked(ctx, now)
	}
	if err != nil {
		return workspace.Result{}, workspace.Fail("BACKUP_FAILED", "无法创建安全备份，配置未更改，请检查磁盘空间和权限。")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.Result{}, workspace.Fail("SAVE_FAILED", "无法开始保存，配置未更改。")
	}
	defer tx.Rollback()
	if credential != nil && credential.Key != nil {
		k := credential.Key
		if _, err = tx.ExecContext(ctx, "INSERT INTO keys(id,name,format,path,cipher) VALUES(?,?,?,?,?)", k.ID, k.Name, k.Format, k.Path, k.Cipher); err != nil {
			return workspace.Result{}, storageWriteError(err, workspace.Fail("CREDENTIAL_SAVE", "无法保存密钥。"))
		}
	}
	modifiedHosts, err := writeDiff(ctx, tx, old, next)
	clearedCredentials := map[string]bool{}
	if err == nil && len(modifiedHosts) > 0 {
		previous := make(map[string]workspace.Host, len(old.Hosts))
		for _, h := range old.Hosts {
			previous[h.ID] = h
		}
		for _, h := range modifiedHosts {
			before := previous[h.ID]
			if workspace.CredentialScope(before) != workspace.CredentialScope(h) {
				if _, err = tx.ExecContext(ctx, "DELETE FROM host_credentials WHERE host_id=?", h.ID); err != nil {
					break
				}
				clearedCredentials[h.ID] = true
			}
			if before.Proxy != h.Proxy {
				if _, err = tx.ExecContext(ctx, "DELETE FROM proxy_credentials WHERE host_id=?", h.ID); err != nil {
					break
				}
			}
		}
	}
	var hostIndex = -1
	if err == nil && credential != nil {
		for i := range next.Hosts {
			if next.Hosts[i].ID == credential.HostID {
				hostIndex = i
				break
			}
		}
		if hostIndex < 0 {
			return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
		}
		host := &next.Hosts[hostIndex]
		if c := credential.Proxy; c != nil {
			if !workspace.ProxyCredentialMatches(*host, *c) {
				return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
			}
			var total int
			if err = tx.QueryRowContext(ctx, "SELECT COALESCE(sum(length(cipher)),0) FROM proxy_credentials WHERE host_id<>?", host.ID).Scan(&total); err != nil {
				return workspace.Result{}, err
			}
			if total+len(c.Cipher) > 16<<20 {
				return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO proxy_credentials VALUES(?,?,?) ON CONFLICT(host_id) DO UPDATE SET scope=excluded.scope,cipher=excluded.cipher", c.HostID, c.Scope, c.Cipher); err != nil {
				return workspace.Result{}, err
			}
		}
		switch credential.Action {
		case "keep":
		case "clear":
			_, err = tx.ExecContext(ctx, "DELETE FROM host_credentials WHERE host_id=?", host.ID)
			clearedCredentials[host.ID] = true
		case "replace":
			if !workspace.CredentialMatches(*host, credential.Credential) {
				return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
			}
			var total int
			if err = tx.QueryRowContext(ctx, "SELECT COALESCE(sum(length(cipher)),0) FROM host_credentials WHERE host_id<>?", host.ID).Scan(&total); err != nil {
				return workspace.Result{}, err
			}
			if total+len(credential.Credential.Cipher) > 16<<20 {
				return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO host_credentials(host_id,scope,cipher) VALUES(?,?,?) ON CONFLICT(host_id) DO UPDATE SET scope=excluded.scope,cipher=excluded.cipher", host.ID, credential.Credential.Scope, credential.Credential.Cipher)
		default:
			return workspace.Result{}, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
		}
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE meta SET revision=revision+1 WHERE id=1")
	}
	if err != nil {
		return workspace.Result{}, storageWriteError(err, workspace.Fail("SAVE_FAILED", "配置保存失败，原有资料和编辑内容已保留。"))
	}
	if err = tx.Commit(); err != nil {
		return workspace.Result{}, storageWriteError(err, workspace.Fail("SAVE_FAILED", "配置提交失败，请重新载入确认保存结果。"))
	}
	oldHasCred := make(map[string]bool, len(old.Hosts))
	for _, h := range old.Hosts {
		oldHasCred[h.ID] = h.HasCredential
	}
	for i := range next.Hosts {
		id := next.Hosts[i].ID
		if clearedCredentials[id] {
			next.Hosts[i].HasCredential = false
		} else {
			next.Hosts[i].HasCredential = oldHasCred[id]
		}
	}
	if credential != nil && hostIndex >= 0 {
		switch credential.Action {
		case "replace":
			next.Hosts[hostIndex].HasCredential = true
		case "clear":
			next.Hosts[hostIndex].HasCredential = false
		}
	}
	next.Revision = old.Revision + 1
	s.notifyLocalChangeLocked()
	if !activity {
		s.scheduleRoutineBackup(now)
	}
	return workspace.Result{Snapshot: next, Warning: s.warning}, nil
}

func removesHost(old, next []workspace.Host) bool {
	if len(next) < len(old) {
		return true
	}
	retained := make(map[string]bool, len(next))
	for _, host := range next {
		retained[host.ID] = true
	}
	for _, host := range old {
		if !retained[host.ID] {
			return true
		}
	}
	return false
}

func (s *Store) Keys(ctx context.Context) ([]workspace.SavedKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, s.unavailable()
	}
	keys, err := readKeys(ctx, s.db)
	if err != nil {
		return nil, workspace.Fail("KEY_STORE_UNAVAILABLE", "无法读取密钥引用记录。")
	}
	return keys, nil
}

func (s *Store) KeyMetadata(ctx context.Context) ([]workspace.KeyMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, s.unavailable()
	}
	keys, _, err := readKeyMetadata(ctx, s.db)
	if err != nil {
		return nil, workspace.Fail("KEY_STORE_UNAVAILABLE", "无法读取密钥引用记录。")
	}
	out := make([]workspace.KeyMeta, len(keys))
	for i, k := range keys {
		out[i] = workspace.KeyMeta{ID: k.ID, Name: k.Name, Format: k.Format, Path: k.Path, CipherLen: k.CipherLen}
	}
	return out, nil
}

func (s *Store) Key(ctx context.Context, id string) (workspace.SavedKey, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.SavedKey{}, false, s.unavailable()
	}
	var key workspace.SavedKey
	err := s.db.QueryRowContext(ctx, "SELECT id,name,format,path,cipher FROM keys WHERE id=?", id).Scan(&key.ID, &key.Name, &key.Format, &key.Path, &key.Cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.SavedKey{}, false, nil
	}
	if err != nil || !workspace.ValidKey(key) {
		return workspace.SavedKey{}, false, workspace.Fail("KEY_STORE_UNAVAILABLE", "无法读取密钥引用记录。")
	}
	return key, true, nil
}

func (s *Store) RegisterKey(ctx context.Context, key workspace.SavedKey) (workspace.SavedKey, error) {
	if !s.hasUnchangedBackup() {
		s.vacuumMu.Lock()
		defer s.vacuumMu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !workspace.ValidKey(key) {
		return workspace.SavedKey{}, workspace.Fail("INVALID_KEY_REFERENCE", "私钥文件引用无效。")
	}
	if s.db == nil {
		return workspace.SavedKey{}, s.unavailable()
	}
	keys, totalCipher, err := readKeyMetadata(ctx, s.db)
	if err != nil {
		return workspace.SavedKey{}, workspace.Fail("KEY_STORE_UNAVAILABLE", "无法读取密钥引用记录。")
	}
	for _, existing := range keys {
		if keyfile.SamePath(existing.Path, key.Path) {
			return workspace.SavedKey{ID: existing.ID, Name: existing.Name, Format: existing.Format, Path: existing.Path}, nil
		}
	}
	if len(keys) >= workspace.MaxSavedKeys {
		return workspace.SavedKey{}, workspace.Fail("KEY_LIMIT", "最多保存 256 个密钥文件引用。")
	}
	if totalCipher+len(key.Cipher) > workspace.MaxTotalKeyCipherBytes {
		return workspace.SavedKey{}, workspace.Fail("KEY_LIMIT", "已保存密钥内容达到容量上限，请先删除不用的密钥。")
	}
	now := s.now().UTC()
	if err = s.prepareBackupLocked(ctx, now); err != nil {
		return workspace.SavedKey{}, workspace.Fail("BACKUP_FAILED", "无法备份密钥引用，未登记文件。")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.SavedKey{}, workspace.Fail("KEY_SAVE_FAILED", "无法登记密钥引用。")
	}
	defer tx.Rollback()
	var cipher any
	if len(key.Cipher) > 0 {
		cipher = key.Cipher
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO keys(id,name,format,path,cipher) VALUES(?,?,?,?,?)", key.ID, key.Name, key.Format, key.Path, cipher); err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE meta SET revision=revision+1 WHERE id=1")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return workspace.SavedKey{}, storageWriteError(err, workspace.Fail("KEY_SAVE_FAILED", "密钥引用保存失败，原文件未修改。"))
	}
	s.notifyLocalChangeLocked()
	s.scheduleRoutineBackup(now)
	return key, nil
}

func (s *Store) ForgetKey(ctx context.Context, id string) error {
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return s.unavailable()
	}
	var inUse bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM hosts WHERE key_id=?)", id).Scan(&inUse); err != nil {
		return workspace.Fail("KEY_STORE_UNAVAILABLE", "无法读取密钥引用记录。")
	}
	if inUse {
		return workspace.Fail("KEY_IN_USE", "请先解除主机配置中的密钥关联。")
	}
	now := s.now().UTC()
	if err := s.checkpointLocked(ctx, now); err != nil {
		return workspace.Fail("BACKUP_FAILED", "无法备份密钥引用，未移除记录。")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.Fail("KEY_SAVE_FAILED", "无法移除密钥引用。")
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM keys WHERE id=?", id); err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE meta SET revision=revision+1 WHERE id=1")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return storageWriteError(err, workspace.Fail("KEY_SAVE_FAILED", "密钥引用移除失败，原文件未修改。"))
	}
	s.notifyLocalChangeLocked()
	s.scheduleRoutineBackup(now)
	return nil
}
