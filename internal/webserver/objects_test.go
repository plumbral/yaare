package webserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
)

func deviceRequest(t *testing.T, s *testInstance, method, body string) *http.Response {
	t.Helper()
	r, e := http.NewRequest(method, s.origin+"/api/devices", strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	r.AddCookie(s.cookie)
	r.Header.Set("Origin", s.origin)
	r.Header.Set("X-Yaare-CSRF", s.csrf)
	resp, e := s.client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	return resp
}
func TestDeviceCASRevocationAndInstanceKey(t *testing.T) {
	s := startTest(t)
	s.login(t)
	response := deviceRequest(t, s, "POST", "")
	var pair struct{ ID, Token string }
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&pair) != nil || len(pair.Token) != 43 {
		t.Fatal("pairing failed")
	}
	response.Body.Close()
	target, e := configsync.NewHTTP(configsync.Config{Provider: "yaare", Endpoint: s.origin}, []byte(pair.Token))
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	ctx := context.Background()
	if e = target.Prepare(ctx, "probe-"+strings.Repeat("a", 32), true); e != nil {
		t.Fatal(e)
	}
	blob, e := cloudsync.Seal([]byte("test workspace"), "test passphrase for configuration")
	if e != nil {
		t.Fatal(e)
	}
	tag, e := target.Put(ctx, "current.yaaresync", blob, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, err := target.Put(ctx, "current.yaaresync", []byte("unencrypted workspace"), tag); err == nil {
		t.Fatal("accepted plaintext")
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := target.Put(ctx, "current.yaaresync", blob, tag); results <- err }()
	}
	wins := 0
	for range 2 {
		err := <-results
		if err == nil {
			wins++
		} else if !errors.Is(err, configsync.ErrChanged) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("concurrent conditional write", wins)
	}
	name := "backups/20261124T000000Z-" + strings.Repeat("b", 32) + ".yaaresync"
	if _, e = target.Put(ctx, name, blob, "*"); e != nil {
		t.Fatal(e)
	}
	if _, e = target.Put(ctx, name, blob, "*"); !errors.Is(e, configsync.ErrChanged) {
		t.Fatal("mutable history", e)
	}
	body, current, e := target.Get(ctx, "current.yaaresync")
	if e != nil || !bytes.Equal(body, blob) || current == tag {
		t.Fatal("current invalid", e)
	}
	if _, _, unchanged, e := target.GetCurrentIfChanged(ctx, current); e != nil || !unchanged {
		t.Fatal("conditional read failed", e)
	}
	foreign := "backups/20200101T000000Z-" + strings.Repeat("a", 32) + ".other"
	if _, e := s.objects.db.Exec("INSERT INTO objects(name,tag,body,created) VALUES(?,?,?,0)", foreign, "tag", blob); e != nil {
		t.Fatal(e)
	}
	ids, e := target.List(ctx)
	if e != nil || len(ids) != 1 || ids[0].Name != name {
		t.Fatal("history invalid", e)
	}
	if e := target.PruneBackups(ctx, 1, "20990101T000000Z"); e != nil {
		t.Fatal(e)
	}
	var untouched []byte
	if err := s.objects.db.QueryRow("SELECT body FROM objects WHERE name=?", foreign).Scan(&untouched); err != nil || !bytes.Equal(untouched, blob) {
		t.Fatal("pruning changed an unrelated object", err)
	}
	r := deviceRequest(t, s, "DELETE", `{"id":"`+pair.ID+`"}`)
	r.Body.Close()
	if r.StatusCode != 204 {
		t.Fatal("revoke", r.StatusCode)
	}
	if _, _, e = target.Get(ctx, "current.yaaresync"); e == nil {
		t.Fatal("revoked device still authorized")
	}
	s.Close()
	original, e := os.ReadFile(s.config.KeyFile)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(original)
	other := bytes.Repeat([]byte{7}, 32)
	if e = os.WriteFile(s.config.KeyFile, other, 0600); e != nil {
		t.Fatal(e)
	}
	if reopened, e := Open(ctx, s.config); e == nil {
		reopened.Close()
		t.Fatal("wrong key opened existing volume")
	}
	if e = os.WriteFile(s.config.KeyFile, original, 0600); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(ctx, s.config)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	var retained []byte
	if e = reopened.objects.db.QueryRow("SELECT body FROM objects WHERE name='current.yaaresync'").Scan(&retained); e != nil || !bytes.Equal(retained, blob) {
		t.Fatal("object lost after restart", e)
	}
}
