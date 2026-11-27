package ipc

import (
	"context"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"

	"errors"

	"time"

	"yaare/internal/adapters/configsync"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

func (s *SyncService) History(ctx context.Context) ([]SyncBackup, error) {
	if e := workspaceCaller(ctx); e != nil {
		return nil, e
	}
	s.runner.LockInteractive()
	defer s.runner.Unlock()
	_, c, e := s.load(ctx)
	defer c.clearSecrets()
	if e != nil {
		return nil, e
	}
	target, e := s.target(c)
	if e != nil {
		return nil, e
	}
	defer target.Close()
	objects, e := target.List(ctx)
	if e != nil {
		return nil, e
	}
	out := []SyncBackup{}
	for _, o := range objects {
		created, e := syncapp.BackupTime(o.Name)
		if e != nil {
			continue
		}
		out = append(out, SyncBackup{o.Name, created.Format(time.RFC3339), o.Size})
	}
	return out, nil
}
func (s *SyncService) backup(ctx context.Context, r sqlite.SyncRecord, c syncSaved, force bool) error {
	p, _, e := s.capture(ctx, c)
	if e != nil {
		return e
	}
	defer syncmodel.ClearPayload(p)
	hash := syncmodel.PayloadHash(p)
	if !force && hash == c.LastBackupHash {
		return nil
	}
	s.setTask("backup", "prepare", "")
	defer s.setTask("", "", "")
	target, e := s.target(c)
	if e != nil {
		return e
	}
	defer target.Close()
	d := syncapp.NewDocument(0, c.IncludeCredentials, time.Now(), p)
	s.taskStage("upload")
	if e = syncapp.WriteBackup(ctx, target, d, c.Password, s.keys.Bind(c.ReceiptEpoch)); e != nil {
		return e
	}
	s.taskStage("confirm")
	c.LastBackup = time.Now().UnixMilli()
	c.LastBackupHash = hash
	c.Issue = ""
	r, e = s.record(r, c, nil)
	if e != nil {
		return e
	}
	if e = s.cloud.store.SaveSync(ctx, r); e != nil {
		return e
	}
	if err := syncapp.PruneHistory(ctx, target); err != nil {
		current, saved, loadErr := s.load(ctx)
		defer saved.clearSecrets()
		if loadErr == nil {
			saved.Issue = "history"
			if current, loadErr = s.record(current, saved, nil); loadErr == nil {
				_ = s.cloud.store.SaveSync(ctx, current)
			}
		}
	}
	return nil
}
func (s *SyncService) BackupNow(ctx context.Context) error {
	if e := workspaceCaller(ctx); e != nil {
		return e
	}
	s.runner.LockInteractive()
	defer s.runner.Unlock()
	r, c, e := s.load(ctx)
	defer c.clearSecrets()
	if e != nil {
		return e
	}
	return s.backup(ctx, r, c, true)
}
func (s *SyncService) restore(ctx context.Context, p syncPlan, target configsync.Storage) (CloudResult, error) {
	data, e := s.materialize(ctx, p.remote, p.include)
	if e != nil {
		return CloudResult{}, e
	}
	all, _, e := (syncapp.NewPortableWorkspace(s.cloud.store, s.cloud.protector)).Capture(ctx, true)
	if e != nil {
		return CloudResult{}, e
	}
	defer syncmodel.ClearPayload(all)
	localCopy, e := sealBackup(all, p.saved.Password, true)
	if e != nil {
		return CloudResult{}, e
	}
	defer clear(localCopy)
	if e = s.cloud.store.PreservePortable(ctx, localCopy); e != nil {
		return CloudResult{}, e
	}
	rollback := syncapp.NewDocument(0, p.saved.IncludeCredentials, time.Now(), p.local)
	if e = syncapp.WriteBackup(ctx, target, rollback, p.saved.Password, s.keys.Bind(p.saved.ReceiptEpoch)); e != nil {
		return CloudResult{}, errors.New("恢复前副本未能保存，当前配置未修改。")
	}
	c := p.saved
	c.ReceiptEpoch = workspace.NewID()
	if c.ReceiptEpoch == "" {
		return CloudResult{}, errors.New("无法安全记录待确认的同步写入。")
	}
	c.Issue = ""
	r := p.record
	r.Paused = true
	r, e = s.record(r, c, nil)
	if e != nil {
		return CloudResult{}, e
	}
	if e = s.cloud.store.RestoreSync(ctx, p.revision, data, r); e != nil {
		return CloudResult{}, e
	}
	s.keys.Clear()
	s.clearSyncReceipt()
	return CloudResult{State: "restored", Pulled: true}, nil
}
