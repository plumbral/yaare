package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
)

var ErrInvalidBaseline = errors.New("同步基线不可用，请先检查并合并配置。")

type PrefetchedRemote struct {
	Blob []byte
	ETag string
}

type RemoteRequest struct {
	Baseline                       []byte
	RestoreName                    string
	Password                       []byte
	Keys                           *cloudsync.KeyCache
	IncludeCredentials, Background bool
	Prefetch                       *PrefetchedRemote
	Open                           func() (configsync.Storage, error)
	Validator                      func() (string, error)
}

type RemoteState struct {
	Base                        syncmodel.Payload
	BaseGeneration              int64
	Document                    Document
	ETag                        string
	Unchanged                   bool
	BaselineBlob, PublishedBlob []byte
}

func ReadRemote(ctx context.Context, request RemoteRequest) (state RemoteState, err error) {
	state.Base = syncmodel.Payload{Version: syncmodel.Version, Snapshot: workspace.Empty(), Preferences: json.RawMessage(`{}`)}
	if request.Keys == nil {
		var keys cloudsync.KeyCache
		request.Keys = keys.Bind("request")
		defer keys.Clear()
	}
	var base Document
	defer func() {
		if err != nil {
			syncmodel.ClearPayload(state.Base)
			syncmodel.ClearPayload(state.Document.Workspace)
			state = RemoteState{}
		}
	}()
	if len(request.Baseline) > 0 {
		base, err = openDocument(request.Baseline, request.Password, configsync.CurrentObject, request.Keys)
		if err != nil {
			return state, ErrInvalidBaseline
		}
		state.Base, state.BaseGeneration = base.Workspace, base.Generation
	}

	restoring := request.RestoreName != ""
	name := configsync.CurrentObject
	if restoring {
		if _, err = BackupTime(request.RestoreName); err != nil {
			return state, err
		}
		name = request.RestoreName
	}
	var blob []byte
	if request.Prefetch != nil && !restoring {
		blob, state.ETag = request.Prefetch.Blob, request.Prefetch.ETag
	} else {
		target, openErr := request.Open()
		if openErr != nil {
			return state, openErr
		}
		defer target.Close()
		var validator string
		if request.Background && !restoring && base.IncludeCredentials == request.IncludeCredentials {
			validator, err = request.Validator()
			if err != nil {
				return state, err
			}
		}
		if validator != "" {
			blob, state.ETag, state.Unchanged, err = target.GetCurrentIfChanged(ctx, validator)
		} else {
			blob, state.ETag, err = target.Get(ctx, name)
		}
		if errors.Is(err, configsync.ErrMissing) && len(request.Baseline) == 0 && !restoring {
			state.Document = Document{Workspace: state.Base, IncludeCredentials: request.IncludeCredentials}
			state.ETag = ""
			return state, nil
		}
		if err != nil {
			return state, err
		}
	}
	state.Document = base
	if !state.Unchanged {
		state.Document, err = openDocument(blob, request.Password, name, request.Keys)
		if err != nil {
			return state, err
		}
	}
	if restoring {
		if err = validateBackupTime(name, state.Document.Created, cloudsync.Legacy(blob)); err != nil {
			return state, err
		}
	}
	if !restoring && state.Document.Generation < state.BaseGeneration {
		return state, errors.New("远端配置早于已同步版本，请检查存储或通过备份恢复。")
	}
	if !restoring && state.Document.IncludeCredentials != request.IncludeCredentials {
		return state, errors.New("两端的认证同步范围不同，请使用相同的范围或独立目录。")
	}
	if !state.Unchanged {
		state.PublishedBlob = blob
		if !restoring {
			state.BaselineBlob = bytes.Clone(blob)
		}
	}
	return state, nil
}
