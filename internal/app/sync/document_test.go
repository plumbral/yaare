package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
)

func TestSyncDocumentRoundTripAndUnknownFields(t *testing.T) {
	snapshot := workspace.Empty()
	snapshot.Commands = []workspace.Command{{ID: "pwd", Name: "Working directory", Command: "pwd", Group: workspace.Ungrouped, Icon: "linux"}}
	raw, err := json.Marshal(NewDocument(4, false, time.Now(), syncmodel.Payload{Version: syncmodel.Version, Snapshot: snapshot}))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := DecodeDocument(raw)
	if err != nil || doc.Generation != 4 || len(doc.Workspace.Snapshot.Commands) != 1 || doc.Workspace.Snapshot.Commands[0].ID != "pwd" {
		t.Fatal("could not read configuration", err)
	}
	unknown := bytes.Replace(raw, []byte(`"Workspace":{`), []byte(`"Workspace":{"Unexpected":true,`), 1)
	if bytes.Equal(unknown, raw) {
		t.Fatal("missing workspace in fixture")
	}
	if _, err := DecodeDocument(unknown); err == nil {
		t.Fatal("unknown workspace field accepted")
	}
}

type historyStore struct {
	configsync.Storage
	name string
	blob []byte
}

func (s *historyStore) Get(context.Context, string) ([]byte, string, error) {
	return s.blob, "history-etag", nil
}

func (s *historyStore) Put(_ context.Context, key string, data []byte, expected string) (string, error) {
	s.name, s.blob = key, data
	return "etag", nil
}

func (s *historyStore) Close() error { return nil }

func TestRestoreAuthenticatesHistoryObjectAndCreationTime(t *testing.T) {
	const legacyFixture = "WUFBUkUyAAABMDEyMzQ1Njc4OWFiY2RlZmFiY2RlZmdoaWprbDfU9pKxT3yOXneHwyKIbwRRcU7sLAakQ4FljaGmcCBxLS4boiYbxiXhbITI8ev273ECWd5jQ8YmIaQde6zuuJExu508xLev3hLQYQJK9JPoKK+0ZdBT9wI9v4ZdmO0I95pd9Ecj8gKQVj8H6OkkcqU2yvknToKEZVDK1eTcgH8iBp9HLlTKrYAh2NB72piOdNS8gaQ="
	const original = "backups/20260102T030405Z-00000000000000000000000000000001.yaaresync"
	pass := []byte("legacy fixture password")
	var cache cloudsync.KeyCache
	cache.Bind("fixture")
	defer cache.Clear()
	store := &historyStore{}
	var err error
	store.blob, err = base64.StdEncoding.DecodeString(legacyFixture)
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) (RemoteState, error) {
		return ReadRemote(t.Context(), RemoteRequest{RestoreName: name, Password: pass, Keys: &cache, Open: func() (configsync.Storage, error) { return store, nil }})
	}

	legacy, err := read(strings.Replace(original, "030405", "030905", 1))
	if err != nil || legacy.Document.Created != "2026-01-02T03:04:05Z" {
		t.Fatal("legacy history unreadable", err)
	}
	if _, err := read(strings.Replace(original, "030405", "030906", 1)); err == nil {
		t.Fatal("accepted legacy name beyond tolerance")
	}
	if _, err := read(""); err != nil {
		t.Fatal("legacy current unreadable", err)
	}
	if raw, err := cloudsync.Open(store.blob, pass); err != nil || !bytes.Contains(raw, []byte("Created")) {
		t.Fatal("legacy portable envelope unreadable", err)
	}

	if err := WriteBackup(t.Context(), store, legacy.Document, pass, &cache); err != nil {
		t.Fatal(err)
	}
	if _, err := read(store.name); err != nil {
		t.Fatal("published backup unreadable", err)
	}
	if _, err := read(original); err == nil {
		t.Fatal("accepted renamed backup")
	}
	if _, err := read(""); err == nil {
		t.Fatal("accepted backup in current slot")
	}
	if _, err := cloudsync.Open(store.blob, pass); err == nil {
		t.Fatal("accepted backup as archive")
	}

	sealer, err := cloudsync.NewSealer(pass, &cache)
	if err != nil {
		t.Fatal(err)
	}
	defer sealer.Close()
	wrong := strings.Replace(original, "030405", "030406", 1)
	store.blob, err = sealDocument(legacy.Document, sealer, wrong)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = read(wrong); err == nil {
		t.Fatal("accepted new backup with inconsistent authenticated time")
	}
}
