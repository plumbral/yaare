package sqlite

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"yaare/internal/domain/preferences"
	"yaare/internal/domain/workspace"
)

func TestDesktopPreferencesPersistAndTrackPortableRevision(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store := openTest(t, root)
	initial, err := store.Preferences(ctx)
	if err != nil || initial.Revision != 0 || !initial.Device.AutoReconnect {
		t.Fatalf("initial preferences: %+v, %v", initial, err)
	}
	local := initial.Device
	local.UIFont = "local:Segoe UI"
	local.FontFamily = "system-mono"
	local.Shortcuts = map[string]*string{"save": nil}
	visual := preferences.Portable{Language: "ja", FontSize: 18}
	request := preferences.SaveRequest{Version: preferences.Version, Portable: &visual, Device: &local}
	saved, err := store.SavePreferences(ctx, request)
	if err != nil || saved.Revision != 1 || saved.Portable != visual || saved.Device.UIFont != local.UIFont {
		t.Fatalf("save: %+v, %v", saved, err)
	}
	workspaceRevision := store.Load(ctx).Snapshot.Revision
	if workspaceRevision != 1 {
		t.Fatalf("portable mutation did not advance workspace revision: %d", workspaceRevision)
	}
	request.Revision = saved.Revision
	again, err := store.SavePreferences(ctx, request)
	if err != nil || again.Revision != saved.Revision {
		t.Fatalf("unchanged preferences advanced revision: %+v, %v", again, err)
	}
	visual.Language = "en"
	local.TransferConcurrency = 6
	local.AutoReconnect = false
	updated, err := store.SavePreferences(ctx, preferences.SaveRequest{Version: preferences.Version, Revision: saved.Revision, Device: &local})
	if err != nil || updated.Revision != 2 || store.Load(ctx).Snapshot.Revision != workspaceRevision {
		t.Fatalf("local-only edit changed workspace revision: %+v, %v", updated, err)
	}
	_, err = store.SavePreferences(ctx, preferences.SaveRequest{Version: preferences.Version, Revision: saved.Revision, Portable: &visual})
	var conflict *workspace.Error
	if !errors.As(err, &conflict) || conflict.Code != "PREFERENCES_CONFLICT" {
		t.Fatalf("stale edit was not rejected: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openTest(t, root)
	reopened, err := store.Preferences(ctx)
	if err != nil || reopened.Revision != 2 || reopened.Portable.Language != "ja" || reopened.Device.AutoReconnect || reopened.Device.TransferConcurrency != 6 {
		t.Fatalf("reopened preferences: %+v, %v", reopened, err)
	}
	if key, ok := reopened.Device.Shortcuts["save"]; !ok || key != nil {
		t.Fatal("explicit shortcut removal did not persist")
	}
}

func TestDesktopPreferencesPortableTransactionPreservesDevice(t *testing.T) {
	ctx := t.Context()
	store := openTest(t, t.TempDir())
	if raw, err := readPortablePreferences(ctx, store.db); err != nil || raw != nil {
		t.Fatalf("uninitialized portable view: %s, %v", raw, err)
	}
	writePortable := func(raw json.RawMessage, commit bool) {
		t.Helper()
		tx, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := writePortablePreferences(ctx, tx, raw); err != nil {
			t.Fatal(err)
		}
		if commit {
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	writePortable(json.RawMessage(`{"mode":"dark"}`), true)
	current, err := store.Preferences(ctx)
	if err != nil || current.Portable.Mode != "dark" {
		t.Fatalf("first cloud write: %+v, %v", current, err)
	}
	local := current.Device
	local.FallbackFont = "local:Installed Mono"
	local.DetectServerSystem = true
	saved, err := store.SavePreferences(ctx, preferences.SaveRequest{Version: preferences.Version, Revision: current.Revision, Device: &local})
	if err != nil || saved.Portable.Mode != "dark" || saved.Device.FallbackFont != local.FallbackFont {
		t.Fatalf("device change replaced remote preferences: %+v, %v", saved, err)
	}
	writePortable(json.RawMessage(`{"mode":"light"}`), false)
	writePortable(nil, true)
	warm, err := store.CloudRead(ctx)
	if err != nil || string(warm.Preferences) != `{"mode":"dark"}` || strings.Contains(string(warm.Preferences), "Installed") {
		t.Fatalf("portable view after rollback and nil: %q, %v", warm.Preferences, err)
	}
	writePortable(json.RawMessage(`{}`), true)
	cleared, err := store.Preferences(ctx)
	if err != nil || cleared.Portable != (preferences.Portable{}) || cleared.Device.FallbackFont != local.FallbackFont || !cleared.Device.DetectServerSystem || cleared.Revision != saved.Revision+1 {
		t.Fatalf("portable clear affected local data: %+v, %v", cleared, err)
	}
	if raw, err := readPortablePreferences(ctx, store.db); err != nil || string(raw) != "{}" {
		t.Fatalf("explicit clear was not stored: %s, %v", raw, err)
	}
}

func TestDesktopPreferencesRejectInvalidAndPreserveUnreadableRecords(t *testing.T) {
	ctx := t.Context()
	store := openTest(t, t.TempDir())
	for _, raw := range []string{`null`, `{"fontSize":7}`, `{"uiFont":"local:private"}`, `{"autoReconnect":false}`, `{"shortcuts":{"save":"alt+s"}}`} {
		if _, err := preferences.DecodePortable(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid portable data accepted: %s", raw)
		}
	}
	local := preferences.Empty().Device
	local.TransferConcurrency = 9
	if _, err := store.SavePreferences(ctx, preferences.SaveRequest{Version: preferences.Version, Device: &local}); err == nil {
		t.Fatal("invalid local data was saved")
	}
	if _, err := store.db.Exec("INSERT INTO desktop_preferences VALUES(1,2,1,?,NULL)", `{}`); err != nil {
		t.Fatal(err)
	}
	local = preferences.Empty().Device
	if _, err := store.Preferences(ctx); err == nil {
		t.Fatal("unrecognized version read as defaults")
	}
	if _, err := store.SavePreferences(ctx, preferences.SaveRequest{Version: preferences.Version, Revision: 1, Device: &local}); err == nil {
		t.Fatal("unreadable record was overwritten")
	}
}
