package cloudsync

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"yaare/internal/adapters/storage/durable"
	"yaare/internal/domain/workspace"
)

const (
	portableBackupMinKeep   = 20
	portableBackupMaxKeep   = 50
	portableBackupRetention = 30 * 24 * time.Hour
	restoreTimestamp        = "20060102T150405Z"
)

var restoreID = regexp.MustCompile("^[0-9a-f]{32}$")

func ownedRestoreCopy(name string) bool {
	base, ok := strings.CutSuffix(name, ".yaare")
	if !ok {
		return false
	}
	if id, ok := strings.CutPrefix(base, "before-import-"); ok {
		return restoreID.MatchString(id)
	}
	value, ok := strings.CutPrefix(base, "restore-sync-")
	if !ok {
		return false
	}
	timestamp, id, ok := strings.Cut(value, "-")
	if !ok || !restoreID.MatchString(id) {
		return false
	}
	_, err := time.Parse(restoreTimestamp, timestamp)
	return err == nil
}

func ReadArchive(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBlob {
		return nil, errors.New("配置文件无效或超过 24 MiB。")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("配置文件不可读。")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBlob {
		return nil, errors.New("配置文件无效或超过 24 MiB。")
	}
	blob, err := io.ReadAll(io.LimitReader(f, MaxBlob+1))
	if err != nil || !ValidEnvelope(blob) {
		clear(blob)
		return nil, errors.New("加密配置文件无效或超过 24 MiB。")
	}
	return blob, nil
}

func WriteArchive(ctx context.Context, dest string, blob []byte) error {
	f, err := os.CreateTemp(filepath.Dir(dest), ".yaare-backup-*")
	if err != nil {
		return errors.New("无法写入所选备份目录。")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(blob)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = durable.Rename(tmp, dest)
	}
	if err != nil {
		return errors.New("加密备份未完成，原目标文件未修改。")
	}
	if err := durable.SyncDir(filepath.Dir(dest)); err != nil {
		return errors.New("无法确认备份已写入磁盘。")
	}
	return nil
}

func SaveRestoreCopy(ctx context.Context, root string, blob []byte, now time.Time, beforeImport bool) (string, error) {
	id := workspace.NewID()
	if id == "" || !ValidEnvelope(blob) {
		return "", errors.New("恢复前副本格式无效。")
	}
	folder := filepath.Join(root, "portable-backups")
	if err := os.MkdirAll(folder, 0700); err != nil {
		return "", errors.New("无法保存本机恢复前副本。")
	}
	if info, err := os.Lstat(folder); err != nil || !info.IsDir() {
		return "", errors.New("无法保存本机恢复前副本。")
	}
	if err := durable.SyncDir(root); err != nil {
		return "", errors.New("无法保存本机恢复前副本。")
	}
	name := "restore-sync-" + now.UTC().Format(restoreTimestamp) + "-" + id + ".yaare"
	if beforeImport {
		name = "before-import-" + id + ".yaare"
	}
	dest := filepath.Join(folder, name)
	if err := WriteArchive(ctx, dest, blob); err != nil {
		return "", err
	}
	pruneRestoreCopies(folder, dest, now)
	return id, nil
}

func ReadImportRestoreCopy(root, id string) ([]byte, error) {
	if !restoreID.MatchString(id) {
		return nil, errors.New("导入前副本标识无效。")
	}
	return ReadArchive(filepath.Join(root, "portable-backups", "before-import-"+id+".yaare"))
}

func pruneRestoreCopies(folder, keep string, now time.Time) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	type item struct {
		path    string
		modTime time.Time
	}
	items := []item{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !ownedRestoreCopy(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, item{path: filepath.Join(folder, entry.Name()), modTime: info.ModTime()})
	}
	if len(items) <= portableBackupMinKeep {
		return
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].path == keep || items[j].path == keep {
			return items[i].path == keep && items[j].path != keep
		}
		if !items[i].modTime.Equal(items[j].modTime) {
			return items[i].modTime.After(items[j].modTime)
		}
		return items[i].path > items[j].path
	})
	cutoff := now.Add(-portableBackupRetention)
	for i := portableBackupMinKeep; i < len(items); i++ {
		if i >= portableBackupMaxKeep || items[i].modTime.Before(cutoff) {
			_ = os.Remove(items[i].path)
		}
	}
}
