package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"yaare/internal/adapters/storage/durable"
	"yaare/internal/domain/workspace"
)

const (
	backupTimeLayout    = "20060102T150405.000000000Z"
	backupInterval      = 10 * time.Minute
	checkpointRetention = 7 * 24 * time.Hour
)

var backupPattern = regexp.MustCompile(`^backup-([0-9]{8}T[0-9]{6}\.[0-9]{9}Z)-([a-f0-9]{32})(-protected)?\.sqlite3$`)

type backupFile struct {
	path, id  string
	created   time.Time
	protected bool
}

type verifiedBackup struct {
	backupFile
	info os.FileInfo
}

type cachedBackup struct {
	info         os.FileInfo
	valid        bool
	fullVerified bool
	hosts        []workspace.Host
}

const maxHostCacheFileBytes = 4 << 20

func (entry cachedBackup) cacheValue() cachedBackup {
	if entry.info.Size() > maxHostCacheFileBytes {
		entry.hosts = nil
	}
	return entry
}

func (backup verifiedBackup) unchanged() bool {
	if backup.info == nil {
		return false
	}
	info, err := os.Lstat(backup.path)
	return err == nil && sameBackup(backup.info, info)
}

func sameBackup(previous, current os.FileInfo) bool {
	return previous.Size() == current.Size() && previous.ModTime().Equal(current.ModTime()) && os.SameFile(previous, current)
}

func backupInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxWorkspaceBytes {
		return nil, workspace.Fail("INVALID_BACKUP", "备份文件不可用。")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !sameBackup(info, opened) {
		return nil, workspace.Fail("INVALID_BACKUP", "备份文件不可用。")
	}
	return opened, nil
}

func backupFilesIn(dir string) ([]backupFile, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []backupFile
	for _, entry := range entries {
		match := backupPattern.FindStringSubmatch(entry.Name())
		if !entry.Type().IsRegular() || match == nil {
			continue
		}
		created, err := time.Parse(backupTimeLayout, match[1])
		if err != nil {
			continue
		}
		files = append(files, backupFile{path: filepath.Join(dir, "backups", entry.Name()), id: match[2], created: created, protected: match[3] != ""})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].path > files[right].path })
	return files, nil
}

func (s *Store) backupFiles() ([]backupFile, error) {
	return backupFilesIn(s.dir)
}

func openBackup(path string) (*sql.DB, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxWorkspaceBytes {
		return nil, workspace.Fail("INVALID_BACKUP", "备份文件不可用。")
	}
	return dbOpen(path, "snapshot")
}

func checkFile(ctx context.Context, path string, full bool) (workspace.Snapshot, []keyMeta, error) {
	db, err := openBackup(path)
	if err != nil {
		return workspace.Snapshot{}, nil, err
	}
	defer db.Close()
	return verifyBackup(ctx, db, full)
}

func checkFileWithDigest(ctx context.Context, path string, full bool) (workspace.Snapshot, string, error) {
	db, err := openBackup(path)
	if err != nil {
		return workspace.Snapshot{}, "", err
	}
	defer db.Close()
	snapshot, _, err := verifyBackup(ctx, db, full)
	if err != nil {
		return workspace.Snapshot{}, "", err
	}
	sum, err := backupDigest(ctx, db)
	if err != nil {
		return workspace.Snapshot{}, "", err
	}
	return snapshot, sum, nil
}

func verifyBackup(ctx context.Context, db *sql.DB, full bool) (workspace.Snapshot, []keyMeta, error) {
	snapshot, keys, err := validateDB(ctx, db, full)
	if err != nil {
		return snapshot, nil, err
	}
	credentials, err := readCredentialMetadata(ctx, db, "SELECT host_id,scope,length(cipher) FROM proxy_credentials ORDER BY host_id")
	if err != nil {
		return snapshot, nil, err
	}
	if len(credentials) != 0 {
		hosts := make(map[string]workspace.Host, len(snapshot.Hosts))
		for _, host := range snapshot.Hosts {
			hosts[host.ID] = host
		}
		for _, entry := range credentials {
			if !workspace.ProxyCredentialMatches(hosts[entry.HostID], entry) {
				return snapshot, nil, workspace.Fail("CREDENTIAL_INVALID", "备份中的代理认证记录无效。")
			}
		}
	}
	return snapshot, keys, nil
}

func backupDigest(ctx context.Context, q queryer) (string, error) {
	version, err := databaseVersion(ctx, q)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	syncQuery := "SELECT id,version,settings,state_hash,paused FROM config_sync ORDER BY id"
	if version >= 2 {
		if err := hashQuery(ctx, q, h, "credential_key_check", "SELECT id,cipher FROM credential_key_check ORDER BY id"); err != nil {
			return "", err
		}
		syncQuery = "SELECT id,version,settings,state_hash,paused,baseline_lost FROM config_sync ORDER BY id"
	}
	queries := []struct {
		tag string
		sql string
	}{
		{"meta", "SELECT id,revision FROM meta ORDER BY id"},
		{"groups", "SELECT name,position FROM groups ORDER BY name"},
		{"host_order", "SELECT id,pins FROM host_order ORDER BY id"},
		{"hosts", "SELECT " + hostSelectColumns + " FROM hosts ORDER BY id"},
		{"command_groups", "SELECT name,position FROM command_groups ORDER BY name"},
		{"commands", "SELECT id,name,command,position,group_name,icon FROM commands ORDER BY id"},
		{"keys", "SELECT id,name,format,path,COALESCE(cipher,X'') FROM keys ORDER BY id"},
		{"identities", "SELECT id,name,username,authentication,COALESCE(key_id,''),version,cipher,use_agent FROM identities ORDER BY id"},
		{"host_credentials", "SELECT host_id,scope,cipher FROM host_credentials ORDER BY host_id"},
		{"proxy_credentials", "SELECT host_id,scope,cipher FROM proxy_credentials ORDER BY host_id"},
		{"desktop_preferences", "SELECT id,version,revision,COALESCE(portable,X''),COALESCE(device,X'') FROM desktop_preferences ORDER BY id"},
		{"session_layout", "SELECT id,revision,payload FROM session_layout ORDER BY id"},
		{"portable_extensions", "SELECT id,data FROM portable_extensions ORDER BY id"},
		{"config_sync", syncQuery},
	}
	for _, entry := range queries {
		if err := hashQuery(ctx, q, h, entry.tag, entry.sql); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeHashBytes(h hash.Hash, b []byte) {
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(b)))
	_, _ = h.Write(lenBuf[:])
	_, _ = h.Write(b)
}

func hashQuery(ctx context.Context, q queryer, h hash.Hash, tag, query string) error {
	writeHashBytes(h, []byte(tag))
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	raw := make([]sql.RawBytes, len(cols))
	dest := make([]any, len(cols))
	for i := range raw {
		dest[i] = &raw[i]
	}
	for rows.Next() {
		if err = rows.Scan(dest...); err != nil {
			return err
		}
		_, _ = h.Write([]byte{1})
		for _, cell := range raw {
			writeHashBytes(h, cell)
		}
	}
	_, _ = h.Write([]byte{0})
	return rows.Err()
}

func (s *Store) verifyBackupCached(ctx context.Context, path string, full, needHosts bool) (cachedBackup, bool) {
	info, err := backupInfo(path)
	if err != nil {
		s.cacheMu.Lock()
		delete(s.backupCache, path)
		s.cacheMu.Unlock()
		return cachedBackup{}, false
	}
	s.cacheMu.Lock()
	if entry, ok := s.backupCache[path]; ok && sameBackup(entry.info, info) &&
		(!full || entry.fullVerified) && (!needHosts || !entry.valid || info.Size() <= maxHostCacheFileBytes) {
		s.cacheMu.Unlock()
		return entry, entry.valid
	}
	s.cacheMu.Unlock()

	snapshot, _, err := checkFile(ctx, path, full)
	if ctx.Err() != nil {
		return cachedBackup{}, false
	}
	entry := cachedBackup{
		info:         info,
		valid:        err == nil,
		fullVerified: full,
	}
	if err == nil {
		entry.hosts = snapshot.Hosts
	}
	s.cacheMu.Lock()
	if s.backupCache == nil {
		s.backupCache = map[string]cachedBackup{}
	}
	s.backupCache[path] = entry.cacheValue()
	s.cacheMu.Unlock()
	return entry, entry.valid
}

func (s *Store) cacheVerifiedBackup(path string, info os.FileInfo, hosts []workspace.Host) {
	if info == nil {
		return
	}
	s.cacheMu.Lock()
	if s.backupCache == nil {
		s.backupCache = map[string]cachedBackup{}
	}
	s.backupCache[path] = cachedBackup{
		info:         info,
		valid:        true,
		fullVerified: true,
		hosts:        hosts,
	}.cacheValue()
	s.cacheMu.Unlock()
}

func (s *Store) hasUnchangedBackup() bool {
	s.cacheMu.Lock()
	backup := s.lastBackup
	s.cacheMu.Unlock()
	return backup.unchanged()
}

func (s *Store) validBackupCount(ctx context.Context) int {
	files, err := s.backupFiles()
	if err != nil {
		return 0
	}
	count := 0
	for _, file := range files {
		if _, ok := s.verifyBackupCached(ctx, file.path, true, false); ok {
			count++
		}
	}
	return count
}

func (s *Store) createBackupFromLive(ctx context.Context, db *sql.DB, dir string, now time.Time, protected bool) error {
	wantDigest, err := backupDigest(ctx, db)
	if err != nil {
		return err
	}
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(backupDir, ".snapshot-*.sqlite3")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err = file.Close(); err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", temp); err != nil {
		return err
	}
	return s.finalizeBackup(ctx, temp, backupDir, wantDigest, now, protected)
}

func (s *Store) finalizeBackup(ctx context.Context, temp, backupDir, wantDigest string, now time.Time, protected bool) error {
	copySnapshot, gotDigest, err := checkFileWithDigest(ctx, temp, true)
	if err != nil {
		return err
	}
	if gotDigest != wantDigest {
		return workspace.Fail("BACKUP_MISMATCH", "备份内容未通过校验。")
	}
	file, err := os.OpenFile(temp, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(syncErr, closeErr); err != nil {
		return err
	}
	id := workspace.NewID()
	if id == "" {
		return workspace.Fail("BACKUP_FAILED", "无法创建备份编号。")
	}
	suffix := ".sqlite3"
	if protected {
		suffix = "-protected" + suffix
	}
	s.cacheMu.Lock()
	lastCreated := s.lastBackup.created
	s.cacheMu.Unlock()
	created := now.UTC()
	if !created.After(lastCreated) && !lastCreated.After(now.UTC().Add(time.Second)) {
		created = lastCreated.Add(time.Nanosecond)
	}
	path := filepath.Join(backupDir, "backup-"+created.Format(backupTimeLayout)+"-"+id+suffix)
	if err = durable.Rename(temp, path); err != nil {
		return err
	}
	if err = durable.SyncDir(backupDir); err != nil {
		return err
	}
	if err = durable.SyncDir(filepath.Dir(backupDir)); err != nil {
		return err
	}
	info, err := backupInfo(path)
	if err != nil {
		return err
	}
	verified := verifiedBackup{backupFile{path, id, created, protected}, info}
	s.cacheMu.Lock()
	s.lastBackup = verified
	if !protected {
		s.lastRoutine = verified
	}
	s.cacheMu.Unlock()
	s.cacheVerifiedBackup(path, info, copySnapshot.Hosts)
	return nil
}

func (s *Store) prepareBackupLocked(ctx context.Context, now time.Time) error {
	if s.hasUnchangedBackup() {
		return nil
	}
	files, err := s.backupFiles()
	if err != nil {
		return err
	}
	for _, file := range files {
		info, err := backupInfo(file.path)
		if err != nil {
			continue
		}
		if _, ok := s.verifyBackupCached(ctx, file.path, true, false); !ok {
			continue
		}
		verified := verifiedBackup{file, info}
		s.cacheMu.Lock()
		s.lastBackup = verified
		if !file.protected {
			s.lastRoutine = verified
		}
		s.cacheMu.Unlock()
		return nil
	}
	if err := s.createBackupFromLive(ctx, s.db, s.dir, now, false); err != nil {
		return err
	}
	s.schedulePrune(now)
	return nil
}

func (s *Store) checkpointLocked(ctx context.Context, now time.Time) error {
	if err := s.createBackupFromLive(ctx, s.db, s.dir, now, true); err != nil {
		return err
	}
	s.schedulePrune(now)
	return nil
}

func (s *Store) runRoutineBackup(ctx context.Context, now time.Time) bool {
	pruned, err := s.runRoutineBackupInternal(ctx, now, false, false)
	s.mu.Lock()
	if s.db != nil {
		if err != nil {
			s.warning = "配置已保存，但自动备份失败；请检查磁盘空间和权限。"
			s.deferRoutineBackup(now, now.Add(backupInterval))
		} else {
			s.warning = ""
		}
	}
	s.mu.Unlock()
	return pruned
}

func (s *Store) runRoutineBackupInternal(ctx context.Context, now time.Time, holdsMu, closing bool) (bool, error) {
	now = now.UTC()
	s.cacheMu.Lock()
	lastRoutine := s.lastRoutine
	s.cacheMu.Unlock()
	if lastRoutine.unchanged() && !closing && s.deferRoutineBackup(now, lastRoutine.created.Add(backupInterval)) {
		return false, nil
	}
	var dir string
	if !holdsMu {
		s.mu.Lock()
		if s.db == nil {
			s.mu.Unlock()
			return false, nil
		}
		dir = s.dir
		s.mu.Unlock()
	} else {
		dir = s.dir
	}

	if !lastRoutine.unchanged() {
		files, err := backupFilesIn(dir)
		if err != nil {
			return false, err
		}
		for _, file := range files {
			if file.protected {
				continue
			}
			info, err := backupInfo(file.path)
			if err != nil {
				continue
			}
			prevSnapshot, prevDigest, err := checkFileWithDigest(ctx, file.path, true)
			if err != nil {
				continue
			}
			verified := verifiedBackup{file, info}
			s.cacheMu.Lock()
			s.lastRoutine = verified
			if s.lastBackup.info == nil {
				s.lastBackup = verified
			}
			s.cacheMu.Unlock()
			s.cacheVerifiedBackup(file.path, info, prevSnapshot.Hosts)
			var currentDigest string
			if !holdsMu {
				s.mu.Lock()
				if s.db != nil {
					currentDigest, err = backupDigest(ctx, s.db)
				}
				s.mu.Unlock()
			} else {
				currentDigest, err = backupDigest(ctx, s.db)
			}
			if err != nil {
				return false, err
			}
			if prevDigest == currentDigest {
				return false, nil
			}
			if !closing && s.deferRoutineBackup(now, file.created.Add(backupInterval)) {
				return false, nil
			}
			break
		}
	}

	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return false, err
	}
	file, err := os.CreateTemp(backupDir, ".snapshot-*.sqlite3")
	if err != nil {
		return false, err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err = file.Close(); err != nil {
		return false, err
	}

	var wantDigest string
	if !holdsMu {
		s.mu.Lock()
		if s.db == nil {
			s.mu.Unlock()
			return false, nil
		}
		wantDigest, err = backupDigest(ctx, s.db)
		if err == nil {
			_, err = s.db.ExecContext(ctx, "VACUUM INTO ?", temp)
		}
		s.mu.Unlock()
	} else {
		wantDigest, err = backupDigest(ctx, s.db)
		if err == nil {
			_, err = s.db.ExecContext(ctx, "VACUUM INTO ?", temp)
		}
	}
	if err != nil {
		return false, err
	}
	if err = s.finalizeBackup(ctx, temp, backupDir, wantDigest, now, false); err != nil {
		return false, err
	}
	s.pruneBackupsIn(ctx, dir, now)
	return true, nil
}

func (s *Store) startBackupWorker() {
	s.backupStateMu.Lock()
	if s.backupDone != nil && !s.backupClosed {
		s.backupStateMu.Unlock()
		return
	}
	s.backupClosed = false
	s.backupOnClose = false
	s.backupQueued = false
	s.pruneQueued = false
	s.backupRunning = false
	s.backupDone = make(chan struct{})
	if s.backupCond == nil {
		s.backupCond = sync.NewCond(&s.backupStateMu)
	}
	ctx := context.Background()
	done := s.backupDone
	s.backupStateMu.Unlock()

	go func() {
		defer close(done)
		for {
			s.backupStateMu.Lock()
			for !s.backupQueued && !s.pruneQueued && !s.backupClosed {
				s.backupCond.Wait()
			}
			if s.backupClosed && !s.backupQueued && !s.pruneQueued {
				s.backupStateMu.Unlock()
				return
			}
			doRoutine := s.backupQueued
			doPrune := s.pruneQueued
			at := s.backupAt
			s.backupQueued = false
			s.pruneQueued = false
			s.backupRunning = true
			s.backupStateMu.Unlock()

			s.vacuumMu.Lock()
			pruned := false
			if doRoutine {
				pruned = s.runRoutineBackup(ctx, at)
			}
			if doPrune && !pruned {
				s.mu.Lock()
				dir := s.dir
				s.mu.Unlock()
				if dir != "" {
					s.pruneBackupsIn(ctx, dir, at)
				}
			}
			s.vacuumMu.Unlock()

			s.backupStateMu.Lock()
			s.backupRunning = false
			s.backupCond.Broadcast()
			s.backupStateMu.Unlock()
		}
	}()
}

func (s *Store) deferRoutineBackup(now, due time.Time) bool {
	delay := due.Sub(now)
	if delay <= 0 || delay > backupInterval {
		return false
	}
	s.backupStateMu.Lock()
	defer s.backupStateMu.Unlock()
	if s.backupClosed {
		s.backupOnClose = true
		return true
	}
	if s.backupTimer != nil {
		return true
	}
	s.backupDue = due
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		s.backupStateMu.Lock()
		defer s.backupStateMu.Unlock()
		if s.backupTimer == timer {
			s.backupTimer = nil
			s.backupQueued = true
			s.backupAt = due
			s.backupCond.Broadcast()
		}
	})
	s.backupTimer = timer
	return true
}

func (s *Store) scheduleRoutineBackup(at time.Time) {
	s.backupStateMu.Lock()
	defer s.backupStateMu.Unlock()
	if s.backupClosed {
		s.backupOnClose = true
		return
	}
	if s.backupDone != nil {
		if s.backupTimer != nil {
			if at.Before(s.backupDue) && s.backupDue.Sub(at) <= backupInterval {
				return
			}
			s.backupTimer.Stop()
			s.backupTimer = nil
		}
		s.backupQueued = true
		s.backupAt = at.UTC()
		s.backupCond.Broadcast()
	}
}

func (s *Store) schedulePrune(at time.Time) {
	s.backupStateMu.Lock()
	if s.backupDone != nil && !s.backupClosed {
		s.pruneQueued = true
		if !s.backupQueued {
			s.backupAt = at.UTC()
		}
		s.backupCond.Broadcast()
	}
	s.backupStateMu.Unlock()
}

func (s *Store) waitBackups() {
	s.backupStateMu.Lock()
	for s.backupDone != nil && (s.backupQueued || s.pruneQueued || s.backupRunning) {
		s.backupCond.Wait()
	}
	s.backupStateMu.Unlock()
}

func (s *Store) stopBackupWorker() {
	s.backupStateMu.Lock()
	done := s.backupDone
	if done == nil {
		s.backupStateMu.Unlock()
		return
	}
	s.backupClosed = true
	if s.backupTimer != nil {
		s.backupTimer.Stop()
		s.backupTimer = nil
		s.backupOnClose = true
	}
	if s.backupQueued {
		s.backupQueued = false
		s.backupOnClose = true
	}
	s.backupCond.Broadcast()
	s.backupStateMu.Unlock()
	<-done
}

func (s *Store) pruneBackups(ctx context.Context, now time.Time) {
	s.pruneBackupsIn(ctx, s.dir, now)
}

func (s *Store) pruneBackupsIn(ctx context.Context, dir string, now time.Time) {
	files, err := backupFilesIn(dir)
	if err != nil {
		return
	}
	s.cacheMu.Lock()
	lastRoutinePath := s.lastRoutine.path
	s.cacheMu.Unlock()
	today := now.UTC().Truncate(24 * time.Hour)
	weekday := (int(today.Weekday()) + 6) % 7
	oldestWeek := today.AddDate(0, 0, -weekday-21)
	oldestDay := today.AddDate(0, 0, -6)
	days := map[time.Time]bool{}
	weeks := map[[2]int]bool{}
	valid := 0
	for _, file := range files {
		if ctx.Err() != nil {
			return
		}
		if file.created.After(now) {
			continue
		}
		day := file.created.Truncate(24 * time.Hour)
		year, week := file.created.ISOWeek()
		weekKey := [2]int{year, week}
		keepDay := !day.Before(oldestDay) && !days[day]
		keepWeek := !day.Before(oldestWeek) && !weeks[weekKey]
		keepCheckpoint := file.protected && now.Sub(file.created) < checkpointRetention
		keepCooldown := file.path == lastRoutinePath && now.Sub(file.created) < backupInterval
		if valid < 5 || keepDay || keepWeek {
			if _, ok := s.verifyBackupCached(ctx, file.path, true, false); ok {
				valid++
				if keepDay {
					days[day] = true
				}
				if keepWeek {
					weeks[weekKey] = true
				}
			}
		} else if !keepCheckpoint && !keepCooldown {
			if _, ok := s.verifyBackupCached(ctx, file.path, false, false); ok && ctx.Err() == nil {
				if os.Remove(file.path) == nil {
					s.cacheMu.Lock()
					delete(s.backupCache, file.path)
					s.cacheMu.Unlock()
				}
			}
		}
	}
}

func copyFile(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return workspace.Fail("PRESERVE_FAILED", "原文件超过自动恢复范围，请保留文件后另行处理。")
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	source, err := in.Stat()
	if err != nil || !os.SameFile(info, source) {
		return workspace.Fail("PRESERVE_FAILED", "无法完整保留原文件，未执行恢复。")
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, err = io.CopyN(out, in, source.Size())
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func (s *Store) Recover(ctx context.Context) (workspace.Report, error) {
	s.waitBackups()
	s.vacuumMu.Lock()
	defer s.vacuumMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.notifySyncLocked(false)
	if s.db != nil {
		return s.report(ctx), nil
	}
	if s.dir == "" || s.lease == nil {
		_ = s.initialize(ctx)
		if s.db != nil {
			return s.report(ctx), nil
		}
	}
	if s.lease == nil || s.problem == nil {
		return s.report(ctx), s.unavailable()
	}
	if s.problem.Code == "NEWER_VERSION" || s.problem.Code == "UNSUPPORTED_FORMAT" {
		return s.report(ctx), s.unavailable()
	}
	files, err := s.backupFiles()
	if err != nil {
		return s.report(ctx), workspace.Fail("BACKUP_UNAVAILABLE", "无法读取备份目录，原文件未修改。")
	}
	chosen := ""
	for _, file := range files {
		if _, _, err := checkFile(ctx, file.path, true); err == nil {
			chosen = file.path
			break
		}
	}
	if chosen == "" {
		return s.report(ctx), workspace.Fail("NO_VALID_BACKUP", "没有通过校验的可用备份，原文件未修改。")
	}
	primary := filepath.Join(s.dir, "workspace.sqlite3")
	if format := protectedFormat(ctx, primary); format != nil {
		_ = s.fail(format.Code, format.Message)
		return s.report(ctx), s.unavailable()
	}
	file, err := os.CreateTemp(s.dir, ".restore-*.sqlite3")
	if err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "无法创建恢复文件，原文件未修改。")
	}
	temp := file.Name()
	file.Close()
	defer os.Remove(temp)
	if err = copyFile(chosen, temp); err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "无法复制备份，原文件未修改。")
	}
	if _, _, err = checkFile(ctx, temp, true); err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "恢复副本未通过校验，原文件未修改。")
	}
	archiveRoot := filepath.Join(s.dir, "recovery")
	if err = os.MkdirAll(archiveRoot, 0700); err != nil {
		return s.report(ctx), workspace.Fail("PRESERVE_FAILED", "无法保留原文件，未执行恢复。")
	}
	archive, err := os.MkdirTemp(archiveRoot, "original-"+time.Now().UTC().Format("20060102T150405")+"-")
	if err != nil {
		return s.report(ctx), workspace.Fail("PRESERVE_FAILED", "无法保留原文件，未执行恢复。")
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		path := primary + suffix
		if _, err = os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err = copyFile(path, filepath.Join(archive, "workspace.sqlite3"+suffix)); err != nil {
			return s.report(ctx), workspace.Fail("PRESERVE_FAILED", "无法完整保留原文件，未执行恢复。")
		}
	}
	for _, dir := range []string{archive, archiveRoot, s.dir} {
		if err = durable.SyncDir(dir); err != nil {
			return s.report(ctx), workspace.Fail("PRESERVE_FAILED", "无法完整保留原文件，未执行恢复。")
		}
	}
	marker := filepath.Join(s.dir, "recovery.pending")
	pending, err := os.OpenFile(marker, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "无法记录恢复状态，原文件未修改。")
	}
	_, err = pending.WriteString("Yaare recovery v1\n")
	if err == nil {
		err = pending.Sync()
	}
	closeErr := pending.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = durable.SyncDir(s.dir)
	}
	if err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "无法记录恢复状态，原文件未修改。")
	}
	_ = s.fail("RECOVERY_INTERRUPTED", "恢复尚未完成，可重试；原文件已保留在恢复目录。")
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err = os.Remove(primary + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return s.report(ctx), workspace.Fail("RESTORE_FAILED", "无法切换恢复副本，原文件已保留，可重试。")
		}
	}
	if err = durable.Rename(temp, primary); err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "恢复文件替换失败，原文件已保留，可重试。")
	}
	if err = durable.SyncDir(s.dir); err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "恢复文件替换失败，原文件已保留，可重试。")
	}
	if _, _, err = checkFile(ctx, primary, true); err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "恢复后的数据库校验失败，原文件和备份均已保留。")
	}
	if err = os.Remove(marker); err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "恢复副本已就绪，但无法完成恢复标记，请重试。")
	}
	if err = durable.SyncDir(s.dir); err != nil {
		return s.report(ctx), workspace.Fail("RESTORE_FAILED", "恢复副本已就绪，但无法完成恢复标记，请重试。")
	}
	if err = s.initialize(ctx); err != nil {
		return s.report(ctx), err
	}
	if err = s.reconcileBaselineAfterRecover(ctx); err != nil {
		return s.report(ctx), err
	}
	report := s.report(ctx)
	report.Message = "已从最近可用备份恢复，原文件已保留在恢复目录。"
	return report, nil
}
