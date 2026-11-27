package ipc

import (
	"context"
	syncapp "yaare/internal/app/sync"

	"yaare/internal/adapters/configsync"
	"yaare/internal/adapters/storage/sqlite"
)

func StartConfigurationSync(s *SyncService) func() {
	localChanges, unsubscribe := s.cloud.store.SubscribeLocalChanges()
	return s.runner.Start(localChanges, unsubscribe, s.debounceDelay, s.runBackgroundTick, s.setTask, func(statusCtx context.Context) {
		current, saved, err := s.load(statusCtx)
		defer saved.clearSecrets()
		if err == nil && saved.Issue == "" {
			saved.Issue = "unavailable"
			if current, err = s.record(current, saved, nil); err == nil {
				_ = s.cloud.store.SaveSync(statusCtx, current)
			}
		}
	}, func() {
		s.keys.Clear()
		s.releaseAllPlans()
		s.transport.CloseIdleConnections()
	})
}

func (s *SyncService) runBackgroundTick(run context.Context) error {
	meta, e := s.cloud.store.ReadSyncMeta(run)
	if e != nil {
		return e
	}
	r, c, e := s.decodeSettings(meta.Record)
	defer c.clearSecrets()
	if e != nil || r.Paused || c.Target.Provider == "" {
		return e
	}
	if c.CanSync && c.Issue != "conflict" {
		var prefetched *syncapp.PrefetchedRemote
		var target configsync.Storage
		if c.Issue == "" {
			idleTag, err := s.idleValidator(meta)
			if err != nil {
				return err
			}
			if idleTag != "" {
				target, err = s.target(c)
				if err != nil {
					return err
				}
				blob, etag, unchanged, err := target.GetCurrentIfChanged(run, idleTag)
				if err != nil {
					target.Close()
					return err
				}
				if unchanged {
					target.Close()
					return nil
				}
				prefetched = &syncapp.PrefetchedRemote{Blob: blob, ETag: etag}
			}
		}
		if target != nil {
			defer target.Close()
		}
		s.setTask("sync", "prepare", "")
		full, err := s.cloud.store.ReadSync(run)
		if err != nil || full.Paused {
			return err
		}
		p, err := s.prepareWithPrefetch(run, full, c, "", true, prefetched)
		if err != nil {
			return err
		}
		defer clearSyncPlan(&p)
		if target == nil {
			target, err = s.target(c)
			if err != nil {
				return err
			}
			defer target.Close()
		}
		result, err := s.applyPlan(run, p, target, nil)
		if err != nil {
			return err
		}
		if result.State == "conflict" {
			c.Issue = "conflict"
			var updated sqlite.SyncRecord
			updated, err = s.record(full, c, nil)
			if err == nil {
				err = s.cloud.store.SaveSync(run, updated)
			}
			return err
		}
		return nil
	}
	if !c.CanSync {
		record := r
		if meta.StateHash != "" {
			var err error
			if record, err = s.cloud.store.ReadSync(run); err != nil {
				return err
			}
			if record.Paused {
				return nil
			}
		}
		return s.backup(run, record, c, false)
	}
	return nil
}
