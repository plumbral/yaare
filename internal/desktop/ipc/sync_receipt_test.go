package ipc

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	syncapp "yaare/internal/app/sync"

	"os"

	"strings"
	"sync/atomic"
	"testing"

	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/workspace"
)

type syncPublicationObserver func(http.ResponseWriter, *http.Request, http.Handler)
type syncPublicationFixture func(*testing.T, syncPublicationObserver) (configsync.Config, string)

func TestSyncPublicationInterruptedBeforeAndAfterCommit(t *testing.T) {
	testSyncPublicationInterrupted(t, func(t *testing.T, observer syncPublicationObserver) (configsync.Config, string) {
		return fixtureS3(t, observer), storageTestSecret
	})
}

func TestRealSyncPublicationInterruptedBeforeAndAfterCommit(t *testing.T) {
	if os.Getenv("YAARE_SYNC_STORAGE_TEST") != "1" {
		t.Skip("isolated Docker storage required")
	}
	for _, provider := range []string{"webdav", "s3"} {
		t.Run(provider, func(t *testing.T) {
			testSyncPublicationInterrupted(t, func(t *testing.T, observer syncPublicationObserver) (configsync.Config, string) {
				target := configsync.Config{Provider: provider, Username: "yaare-test", Bucket: "config-test", Region: "us-east-1", Prefix: "receipt-" + workspace.NewID()}
				endpoint, credential := "http://127.0.0.1:4195", "Yaare-Sync-Test-Only"
				if provider == "s3" {
					endpoint, credential = "http://127.0.0.1:4194", "Yaare-S3-Test-Only"
				}
				upstream, _ := url.Parse(endpoint)
				proxy := httputil.NewSingleHostReverseProxy(upstream)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					observer(w, r, proxy)
				}))
				t.Cleanup(server.Close)
				target.Endpoint = server.URL
				return target, credential
			})
		})
	}
}

func testSyncPublicationInterrupted(t *testing.T, fixture syncPublicationFixture) {
	t.Helper()
	for _, stage := range []string{"before-commit", "after-commit"} {
		t.Run(stage, func(t *testing.T) {
			var drop atomic.Bool
			var committed atomic.Bool
			target, credential := fixture(t, func(w http.ResponseWriter, r *http.Request, storage http.Handler) {
				publishing := r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/current.yaaresync") ||
					r.Method == "MOVE" && strings.HasSuffix(r.Header.Get("Destination"), "/current.yaaresync")
				if !publishing || !drop.CompareAndSwap(true, false) {
					storage.ServeHTTP(w, r)
					return
				}
				if stage == "after-commit" {
					result := httptest.NewRecorder()
					storage.ServeHTTP(result, r)
					committed.Store(result.Code >= 200 && result.Code < 300)
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					conn.Close()
				}
			})

			cloud, store, ctx := cloudFixture(t, 79)
			service := NewSyncService(cloud)
			if _, err := service.Save(ctx, SyncSaveRequest{
				Target:     target,
				Credential: credential, Password: storageTestPassword,
			}); err != nil {
				t.Fatal(err)
			}
			host := syncHost("lost-receipt")
			editSync(t, store, ctx, func(p *workspace.Snapshot) { p.Hosts = []workspace.Host{host} })
			syncNow(t, service, ctx)
			secondHostID := ""
			if stage == "after-commit" {
				otherCloud, otherStore, otherCtx := cloudFixture(t, 80)
				other := NewSyncService(otherCloud)
				if _, err := other.Save(otherCtx, SyncSaveRequest{
					Target:     target,
					Credential: credential, Password: storageTestPassword,
				}); err != nil {
					t.Fatal(err)
				}
				syncNow(t, other, otherCtx)
				secondHost := syncHost("remote-only")
				secondHostID = secondHost.ID
				editSync(t, otherStore, otherCtx, func(p *workspace.Snapshot) { p.Hosts = append(p.Hosts, secondHost) })
				syncNow(t, other, otherCtx)
			}
			editSync(t, store, ctx, func(p *workspace.Snapshot) { p.Hosts[0].Notes = "first local edit" })
			preview, err := service.Preview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			before, err := store.ReadSync(ctx)
			if err != nil {
				t.Fatal(err)
			}
			drop.Store(true)
			if _, err = service.Apply(ctx, preview.ID, nil); err == nil || drop.Load() || committed.Load() != (stage == "after-commit") {
				t.Fatal("fixture did not simulate the intended failure", err)
			}
			current, err := store.ReadSync(ctx)
			if err != nil || current.Version != before.Version {
				t.Fatal("failed attempt advanced the baseline", err)
			}
			path, err := store.SyncReceiptPath()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("publication intent was not persisted", err)
			}
			editSync(t, store, ctx, func(p *workspace.Snapshot) { p.Hosts[0].Notes = "second local edit" })
			prepared, err := prepareSyncTick(t, ctx, service)
			if err != nil || prepared.remoteUnchanged || (prepared.receipt != nil) != (stage == "after-commit") {
				t.Fatal("cached validator bypassed pending publication verification", err)
			}
			store.Close()
			if err := store.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			service = NewSyncService(cloud)
			preview, err = service.Preview(ctx)
			if err != nil || len(preview.Conflicts) != 0 {
				t.Fatalf("restart raised a false conflict, %d: %v", len(preview.Conflicts), err)
			}
			if _, err := service.Apply(ctx, preview.ID, nil); err != nil {
				t.Fatal(err)
			}
			if secondHostID != "" {
				found := false
				for _, entry := range store.Load(ctx).Snapshot.Hosts {
					found = found || entry.ID == secondHostID
				}
				if !found {
					t.Fatal("receipt recovery discarded the other device's addition")
				}
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("completed publication retained pending receipt", err)
			}
			remote, err := service.Preview(ctx)
			if err != nil || len(remote.Conflicts) != 0 || len(remote.Changed) != 0 {
				t.Fatal("remote did not converge", err)
			}
			transport, err := service.target(mustSaved(t, service, ctx))
			if err != nil {
				t.Fatal(err)
			}
			defer transport.Close()
			blob, _, err := transport.Get(ctx, "current.yaaresync")
			if err != nil {
				t.Fatal(err)
			}
			document, err := syncapp.OpenDocument(blob, storageTestPassword)
			if err != nil || document.Workspace.Snapshot.Hosts[0].Notes != "second local edit" {
				t.Fatal("latest local edit was not published", err)
			}
		})
	}
}
