package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"yaare/internal/adapters/storage/durable"
	"yaare/internal/domain/syncmodel"
)

type SyncRecord struct {
	Version         int64
	Settings, State []byte
	Paused          bool
	BaselineLost    bool
}

type SyncMeta struct {
	Record            SyncRecord
	StateHash         string
	WorkspaceRevision int64
	DBEpoch           uint64
}

func (s *Store) SubscribeSync() (<-chan struct{}, func()) {
	changes := make(chan struct{}, 1)
	s.mu.Lock()
	if s.syncListeners == nil {
		s.syncListeners = make(map[chan struct{}]struct{})
	}
	s.syncListeners[changes] = struct{}{}
	s.mu.Unlock()
	return changes, func() {
		s.mu.Lock()
		delete(s.syncListeners, changes)
		s.mu.Unlock()
	}
}

func (s *Store) SubscribeLocalChanges() (<-chan struct{}, func()) {
	changes := make(chan struct{}, 1)
	s.mu.Lock()
	if s.localChangeListeners == nil {
		s.localChangeListeners = make(map[chan struct{}]struct{})
	}
	s.localChangeListeners[changes] = struct{}{}
	s.mu.Unlock()
	return changes, func() {
		s.mu.Lock()
		delete(s.localChangeListeners, changes)
		s.mu.Unlock()
	}
}

func (s *Store) notifyLocalChangeLocked() {
	for changes := range s.localChangeListeners {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
}

func (s *Store) notifySyncLocked(applied bool) {
	if applied {
		s.syncApplied++
	}
	for changes := range s.syncListeners {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
}

func validBaselineHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for i := 0; i < len(hash); i++ {
		c := hash[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func (s *Store) baselineDir() string {
	return filepath.Join(s.dir, "sync-baselines")
}

func (s *Store) baselinePath(hash string) string {
	return filepath.Join(s.baselineDir(), hash+".bin")
}

func (s *Store) writeBaselineFile(ctx context.Context, state []byte) (string, error) {
	if len(state) == 0 {
		return "", nil
	}
	if len(state) > 24<<20 {
		return "", errors.New("同步配置超过大小限制。")
	}
	sum := sha256.Sum256(state)
	hash := hex.EncodeToString(sum[:])
	dir := s.baselineDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := durable.SyncDir(s.dir); err != nil {
		return "", err
	}
	dest := s.baselinePath(hash)
	if info, err := os.Lstat(dest); err == nil && info.Mode().IsRegular() && info.Size() == int64(len(state)) {
		if existing, readErr := s.readBaselineFile(hash); readErr == nil && len(existing) == len(state) {
			return hash, durable.SyncDir(dir)
		}
	}
	file, err := os.CreateTemp(dir, ".baseline-*.tmp")
	if err != nil {
		return "", err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(state)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return "", err
	}
	if err = durable.Rename(temp, dest); err != nil {
		return "", err
	}
	return hash, durable.SyncDir(dir)
}

func (s *Store) readBaselineFile(hash string) ([]byte, error) {
	if hash == "" {
		return nil, nil
	}
	if !validBaselineHash(hash) {
		return nil, errors.New("同步基线标识无效。")
	}
	path := s.baselinePath(hash)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 24<<20 {
		return nil, errors.New("同步基线文件不可用。")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 24<<20 {
		return nil, errors.New("同步基线文件不可用。")
	}
	data, err := io.ReadAll(io.LimitReader(f, (24<<20)+1))
	if err != nil || len(data) > 24<<20 {
		return nil, errors.New("同步基线读取失败。")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		return nil, errors.New("同步基线校验失败。")
	}
	s.baselineHash, s.baselineInfo = hash, info
	return data, nil
}

func (s *Store) pruneBaselines(keepHash string) {
	entries, err := os.ReadDir(s.baselineDir())
	if err != nil {
		return
	}
	keepName := ""
	if keepHash != "" {
		keepName = keepHash + ".bin"
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".bin") || name == keepName {
			continue
		}
		_ = os.Remove(filepath.Join(s.baselineDir(), name))
	}
}

func (s *Store) reconcileBaselineAfterRecover(ctx context.Context) error {
	_, _, err := s.readSyncLocked(ctx, false)
	return err
}

func (s *Store) ReadSyncSettings(ctx context.Context) (SyncRecord, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, _, err := s.readSyncLocked(ctx, false)
	return record, s.syncApplied, err
}

func (s *Store) SyncVersion(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return 0, s.unavailable()
	}
	var version int64
	err := s.db.QueryRowContext(ctx, "SELECT version FROM config_sync WHERE id=1").Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return version, err
}

func (s *Store) ReadSyncMeta(ctx context.Context) (SyncMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var meta SyncMeta
	if s.db == nil {
		return meta, s.unavailable()
	}
	meta.DBEpoch = s.dbEpoch
	if err := s.db.QueryRowContext(ctx, "SELECT revision FROM meta WHERE id=1").Scan(&meta.WorkspaceRevision); err != nil {
		return SyncMeta{}, err
	}
	var err error
	meta.Record, meta.StateHash, err = s.readSyncLocked(ctx, false)
	return meta, err
}

func (s *Store) ReadSync(ctx context.Context) (SyncRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, _, err := s.readSyncLocked(ctx, true)
	return r, err
}

func (s *Store) readSyncLocked(ctx context.Context, withState bool) (SyncRecord, string, error) {
	var r SyncRecord
	if s.db == nil {
		return r, "", s.unavailable()
	}
	var stateHash string
	e := s.db.QueryRowContext(ctx, "SELECT version,settings,state_hash,paused,baseline_lost FROM config_sync WHERE id=1").Scan(&r.Version, &r.Settings, &stateHash, &r.Paused, &r.BaselineLost)
	if errors.Is(e, sql.ErrNoRows) {
		return r, "", nil
	}
	if e != nil || stateHash == "" {
		return r, "", e
	}
	if !withState && s.baselineHash == stateHash && s.baselineInfo != nil {
		info, err := os.Lstat(s.baselinePath(stateHash))
		if err == nil && info.Mode().IsRegular() && os.SameFile(info, s.baselineInfo) && info.Size() == s.baselineInfo.Size() && info.ModTime().Equal(s.baselineInfo.ModTime()) {
			return r, stateHash, nil
		}
	}
	state, err := s.readBaselineFile(stateHash)
	if err != nil {
		if err := s.pauseLostBaselineLocked(ctx, r.Version); err != nil {
			return SyncRecord{}, "", err
		}
		r.Version++
		r.Paused, r.BaselineLost = true, true
		return r, "", nil
	}
	if withState {
		r.State = state
	}
	return r, stateHash, nil
}

func (s *Store) PauseLostBaseline(ctx context.Context, version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return s.unavailable()
	}
	return s.pauseLostBaselineLocked(ctx, version)
}

func (s *Store) pauseLostBaselineLocked(ctx context.Context, version int64) error {
	result, err := s.db.ExecContext(ctx, "UPDATE config_sync SET state_hash='',paused=1,baseline_lost=1,version=version+1 WHERE id=1 AND version=? AND state_hash<>''", version)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return errors.New("同步设置已修改，请重新读取。")
	}
	s.baselineHash, s.baselineInfo = "", nil
	s.notifySyncLocked(false)
	s.scheduleRoutineBackup(s.now())
	return nil
}

func writeSync(ctx context.Context, tx *sql.Tx, r SyncRecord, stateHash string) error {
	if len(r.Settings) == 0 || len(r.Settings) > 65536 || (stateHash != "" && !validBaselineHash(stateHash)) {
		return errors.New("同步配置超过大小限制。")
	}
	if r.BaselineLost && (!r.Paused || stateHash != "") {
		return errors.New("同步基线不可用，请先检查并合并配置。")
	}
	var current int64
	e := tx.QueryRowContext(ctx, "SELECT version FROM config_sync WHERE id=1").Scan(&current)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if current != r.Version {
		return errors.New("同步设置已修改，请重新读取。")
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO config_sync(id,version,settings,state_hash,paused,baseline_lost) VALUES(1,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version,settings=excluded.settings,state_hash=excluded.state_hash,paused=excluded.paused,baseline_lost=excluded.baseline_lost", r.Version+1, r.Settings, stateHash, r.Paused, r.BaselineLost)
	return e
}

func (s *Store) SaveSync(ctx context.Context, r SyncRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return s.unavailable()
	}
	if len(r.Settings) == 0 || len(r.Settings) > 65536 || len(r.State) > 24<<20 {
		return errors.New("同步配置超过大小限制。")
	}
	stateHash, e := s.writeBaselineFile(ctx, r.State)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = writeSync(ctx, tx, r, stateHash); e != nil {
		return storageWriteError(e, nil)
	}
	if e = tx.Commit(); e != nil {
		return storageWriteError(e, nil)
	}
	s.pruneBaselines(stateHash)
	s.notifySyncLocked(false)
	s.scheduleRoutineBackup(s.now())
	return nil
}

func (s *Store) ApplySync(ctx context.Context, revision int64, next syncmodel.LocalData, r SyncRecord) error {
	return storageWriteError(s.replaceCloud(ctx, revision, next, nil, &r), nil)
}

func (s *Store) RestoreSync(ctx context.Context, revision int64, next syncmodel.LocalData, r SyncRecord) error {
	r.Paused = true
	return storageWriteError(s.replaceCloud(ctx, revision, next, nil, &r), nil)
}
