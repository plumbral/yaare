package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/preferences"
	"yaare/internal/domain/workspace"
)

func changeFixturePreferences(t *testing.T, ctx context.Context, store *sqlite.Store, portable *preferences.Portable, device *preferences.Device) preferences.Snapshot {
	t.Helper()
	current, err := store.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.SavePreferences(ctx, preferences.SaveRequest{Version: preferences.Version, Revision: current.Revision, Portable: portable, Device: device})
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestV2SyncPreferencesAndExtensionsAcrossTwoDevices(t *testing.T) {
	target := fixtureS3(t, nil)
	aCloud, storeA, ctxA := cloudFixture(t, 201)
	bCloud, storeB, ctxB := cloudFixture(t, 202)
	a, b := NewSyncService(aCloud), NewSyncService(bCloud)
	saveSyncFixture(t, ctxA, a, target)
	saveSyncFixture(t, ctxB, b, target)
	device := preferences.Empty().Device
	device.FontFamily = "local:My Device Mono"
	custom := "alt+b"
	device.Shortcuts["hosts"] = &custom
	changeFixturePreferences(t, ctxB, storeB, nil, &device)
	changeFixturePreferences(t, ctxA, storeA, &preferences.Portable{Mode: "dark", FontSize: 18}, nil)
	data, err := storeA.CloudRead(ctxA)
	if err != nil {
		t.Fatal(err)
	}
	data.Extensions = workspace.PortableExtensions{"example.optional": json.RawMessage(`{"enabled":true}`)}
	if err = storeA.CloudReplace(ctxA, data.Snapshot.Revision, data); err != nil {
		t.Fatal(err)
	}
	syncNow(t, a, ctxA)
	syncNow(t, b, ctxB)
	got, err := storeB.Preferences(ctxB)
	if err != nil || got.Portable.Mode != "dark" || got.Portable.FontSize != 18 || got.Device.FontFamily != device.FontFamily || got.Device.Shortcuts["hosts"] == nil || *got.Device.Shortcuts["hosts"] != custom {
		t.Fatal("portable sync did not preserve the receiving device settings", err)
	}
	got.Portable.TerminalPalette = "tokyo-auto"
	changeFixturePreferences(t, ctxB, storeB, &got.Portable, nil)
	syncNow(t, b, ctxB)
	syncNow(t, a, ctxA)
	data, err = storeA.CloudRead(ctxA)
	if err != nil || !bytes.Equal(data.Extensions["example.optional"], []byte(`{"enabled":true}`)) || !bytes.Contains(data.Preferences, []byte("tokyo-auto")) {
		t.Fatal("the later preference write dropped extension data", err)
	}
	if bytes.Contains(data.Preferences, []byte(device.FontFamily)) {
		t.Fatal("a local-only font crossed devices")
	}
}

func TestV2BackupRestoresPortablePreferencesEvenWithoutCredentials(t *testing.T) {
	a, sa, ca, _ := backupFixture(t, 203)
	changeFixturePreferences(t, ca, sa, &preferences.Portable{Mode: "dark", CursorBlink: true}, nil)
	blob, err := a.exportBytes(ca, BackupExportRequest{Password: backupPassword})
	if err != nil {
		t.Fatal(err)
	}
	b, sb, cb, _ := backupFixture(t, 204)
	device := preferences.Empty().Device
	device.TransferConcurrency = 4
	changeFixturePreferences(t, cb, sb, &preferences.Portable{Mode: "light"}, &device)
	file := chooseBackup(t, b, cb, blob)
	preview, err := b.Preview(cb, file.ID, backupPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Apply(cb, preview.ID); err != nil {
		t.Fatal(err)
	}
	got, err := sb.Preferences(cb)
	if err != nil || got.Portable.Mode != "dark" || !got.Portable.CursorBlink || got.Device.TransferConcurrency != 4 {
		t.Fatal("backup did not preserve device-local preferences", err)
	}
}

func TestV2SyncRejectsFutureReaderAndUnrecognizedFields(t *testing.T) {
	document := syncapp.NewDocument(1, false, time.Now(), syncmodel.Payload{Version: syncmodel.Version, Snapshot: workspace.Empty()})
	document.MinReaderVersion++
	sealed, err := syncapp.SealDocument(document, storageTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = syncapp.OpenDocument(sealed, storageTestPassword); err == nil {
		t.Fatal("future minimum reader accepted")
	}
	document.MinReaderVersion = syncmodel.Version
	payload, _ := json.Marshal(document)
	payload = append(payload[:len(payload)-1], []byte(`,"notAnExtension":true}`)...)
	sealed, err = cloudsync.Seal(payload, storageTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = syncapp.OpenDocument(sealed, storageTestPassword); err == nil {
		t.Fatal("unrecognized required field accepted")
	}
}
