package ipc

import (
	"bytes"
	"context"
	"encoding/json"

	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/preferences"

	"testing"
)

func cloudFixture(t *testing.T, keyByte byte) (*CloudService, *sqlite.Store, context.Context) {
	t.Helper()
	dir := t.TempDir()
	store := sqlite.New(func() (string, error) { return dir, nil })
	if e := store.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	p, _ := newTestVault(bytes.Repeat([]byte{keyByte}, 32))
	return NewCloudService(store, p), store, testOwner(context.Background())
}
func TestPortablePreferencesWhitelist(t *testing.T) {
	for _, raw := range []string{`{"password":"secret"}`, `{"fontSize":7}`, `{"fontSize":33}`, `{"fontSize":14.5}`, `{"autoReconnect":true}`, `{"transferConcurrency":2}`} {
		if _, e := preferences.DecodePortable(json.RawMessage(raw)); e == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"fontSize":8}`, `{"fontSize":32}`} {
		if _, e := preferences.DecodePortable(json.RawMessage(raw)); e != nil {
			t.Fatalf("%s rejected: %v", raw, e)
		}
	}
}
