package sync

import (
	"context"
	"errors"
	"strings"
	"time"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/workspace"
)

type historyPruner interface {
	PruneBackups(context.Context, int, string) error
}

const backupTimestamp = "20060102T150405Z"

func BackupTime(name string) (time.Time, error) {
	if !configsync.ValidBackupName(name) {
		return time.Time{}, errors.New("备份名称无效。")
	}
	return time.Parse(backupTimestamp, name[len("backups/"):len("backups/")+len(backupTimestamp)])
}

func WriteBackup(ctx context.Context, target configsync.Storage, doc Document, password []byte, keys *cloudsync.KeyCache) error {
	sealer, err := cloudsync.NewSealer(password, keys)
	if err != nil {
		return err
	}
	defer sealer.Close()
	return writeBackup(ctx, target, doc, sealer)
}

func writeBackup(ctx context.Context, target configsync.Storage, doc Document, sealer *cloudsync.Sealer) error {
	at, err := time.Parse(time.RFC3339, doc.Created)
	if err != nil {
		return err
	}
	id := workspace.NewID()
	if id == "" {
		return errors.New("无法创建备份名称。")
	}
	name := "backups/" + at.UTC().Format(backupTimestamp) + "-" + id + configsync.ObjectExtension
	blob, err := sealDocument(doc, sealer, name)
	if err != nil {
		return err
	}
	_, err = target.Put(ctx, name, blob, "")
	return err
}

func validateBackupTime(name string, created string, legacy bool) error {
	named, err := BackupTime(name)
	if err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return err
	}
	delta := named.Sub(at)
	if delta == 0 || (legacy && delta.Abs() <= 5*time.Minute) {
		return nil
	}
	return errors.New("备份文件名与加密配置的创建时间不一致。")
}

func PruneHistory(ctx context.Context, target configsync.Storage) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -30).Format("20060102T150405Z")
	if pruner, ok := target.(historyPruner); ok {
		return pruner.PruneBackups(ctx, 120, cutoff)
	}
	objects, e := target.List(ctx)
	if e != nil {
		return e
	}
	validCount := 0
	for _, o := range objects {
		if !configsync.ValidBackupName(o.Name) {
			continue
		}
		validCount++
		if validCount == 1 {
			continue
		}
		if strings.TrimPrefix(o.Name, "backups/")[:16] < cutoff || validCount > 120 {
			if e = target.Delete(ctx, o.Name); e != nil {
				return e
			}
		}
	}
	return nil
}
