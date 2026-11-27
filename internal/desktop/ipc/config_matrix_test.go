package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"reflect"
	"sync/atomic"
	"testing"

	"yaare/internal/adapters/configsync"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app"
	"yaare/internal/app/vault"
	"yaare/internal/domain/preferences"
	"yaare/internal/domain/workspace"
)

func runRealSyncTargets(t *testing.T, prefix string, run func(*testing.T, configsync.Config, string)) {
	t.Helper()
	if os.Getenv("YAARE_SYNC_STORAGE_TEST") != "1" {
		t.Skip("isolated Docker WebDAV/S3 services required")
	}
	for _, provider := range []string{"webdav", "s3"} {
		t.Run(provider, func(t *testing.T) {
			target := configsync.Config{Provider: provider, Username: "yaare-test", Prefix: prefix + "-" + workspace.NewID(), Endpoint: "http://127.0.0.1:4195"}
			credential := "Yaare-Sync-Test-Only"
			if provider == "s3" {
				target.Endpoint, target.Bucket, target.Region = "http://127.0.0.1:4194", "config-test", "us-east-1"
				credential = "Yaare-S3-Test-Only"
			}
			run(t, target, credential)
		})
	}
}

func TestRealSyncBulkOfflineAndNewDevice(t *testing.T) {
	runRealSyncTargets(t, "batch", func(t *testing.T, target configsync.Config, credential string) {
		const password = "Batch-Sync-Fixture-Only!"
		device := func(key byte, pass string) (*SyncService, *sqlite.Store, context.Context) {
			cloud, store, ctx := cloudFixture(t, key)
			service := NewSyncService(cloud)
			if _, err := service.Save(ctx, SyncSaveRequest{Target: target, Credential: credential, Password: pass}); err != nil {
				t.Fatal(err)
			}
			return service, store, ctx
		}
		a, sa, ca := device(31, password)
		b, sb, cb := device(32, password)
		const count = 256
		original := make([]workspace.Host, count)
		expected := map[string]string{}
		for i := range original {
			host := syncHost(fmt.Sprintf("test-%03d", i))
			original[i] = host
			expected[host.ID] = ""
		}
		editSync(t, sa, ca, func(p *workspace.Snapshot) { p.Hosts = original })
		syncNow(t, a, ca)
		syncNow(t, b, cb)
		editSync(t, sa, ca, func(p *workspace.Snapshot) {
			for i := 0; i < count; i += 4 {
				p.Hosts[i].Notes = "offline-A"
				expected[p.Hosts[i].ID] = "offline-A"
			}
		})
		editSync(t, sb, cb, func(p *workspace.Snapshot) {
			var kept []workspace.Host
			for i, h := range p.Hosts {
				if i%4 == 1 {
					delete(expected, h.ID)
					continue
				}
				kept = append(kept, h)
			}
			for i := 0; i < 16; i++ {
				h := syncHost(fmt.Sprintf("new-%02d", i))
				kept = append(kept, h)
				expected[h.ID] = ""
			}
			p.Hosts = kept
		})
		syncNow(t, b, cb)
		syncNow(t, a, ca)
		syncNow(t, b, cb)
		c, sc, cc := device(33, "Wrong-Batch-Fixture-Only!")
		before := sc.Load(cc).Snapshot
		if _, err := c.Preview(cc); err == nil {
			t.Fatal("wrong configuration password accepted")
		}
		if !reflect.DeepEqual(before, sc.Load(cc).Snapshot) {
			t.Fatal("failed decrypt modified the new device")
		}
		settings, err := c.Settings(cc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.Save(cc, SyncSaveRequest{Version: settings.Version, Target: target, Password: password}); err != nil {
			t.Fatal(err)
		}
		syncNow(t, c, cc)
		left := sa.Load(ca).Snapshot.Hosts
		if len(left) != len(expected) || !reflect.DeepEqual(left, sb.Load(cb).Snapshot.Hosts) || !reflect.DeepEqual(left, sc.Load(cc).Snapshot.Hosts) {
			t.Fatal("batch values or ordering differed between devices")
		}
		for _, h := range left {
			note, ok := expected[h.ID]
			if !ok || h.Notes != note {
				t.Fatal("unexpected host or lost offline edit")
			}
			delete(expected, h.ID)
		}
		if len(expected) != 0 {
			t.Fatal("missing host after batch merge")
		}
	})
}

func TestRealSyncPortableDataAndDevicePreferences(t *testing.T) {
	runRealSyncTargets(t, "portable", func(t *testing.T, target configsync.Config, credential string) {
		x, sa, ca := cloudFixture(t, 61)
		y, sb, cb := cloudFixture(t, 62)
		a, b := NewSyncService(x), NewSyncService(y)
		host, _, privateKey := seedBackup(t, NewBackupService(x), ca, "portable test host")
		standalone := syncHost("password and proxy")
		standalone.Proxy = workspace.ProxyConfig{Kind: "socks5", Address: "proxy.example", Port: 1080, Username: "fixture", UseAuthentication: true}
		const hostPass = "host-pass-test-only"
		const proxyPass = "proxy-pass-test-only"
		ws := NewWorkspaceService(app.NewWorkspace(sa), x.protector)
		if _, err := ws.SaveConnection(ca, SaveConnectionRequest{Host: standalone, Revision: sa.Load(ca).Snapshot.Revision, RememberPassword: true, Password: hostPass, ProxyPassword: proxyPass}); err != nil {
			t.Fatal(err)
		}
		icon := fixtureCustomIcon(t)
		cmd := workspace.Command{ID: workspace.NewID(), Name: "Test command", Command: "pwd", Group: "工具", Icon: "debian"}
		editSync(t, sa, ca, func(p *workspace.Snapshot) {
			p.Groups = append(p.Groups, "日本")
			p.Hosts[0].Group = "日本"
			p.Hosts[0].Icon = icon
			p.CommandGroups = append(p.CommandGroups, "工具")
			p.Commands = []workspace.Command{cmd}
		})
		portable := preferences.Portable{Language: "ja", Mode: "dark", FontSize: 17}
		if _, err := sa.SavePreferences(ca, preferences.SaveRequest{Version: preferences.Version, Portable: &portable}); err != nil {
			t.Fatal(err)
		}
		local := preferences.Empty().Device
		local.TransferConcurrency = 7
		if _, err := sb.SavePreferences(cb, preferences.SaveRequest{Version: preferences.Version, Device: &local}); err != nil {
			t.Fatal(err)
		}
		for _, d := range []struct {
			s   *SyncService
			ctx context.Context
		}{{a, ca}, {b, cb}} {
			if _, err := d.s.Save(d.ctx, SyncSaveRequest{Target: target, Credential: credential, Password: "Portable-Sync-Fixture-Only!"}); err != nil {
				t.Fatal(err)
			}
		}
		syncNow(t, a, ca)
		syncNow(t, b, cb)
		p := sb.Load(cb).Snapshot
		if len(p.Hosts) != 2 || p.Hosts[0].ID != host.ID || p.Hosts[0].Group != "日本" || p.Hosts[0].Icon != icon || len(p.Commands) != 1 || p.Commands[0] != cmd {
			t.Fatal("portable records changed")
		}
		prefs, err := sb.Preferences(cb)
		if err != nil || prefs.Portable != portable || !reflect.DeepEqual(prefs.Device, local) {
			t.Fatal("device-local or portable preferences mismatch", err)
		}
		payload, _, err := b.capture(cb, mustSaved(t, b, cb))
		var decoded vault.KeySecret
		if err != nil || len(payload.Keys) != 1 || len(payload.Identities) != 1 || json.Unmarshal(payload.Keys[0].Material, &decoded) != nil || string(decoded.PrivateKey) != privateKey {
			t.Fatal("target could not recover the original key or identity", err)
		}
		source, _, err := a.capture(ca, mustSaved(t, a, ca))
		if err != nil || len(payload.HostCredentials) != 1 || len(payload.ProxyCredentials) != 1 ||
			!reflect.DeepEqual(payload.HostCredentials, source.HostCredentials) || !reflect.DeepEqual(payload.ProxyCredentials, source.ProxyCredentials) {
			t.Fatal("host-bound or proxy credentials differ after migration", err)
		}
	})
}

func TestRealSyncUnavailableStorageRetry(t *testing.T) {
	runRealSyncTargets(t, "outage", func(t *testing.T, target configsync.Config, credential string) {
		upstream, err := url.Parse(target.Endpoint)
		if err != nil {
			t.Fatal(err)
		}
		var down atomic.Bool
		forward := httputil.NewSingleHostReverseProxy(upstream)
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if down.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			forward.ServeHTTP(w, r)
		}))
		defer proxy.Close()
		target.Endpoint = proxy.URL
		cloud, store, ctx := cloudFixture(t, 51)
		svc := NewSyncService(cloud)
		if _, err := svc.Save(ctx, SyncSaveRequest{Target: target, Credential: credential, Password: "Outage-Test-Fixture-Only!"}); err != nil {
			t.Fatal(err)
		}
		editSync(t, store, ctx, func(p *workspace.Snapshot) { p.Hosts = []workspace.Host{syncHost("before outage")} })
		syncNow(t, svc, ctx)
		editSync(t, store, ctx, func(p *workspace.Snapshot) { p.Hosts[0].Notes = "edited offline" })
		before, err := store.ReadSync(ctx)
		if err != nil {
			t.Fatal(err)
		}
		local := store.Load(ctx).Snapshot
		down.Store(true)
		if err := svc.runBackgroundTick(ctx); err == nil {
			t.Fatal("outage reported success")
		}
		after, err := store.ReadSync(ctx)
		if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(local, store.Load(ctx).Snapshot) {
			t.Fatal("failed tick advanced baseline or lost local edit", err)
		}
		down.Store(false)
		if err := svc.runBackgroundTick(ctx); err != nil {
			t.Fatal(err)
		}
		other, receiver, ctxB := cloudFixture(t, 52)
		b := NewSyncService(other)
		if _, err := b.Save(ctxB, SyncSaveRequest{Target: target, Credential: credential, Password: "Outage-Test-Fixture-Only!"}); err != nil {
			t.Fatal(err)
		}
		syncNow(t, b, ctxB)
		if !reflect.DeepEqual(local.Hosts, receiver.Load(ctxB).Snapshot.Hosts) {
			t.Fatal("retry lost offline data")
		}
	})
}

func TestRealSyncCapabilityProbeDistinguishesOutageFromBackupOnly(t *testing.T) {
	runRealSyncTargets(t, "capability", func(t *testing.T, target configsync.Config, credential string) {
		upstream, err := url.Parse(target.Endpoint)
		if err != nil {
			t.Fatal(err)
		}
		forward := httputil.NewSingleHostReverseProxy(upstream)
		var mode atomic.Int32
		var hits atomic.Int32
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conditional := r.Method == "LOCK" || (target.Provider == "s3" && r.Method == "PUT" && r.Header.Get("If-Match") != "")
			if conditional && mode.Load() != 0 {
				hits.Add(1)
				if status := mode.Load(); status >= 400 {
					w.WriteHeader(int(status))
					return
				}
				if r.Method == "LOCK" {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set("ETag", `"test-etag"`)
				w.WriteHeader(http.StatusOK)
				return
			}
			forward.ServeHTTP(w, r)
		}))
		defer proxy.Close()
		cloud, store, ctx := cloudFixture(t, 71)
		svc := NewSyncService(cloud)
		saved, err := svc.Save(ctx, SyncSaveRequest{Target: target, Credential: credential, Password: "Capability-Test-Fixture!"})
		if err != nil || !saved.CanSync {
			t.Fatal("healthy storage rejected", err)
		}
		proxied := target
		proxied.Endpoint = proxy.URL
		proxied.Prefix += "-candidate"
		request := SyncSaveRequest{Version: saved.Version, Target: proxied, Credential: credential}
		for _, status := range []int32{http.StatusServiceUnavailable, http.StatusForbidden, http.StatusLocked} {
			mode.Store(status)
			hits.Store(0)
			if _, err = svc.Save(ctx, request); err == nil || hits.Load() == 0 {
				t.Fatalf("probe HTTP %d must reject settings save", status)
			}
			after, err := svc.Settings(ctx)
			if err != nil || !reflect.DeepEqual(after, saved) {
				t.Fatal("probe failure replaced working settings", err)
			}
		}
		mode.Store(2)
		limited, err := svc.Save(ctx, request)
		if err != nil || limited.CanSync {
			t.Fatal("unsafe storage enabled bidirectional sync", err)
		}
		editSync(t, store, ctx, func(p *workspace.Snapshot) { p.Hosts = []workspace.Host{syncHost("backup only")} })
		if err := svc.runBackgroundTick(ctx); err != nil {
			t.Fatal(err)
		}
		history, err := svc.History(ctx)
		if err != nil || len(history) != 1 {
			t.Fatal("backup-only mode did not publish history", err)
		}
		mode.Store(0)
		current, err := svc.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		request.Version = current.Version
		fixed, err := svc.Save(ctx, request)
		if err != nil || !fixed.CanSync {
			t.Fatal("healthy storage did not recover sync capability", err)
		}
	})
}
