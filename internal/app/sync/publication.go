package sync

import (
	"context"
	"time"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/syncmodel"
)

type MergePlan struct {
	Base, Local, Remote                              syncmodel.Payload
	RemoteBlob                                       []byte
	ETag, Hash, Issue                                string
	Password                                         []byte
	Keys                                             *cloudsync.KeyCache
	Generation, BaseGeneration                       int64
	RemoteUnchanged, IncludeCredentials, HasBaseline bool
}

type MergeResult struct {
	State  string
	Pulled bool
}

type MergeCommit struct {
	Data                               syncmodel.LocalData
	State                              []byte
	Document                           Document
	ETag                               string
	RetainBaseline, Publishing, Pulled bool
}

type PublicationHooks struct {
	Reconcile          func(context.Context, *MergePlan) (bool, error)
	Materialize        func(context.Context, syncmodel.Payload, bool) (syncmodel.LocalData, error)
	Validate           func(context.Context, syncmodel.Payload, bool) error
	Stage              func(string)
	SaveReceipt        func(context.Context, []byte) error
	AcceptUnchanged    func(string)
	Commit             func(context.Context, MergeCommit) error
	ReportHistoryError func(context.Context)
}

func PublishMerge(ctx context.Context, p MergePlan, target configsync.Storage, choices map[string]string, hooks PublicationHooks) (MergeResult, error) {
	if p.RemoteUnchanged && p.Issue == "" && p.Hash == syncmodel.PayloadHash(p.Base) {
		return MergeResult{State: "equal"}, nil
	}
	receiptApplied, e := hooks.Reconcile(ctx, &p)
	if e != nil {
		return MergeResult{}, e
	}
	next, conflicts, e := syncmodel.Merge(p.Base, p.Local, p.Remote, choices)
	defer syncmodel.ClearPayload(next)
	if e != nil {
		return MergeResult{}, e
	}
	if len(conflicts) > 0 {
		return MergeResult{State: "conflict"}, nil
	}
	nextHash := syncmodel.PayloadHash(next)
	pulled := nextHash != p.Hash
	var data syncmodel.LocalData
	if pulled {
		data, e = hooks.Materialize(ctx, next, p.IncludeCredentials)
	} else {
		e = hooks.Validate(ctx, next, p.IncludeCredentials)
	}
	if e != nil {
		return MergeResult{}, e
	}
	changed := nextHash != syncmodel.PayloadHash(p.Remote)
	publishing := changed || p.ETag == ""
	generation := p.Generation
	d := NewDocument(generation, p.IncludeCredentials, time.Now(), next)
	var stateBlob []byte
	if publishing {
		hooks.Stage("upload")
		sealer, err := cloudsync.NewSealer(p.Password, p.Keys)
		if err != nil {
			return MergeResult{}, err
		}
		defer sealer.Close()
		if len(choices) > 0 {
			for _, snapshot := range []syncmodel.Payload{p.Remote, p.Local} {
				doc := NewDocument(0, p.IncludeCredentials, time.Now(), snapshot)
				if e := writeBackup(ctx, target, doc, sealer); e != nil {
					return MergeResult{}, e
				}
			}
		}
		d.Generation++
		blob, e := sealDocument(d, sealer, configsync.CurrentObject)
		if e != nil {
			return MergeResult{}, e
		}
		if e := writeBackup(ctx, target, d, sealer); e != nil {
			return MergeResult{}, e
		}
		if e = hooks.SaveReceipt(ctx, blob); e != nil {
			return MergeResult{}, e
		}
		if p.ETag, e = target.Put(ctx, configsync.CurrentObject, blob, p.ETag); e != nil {
			return MergeResult{}, e
		}
		stateBlob = blob
	} else {
		etag, e := target.Head(ctx, configsync.CurrentObject)
		if e != nil {
			return MergeResult{}, e
		}
		if etag != p.ETag {
			return MergeResult{}, configsync.ErrChanged
		}
		if !pulled && p.HasBaseline && nextHash == syncmodel.PayloadHash(p.Base) && p.Generation == p.BaseGeneration && p.Issue == "" {
			hooks.AcceptUnchanged(p.ETag)
			return MergeResult{State: "equal", Pulled: receiptApplied}, nil
		}
		if len(p.RemoteBlob) > 0 {
			stateBlob = p.RemoteBlob
		}
	}
	commit := MergeCommit{
		Data: data, State: stateBlob, Document: d, ETag: p.ETag,
		RetainBaseline: p.RemoteUnchanged && p.HasBaseline, Publishing: publishing, Pulled: pulled,
	}
	if err := hooks.Commit(ctx, commit); err != nil {
		return MergeResult{}, err
	}
	if publishing {
		if err := PruneHistory(ctx, target); err != nil {
			hooks.ReportHistoryError(ctx)
		}
	}
	return MergeResult{State: "equal", Pulled: pulled || receiptApplied}, nil
}
