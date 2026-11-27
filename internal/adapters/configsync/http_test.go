package configsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRealStorageConditionsRoundTripAndAuthentication(t *testing.T) {
	if os.Getenv("YAARE_SYNC_STORAGE_TEST") != "1" {
		t.Skip("isolated Docker WebDAV/S3 services required")
	}
	for _, provider := range []string{"webdav", "s3"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			credential := []byte("Yaare-Sync-Test-Only")
			cfg := Config{Provider: provider, Endpoint: "http://127.0.0.1:4195", Username: "yaare-test", Prefix: "transport-" + strings.ReplaceAll(time.Now().UTC().Format("150405.000000000"), ".", ""), Bucket: "config-test", Region: "us-east-1"}
			cfg.Prefix += "/中文 space+@%"
			if provider == "s3" {
				cfg.Endpoint = "http://127.0.0.1:4194"
				credential = []byte("Yaare-S3-Test-Only")
			}
			h, e := NewHTTP(cfg, credential)
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			if e = h.Prepare(ctx, "probe-"+strings.Repeat("a", 32), true); e != nil {
				debugName := "probe-" + strings.Repeat("c", 32)
				v, err := h.Put(ctx, debugName, []byte("probe"), "")
				t.Log("create", v, err)
				_, getTag, err := h.Get(ctx, debugName)
				t.Log("get", getTag, err)
				_, err = h.Put(ctx, debugName, []byte("duplicate"), "")
				t.Log("IfNoneMatch", err)
				_, err = h.Put(ctx, debugName, []byte("stale"), `"stale"`)
				t.Log("IfMatch-stale", err)
				_, err = h.Put(ctx, debugName, []byte("valid"), v)
				t.Log("IfMatch-current", err)
				_ = h.Delete(ctx, debugName)
				t.Fatal("provider condition check", e)
			}
			name := "backups/20261117T000000Z-" + strings.Repeat("b", 32) + ".yaaresync"
			content := bytes.Repeat([]byte("encrypted fixture\x00"), 5000)
			version, e := h.Put(ctx, name, content, "")
			if e != nil {
				t.Fatal(e)
			}
			got, _, e := h.Get(ctx, name)
			if e != nil || !bytes.Equal(content, got) {
				t.Fatal("roundtrip", e)
			}
			if _, e = h.Put(ctx, name, []byte("incorrect"), ""); !errors.Is(e, ErrChanged) {
				t.Fatal("exclusive creation not enforced", e)
			}
			if _, e = h.Put(ctx, name, []byte("incorrect"), `"stale"`); !errors.Is(e, ErrChanged) {
				t.Fatal("stale write not rejected", e)
			}
			if _, e = h.Put(ctx, name, []byte("changed"), version); e != nil {
				t.Fatal("valid CAS", e)
			}
			_, tag, e := h.Get(ctx, name)
			if e != nil {
				t.Fatal(e)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var group sync.WaitGroup
			for _, content := range [][]byte{bytes.Repeat([]byte("A"), 128<<10), bytes.Repeat([]byte("B"), 128<<10)} {
				group.Add(1)
				go func(data []byte) { defer group.Done(); <-start; _, err := h.Put(ctx, name, data, tag); results <- err }(content)
			}
			close(start)
			group.Wait()
			close(results)
			success, changed := 0, 0
			for err := range results {
				if err == nil {
					success++
				} else if errors.Is(err, ErrChanged) {
					changed++
				} else {
					t.Fatal("concurrent write", err)
				}
			}
			if success != 1 || changed != 1 {
				t.Fatal("CAS lost concurrent update", success, changed)
			}
			list, e := h.List(ctx)
			if e != nil || len(list) != 1 || list[0].Name != name {
				t.Fatal("list", list, e)
			}
			bad, _ := NewHTTP(cfg, []byte("wrong credential"))
			defer bad.Close()
			if _, _, e = bad.Get(ctx, name); e == nil {
				t.Fatal("wrong auth accepted")
			}
			if e = h.Delete(ctx, name); e != nil {
				t.Fatal(e)
			}
			if _, _, e = h.Get(ctx, name); !errors.Is(e, ErrMissing) {
				t.Fatal("delete", e)
			}
			baseline := []byte("last verified current configuration")
			if _, e = h.Put(ctx, "current.yaaresync", baseline, ""); e != nil {
				t.Fatal(e)
			}
			interrupted := "backups/20261117T000002Z-" + strings.Repeat("d", 32) + ".yaaresync"
			partial, abort := context.WithCancel(ctx)
			baseTransport := h.client.Transport
			h.client.Transport = &abortUploadTransport{base: baseTransport, cancel: abort}
			_, e = h.Put(partial, interrupted, bytes.Repeat([]byte("encrypted data"), 1<<18), "")
			h.client.Transport = baseTransport
			abort()
			if e == nil {
				t.Fatal("cancelled upload reported success")
			}
			objects, e := h.List(ctx)
			if e != nil || len(objects) != 0 {
				t.Fatal("partial backup published", objects, e)
			}
			current, _, e := h.Get(ctx, "current.yaaresync")
			if e != nil || !bytes.Equal(current, baseline) {
				t.Fatal("interruption changed current", e)
			}
		})
	}
}

type abortUploadTransport struct {
	base   http.RoundTripper
	cancel context.CancelFunc
}

func TestRealStorageHistoryRetentionAndPrefixIsolation(t *testing.T) {
	if os.Getenv("YAARE_SYNC_STORAGE_TEST") != "1" {
		t.Skip("isolated Docker storage required")
	}
	for _, provider := range []string{"webdav", "s3"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			credential := []byte("Yaare-Sync-Test-Only")
			cfg := Config{Provider: provider, Endpoint: "http://127.0.0.1:4195", Username: "yaare-test", Prefix: fmt.Sprintf("history-%d", time.Now().UnixNano())}
			count := 125
			if provider == "s3" {
				cfg.Endpoint = "http://127.0.0.1:4194"
				credential = []byte("Yaare-S3-Test-Only")
				cfg.Bucket = "config-test"
				cfg.Region = "us-east-1"
				count = 1005
			}
			connect := func(cfg Config) *HTTP {
				h, err := NewHTTP(cfg, credential)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = h.Close() })
				probe, err := newProbe()
				if err != nil {
					t.Fatal(err)
				}
				if err = h.Prepare(ctx, probe, true); err != nil {
					t.Fatal(err)
				}
				return h
			}
			h := connect(cfg)
			cfg.Prefix += "-other"
			sibling := connect(cfg)
			start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			name := func(i int) string {
				return fmt.Sprintf("backups/%s-%032x.yaaresync", start.Add(time.Duration(i)*time.Minute).Format("20060102T150405Z"), i)
			}
			if _, err := sibling.Put(ctx, name(0), []byte("other prefix marker"), ""); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < count; i++ {
				if _, err := h.Put(ctx, name(i), []byte("test history"), ""); err != nil {
					t.Fatalf("create history entry %d: %v", i, err)
				}
			}
			assertHistory := func(want int) {
				t.Helper()
				all, err := h.List(ctx)
				if err != nil || len(all) != want {
					t.Fatalf("expected %d history entries, found %d, error %v", want, len(all), err)
				}
				for i, obj := range all {
					if obj.Name != name(count-1-i) {
						t.Fatalf("history item at position %d missing or out of order", i)
					}
				}
			}
			assertHistory(count)
			for i := 0; i < (count-120+maxPruneDeletes-1)/maxPruneDeletes; i++ {
				if err := h.PruneBackups(ctx, 120, "20000101T000000Z"); err != nil {
					t.Fatal(err)
				}
			}
			assertHistory(120)
			if err := h.PruneBackups(ctx, 120, "20990101T000000Z"); err != nil {
				t.Fatal(err)
			}
			assertHistory(1)
			data, _, err := sibling.Get(ctx, name(0))
			if err != nil || string(data) != "other prefix marker" {
				t.Fatal("retention altered the adjacent prefix", err)
			}
		})
	}
}

func (t *abortUploadTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == "PUT" && r.Body != nil {
		r.Body = &abortUploadBody{ReadCloser: r.Body, left: 64 << 10, cancel: t.cancel}
	}
	return t.base.RoundTrip(r)
}

type abortUploadBody struct {
	io.ReadCloser
	left   int
	cancel context.CancelFunc
}

func (b *abortUploadBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		time.Sleep(2 * time.Millisecond)
		b.cancel()
		return 0, context.Canceled
	}
	if len(p) > b.left {
		p = p[:b.left]
	}
	n, e := b.ReadCloser.Read(p)
	b.left -= n
	return n, e
}
func TestValidationRedirectAndSecretSafeErrors(t *testing.T) {
	for _, endpoint := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com?token=private", "file:///tmp/a"} {
		if _, e := NewHTTP(Config{Provider: "webdav", Endpoint: endpoint}, nil); e == nil {
			t.Fatal("accepted unsafe endpoint", endpoint)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://elsewhere.invalid/?secret=private", 302)
	}))
	defer server.Close()
	h, _ := NewHTTP(Config{Provider: "webdav", Endpoint: server.URL, Username: "sensitive-user"}, []byte("sensitive-secret"))
	defer h.Close()
	_, _, e := h.Get(context.Background(), "current.yaaresync")
	if e == nil || strings.Contains(e.Error(), "sensitive") || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = h.Get(ctx, "current.yaaresync"); !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation", e)
	}
}

func TestHTTPClientOwnsAndClearsCredential(t *testing.T) {
	original := []byte("test credential")
	client, err := NewHTTP(Config{Provider: "webdav", Endpoint: "https://webdav.invalid"}, original)
	if err != nil {
		t.Fatal(err)
	}
	held := client.credential
	clear(original)
	if string(held) != "test credential" {
		t.Fatal("client borrowed caller's buffer")
	}
	client.Close()
	if client.credential != nil || !bytes.Equal(held, make([]byte, len(held))) {
		t.Fatal("closed client retained credential")
	}
}
