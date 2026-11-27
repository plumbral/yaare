package webserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"
	"time"
	"yaare/internal/adapters/cloudsync"
)

func TestBrowserPortableArchivePreviewAndRestore(t *testing.T) {
	s := startTest(t)
	s.login(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p := protocol(t, s, ctx)
	defer p.conn.CloseNow()
	other := protocol(t, s, ctx)
	defer other.conn.CloseNow()
	pass := "browser portable test passphrase"
	protocolCall(t, p, "SyncService.Save", map[string]any{"version": 0, "target": map[string]any{"provider": "yaare", "endpoint": s.origin}, "password": pass})
	href := protocolString(t, p, "BrowserBackup.Export", map[string]any{"password": pass, "includeCredentials": true})
	resp := s.request(t, "GET", href, "", "")
	blob, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || resp.StatusCode != 200 || !cloudsync.ValidEnvelope(blob) || bytes.Contains(blob, []byte(pass)) {
		t.Fatal("invalid export", e, resp.Status)
	}
	var folder struct{ Token string }
	if json.Unmarshal(protocolCall(t, p, "BrowserFiles.Begin"), &folder) != nil {
		t.Fatal("folder")
	}
	u := protocolString(t, p, "BrowserFiles.UploadBegin", map[string]any{"token": folder.Token, "path": "import-test", "size": len(blob)})
	protocolCall(t, p, "BrowserFiles.UploadChunk", map[string]any{"id": u, "offset": 0, "finish": true, "data": base64.StdEncoding.EncodeToString(blob)})
	if _, e = other.call("BrowserBackup.Import", folder.Token, "import-test", "copy.yaare"); e == nil {
		t.Fatal("foreign import")
	}
	var selected struct{ ID string }
	if json.Unmarshal(protocolCall(t, p, "BrowserBackup.Import", folder.Token, "import-test", "copy.yaare"), &selected) != nil {
		t.Fatal("selection")
	}
	if _, e = p.call("BackupService.Preview", selected.ID, "wrong password for archive"); e == nil {
		t.Fatal("wrong password accepted")
	}
	var preview struct{ ID string }
	if json.Unmarshal(protocolCall(t, p, "BackupService.Preview", selected.ID, pass), &preview) != nil || preview.ID == "" {
		t.Fatal("preview")
	}
	var result struct{ RestorePoint string }
	if json.Unmarshal(protocolCall(t, p, "BackupService.Apply", preview.ID), &result) != nil || result.RestorePoint == "" {
		t.Fatal("missing rollback")
	}
	if _, e = other.call("BrowserBackup.RestorePoint", result.RestorePoint); e == nil {
		t.Fatal("foreign restore point")
	}
	href = protocolString(t, p, "BrowserBackup.RestorePoint", result.RestorePoint)
	resp = s.request(t, "GET", href, "", "")
	rollback, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || resp.StatusCode != 200 || !cloudsync.ValidEnvelope(rollback) {
		t.Fatal("restore copy invalid")
	}
	var state struct{ Paused bool }
	if json.Unmarshal(protocolCall(t, p, "SyncService.Settings"), &state) != nil || !state.Paused {
		t.Fatal("sync not paused after restore")
	}
}
