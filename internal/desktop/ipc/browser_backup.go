package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"yaare/internal/adapters/cloudsync"
)

func (b *BrowserServices) importBrowserBackup(ctx context.Context, args []json.RawMessage) (any, error) {
	var token, stage, name string
	if e := decodeBrowserArgs(args, &token, &stage, &name); e != nil {
		return nil, e
	}
	if !browserRelative(stage) || strings.Contains(stage, "/") || !strings.HasPrefix(stage, "import-") || !browserRelative(name) || path.Base(name) != name {
		return nil, errors.New("请选择有效的 Yaare 加密配置文件。")
	}
	if e := b.backup.begin(); e != nil {
		return nil, e
	}
	defer b.backup.end()
	b.localMu.Lock()
	folder := b.local[callerPage(ctx)]
	b.localMu.Unlock()
	if folder == nil || folder.folder.Token != token {
		return nil, errors.New("浏览器文件区不可用。")
	}
	p := "files/" + stage
	info, e := folder.parent.Lstat(p)
	if e != nil || !info.Mode().IsRegular() || info.Size() > cloudsync.MaxBlob {
		return nil, errors.New("请选择有效的 Yaare 加密配置文件。")
	}
	f, e := folder.parent.Open(p)
	if e != nil {
		return nil, e
	}
	defer folder.parent.Remove(p)
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, cloudsync.MaxBlob+1))
	if e != nil {
		return nil, e
	}
	defer clear(data)
	return b.backup.stageImport(ctx, name, data)
}

func (b *BrowserServices) PrepareBrowserArchive(ctx context.Context, method string, args []json.RawMessage) (*BrowserDownload, error) {
	if e := workspaceCaller(ctx); e != nil {
		return nil, e
	}
	if e := b.backup.begin(); e != nil {
		return nil, e
	}
	defer b.backup.end()
	if _, e := b.beginBrowserLocal(ctx); e != nil {
		return nil, e
	}
	page := callerPage(ctx)
	b.localMu.Lock()
	folder := b.local[page]
	b.localMu.Unlock()
	var blob []byte
	var e error
	name := "Yaare.yaare"
	switch method {
	case "BrowserBackup.Export":
		var q BackupExportRequest
		if e = decodeBrowserArgs(args, &q); e == nil {
			blob, e = b.backup.exportBytes(ctx, q)
		}
	case "BrowserBackup.RestorePoint":
		var id string
		if e = decodeBrowserArgs(args, &id); e == nil {
			blob, e = b.backup.restorePointBytes(ctx, id)
		}
		name = "Yaare-before-import.yaare"
	default:
		return nil, errors.New("请求方法无效。")
	}
	if e != nil {
		return nil, e
	}
	defer clear(blob)
	p := "incoming/" + name
	if e = folder.parent.Remove(p); e != nil && !errors.Is(e, fs.ErrNotExist) {
		return nil, e
	}
	f, e := folder.parent.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, e
	}
	_, e = f.Write(blob)
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return nil, e
	}
	info, e := folder.parent.Stat(p)
	if e != nil {
		return nil, e
	}
	return &BrowserDownload{folder: folder.parent, entries: []browserExportEntry{{Path: p}}, versions: []fs.FileInfo{info}, Name: name, Size: info.Size()}, nil
}
