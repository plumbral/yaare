package ipc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yaare/internal/adapters/configsync"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

func TestMergedKeyLimitStopsPreviewAndBackgroundPublication(t *testing.T) {
	var puts atomic.Int32
	target := fixtureS3(t, func(w http.ResponseWriter, r *http.Request, storage http.Handler) {
		if r.Method == http.MethodPut && (strings.Contains(r.URL.Path, "/backups/") || strings.HasSuffix(r.URL.Path, "/current.yaaresync")) {
			puts.Add(1)
		}
		storage.ServeHTTP(w, r)
	})
	first, storeA, ctxA := cloudFixture(t, 190)
	second, storeB, ctxB := cloudFixture(t, 191)
	a, b := NewSyncService(first), NewSyncService(second)
	saveSyncFixture(t, ctxA, a, target)
	saveSyncFixture(t, ctxB, b, target)
	root := t.TempDir()
	for i, store := range []*sqlite.Store{storeA, storeB} {
		data, err := store.CloudRead(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for j := range 128 + i {
			id := fmt.Sprintf("key-%d-%03d", i, j)
			data.Keys = append(data.Keys, workspace.SavedKey{ID: id, Name: id, Format: "PEM", Path: filepath.Join(root, id)})
		}
		if err := store.CloudReplace(t.Context(), data.Snapshot.Revision, data); err != nil {
			t.Fatal(err)
		}
	}
	syncNow(t, a, ctxA)
	if puts.Swap(0) == 0 {
		t.Fatal("first device did not publish")
	}
	if _, err := b.Preview(ctxB); err == nil {
		t.Fatal("accepted preview of a 257-key union")
	}
	if err := b.runBackgroundTick(ctxB); err == nil {
		t.Fatal("accepted background publication of a 257-key union")
	}
	if puts.Load() != 0 {
		t.Fatal("rejected merge wrote remote data")
	}
	for i, store := range []*sqlite.Store{storeA, storeB} {
		data, err := store.CloudRead(t.Context())
		if err != nil || len(data.Keys) != 128+i {
			t.Fatalf("device %d has %d keys after rejected merge: %v", i, len(data.Keys), err)
		}
	}
}

func TestLostSyncBaselineRequiresReviewedMerge(t *testing.T) {
	for _, damage := range []string{"missing", "corrupt"} {
		t.Run(damage, func(t *testing.T) {
			var puts atomic.Int32
			target := fixtureS3(t, func(w http.ResponseWriter, r *http.Request, storage http.Handler) {
				if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/current.yaaresync") {
					puts.Add(1)
				}
				storage.ServeHTTP(w, r)
			})
			cloud, store, ctx := cloudFixture(t, 192)
			s := NewSyncService(cloud)
			saveSyncFixture(t, ctx, s, target)
			syncNow(t, s, ctx)
			before, err := s.Settings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := store.SyncReceiptPath()
			if err != nil {
				t.Fatal(err)
			}
			paths, err := filepath.Glob(filepath.Join(filepath.Dir(receipt), "sync-baselines", "*.bin"))
			if err != nil || len(paths) != 1 {
				t.Fatal(paths, err)
			}
			switch damage {
			case "missing":
				err = os.Remove(paths[0])
			case "corrupt":
				raw, err := os.ReadFile(paths[0])
				if err != nil {
					t.Fatal(err)
				}
				raw[len(raw)-1] ^= 1
				if err = os.WriteFile(paths[0], raw, 0600); err != nil {
					t.Fatal(err)
				}
				later := time.Now().Add(time.Second)
				if err = os.Chtimes(paths[0], later, later); err != nil {
					t.Fatal(err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			puts.Store(0)
			if err := s.runBackgroundTick(ctx); err != nil {
				t.Fatal(err)
			}
			paused, err := s.Settings(ctx)
			if err != nil || !paused.Paused || paused.Issue != "baseline" || paused.Version != before.Version+1 {
				t.Fatal(paused, err)
			}
			if _, err := s.Save(ctx, SyncSaveRequest{Version: paused.Version, Target: target}); err != nil {
				t.Fatal(err)
			}
			paused, err = s.Settings(ctx)
			if err != nil || !paused.Paused || paused.Issue != "baseline" {
				t.Fatal("settings save resumed sync", paused, err)
			}
			if err := s.Resume(ctx, paused.Version); err == nil {
				t.Fatal("resumed with no reviewed ancestor")
			}
			history, err := s.History(ctx)
			if err != nil || len(history) == 0 {
				t.Fatal(history, err)
			}
			if _, err := s.PreviewRestore(ctx, history[0].Name); err != nil {
				t.Fatal(err)
			}
			if err := s.BackupNow(ctx); err != nil {
				t.Fatal(err)
			}
			if puts.Load() != 0 {
				t.Fatal("baseline loss overwrote current")
			}
			editSync(t, store, ctx, func(snapshot *workspace.Snapshot) { snapshot.Hosts = append(snapshot.Hosts, syncHost("after loss")) })
			syncNow(t, s, ctx)
			rebuilt, err := s.Settings(ctx)
			if err != nil || rebuilt.Issue != "" || !rebuilt.Paused || puts.Load() != 1 {
				t.Fatal("review did not establish a paused, valid ancestor", rebuilt, err)
			}
			if err := s.Resume(ctx, rebuilt.Version); err != nil {
				t.Fatal(err)
			}
			if record, err := store.ReadSync(ctx); err != nil || record.Paused || len(record.State) == 0 {
				t.Fatal("failed to resume rebuilt baseline", err)
			}
		})
	}
}

type conditionalReads struct {
	total, conditional, heads atomic.Int32
	missing                   atomic.Bool
}

func (reads *conditionalReads) observe(w http.ResponseWriter, r *http.Request, storage http.Handler) {
	if strings.HasSuffix(r.URL.Path, "/current.yaaresync") {
		if r.Method == http.MethodHead {
			reads.heads.Add(1)
			if reads.missing.Load() {
				http.NotFound(w, r)
				return
			}
		} else if r.Method == http.MethodGet {
			reads.total.Add(1)
			if r.Header.Get("If-None-Match") != "" {
				reads.conditional.Add(1)
			}
			if reads.missing.Load() {
				http.NotFound(w, r)
				return
			}
		}
	}
	storage.ServeHTTP(w, r)
}

func (reads *conditionalReads) expect(t *testing.T, total, conditional int32) {
	t.Helper()
	if gotTotal, gotConditional := reads.total.Swap(0), reads.conditional.Swap(0); gotTotal != total || gotConditional != conditional {
		t.Fatalf("current GETs: %d total and %d conditional; want %d and %d", gotTotal, gotConditional, total, conditional)
	}
}

func (reads *conditionalReads) expectHeads(t *testing.T, heads int32) {
	t.Helper()
	if got := reads.heads.Swap(0); got != heads {
		t.Fatalf("current HEADs: %d; want %d", got, heads)
	}
}

func prepareSyncTick(t *testing.T, ctx context.Context, service *SyncService) (syncPlan, error) {
	t.Helper()
	record, saved, err := service.load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return service.prepare(ctx, record, saved, "", true)
}

func applySyncTick(t *testing.T, ctx context.Context, service *SyncService, plan syncPlan) (CloudResult, error) {
	t.Helper()
	target, err := service.target(plan.saved)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	return service.applyPlan(ctx, plan, target, nil)
}

func saveSyncFixture(t *testing.T, ctx context.Context, service *SyncService, target configsync.Config) {
	t.Helper()
	t.Cleanup(service.transport.CloseIdleConnections)
	if _, err := service.Save(ctx, SyncSaveRequest{Target: target, Credential: storageTestSecret, Password: storageTestPassword}); err != nil {
		t.Fatal(err)
	}
}

func TestConditionalSyncIdleDirtyAndStaleCAS(t *testing.T) {
	var reads conditionalReads
	target := fixtureS3(t, reads.observe)
	cloud, store, ctx := cloudFixture(t, 171)
	service := NewSyncService(cloud)
	saveSyncFixture(t, ctx, service, target)
	editSync(t, store, ctx, func(snapshot *workspace.Snapshot) { snapshot.Hosts = []workspace.Host{syncHost("conditional")} })
	syncNow(t, service, ctx)
	reads.expect(t, 1, 0)

	idle, err := prepareSyncTick(t, ctx, service)
	if err != nil || !idle.remoteUnchanged {
		t.Fatal("expected verified baseline on 304", err)
	}
	if result, err := applySyncTick(t, ctx, service, idle); err != nil || result.State != "equal" || result.Pulled {
		t.Fatalf("unexpected idle result: %+v, %v", result, err)
	}
	reads.expect(t, 1, 1)
	if version, err := store.SyncVersion(ctx); err != nil || version != idle.record.Version {
		t.Fatal("idle check wrote a new baseline", err)
	}

	editSync(t, store, ctx, func(snapshot *workspace.Snapshot) { snapshot.Hosts[0].Notes = "published from a 304" })
	dirty, err := prepareSyncTick(t, ctx, service)
	if err != nil || !dirty.remoteUnchanged {
		t.Fatal("local edits should not invalidate the remote validator", err)
	}
	if _, err := applySyncTick(t, ctx, service, dirty); err != nil {
		t.Fatal("304 blocked a dirty local merge", err)
	}
	reads.expect(t, 1, 1)
	otherCloud, otherStore, otherCtx := cloudFixture(t, 172)
	other := NewSyncService(otherCloud)
	saveSyncFixture(t, otherCtx, other, target)
	syncNow(t, other, otherCtx)
	if got := otherStore.Load(otherCtx).Snapshot.Hosts[0].Notes; got != "published from a 304" {
		t.Fatalf("local update did not reach remote: %q", got)
	}

	editSync(t, store, ctx, func(snapshot *workspace.Snapshot) { snapshot.Hosts[0].Notes = "different local edit" })
	stale, err := prepareSyncTick(t, ctx, service)
	if err != nil || !stale.remoteUnchanged {
		t.Fatal("expected a 304 before the competing write", err)
	}
	editSync(t, otherStore, otherCtx, func(snapshot *workspace.Snapshot) { snapshot.Hosts[0].Notes = "competing edit" })
	syncNow(t, other, otherCtx)
	if _, err := applySyncTick(t, ctx, service, stale); !errors.Is(err, configsync.ErrChanged) {
		t.Fatal("stale 304 authorized a concurrent overwrite", err)
	}
	reads.total.Store(0)
	reads.conditional.Store(0)
	latest, err := prepareSyncTick(t, ctx, service)
	if err != nil || latest.remoteUnchanged || latest.remote.Snapshot.Hosts[0].Notes != "competing edit" {
		t.Fatal("pending receipt must re-read the competing version", err)
	}
	reads.expect(t, 1, 0)
	if result, err := applySyncTick(t, ctx, service, latest); err != nil || result.State != "conflict" {
		t.Fatal("competing edit did not surface as conflict", result, err)
	}
}

func TestConditionalSyncMissingScopeAndCorruptBaseline(t *testing.T) {
	var reads conditionalReads
	target := fixtureS3(t, reads.observe)
	cloud, store, ctx := cloudFixture(t, 173)
	service := NewSyncService(cloud)
	saveSyncFixture(t, ctx, service, target)
	syncNow(t, service, ctx)
	reads.expect(t, 1, 0)
	reads.missing.Store(true)
	if _, err := prepareSyncTick(t, ctx, service); !errors.Is(err, configsync.ErrMissing) {
		t.Fatal("a vanished established remote must not be reseeded", err)
	}
	reads.expect(t, 1, 1)
	reads.missing.Store(false)
	settings, err := service.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Save(ctx, SyncSaveRequest{Version: settings.Version, Target: target, Credential: "rotated-storage-fixture"}); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareSyncTick(t, ctx, service)
	if err != nil || plan.remoteUnchanged || len(plan.record.State) == 0 {
		t.Fatal("changing credentials reused the tag or discarded the baseline", err)
	}
	reads.expect(t, 1, 0)
	reads.heads.Store(0)
	if _, err := applySyncTick(t, ctx, service, plan); err != nil {
		t.Fatal(err)
	}
	reads.expect(t, 0, 0)
	reads.expectHeads(t, 1)
	corrupt := plan.record
	corrupt.State[len(corrupt.State)-1] ^= 1
	if err := store.SaveSync(ctx, corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareSyncTick(t, ctx, service); err == nil {
		t.Fatal("conditional read hid baseline authentication failure")
	}
	paused, err := service.Settings(ctx)
	if err != nil || !paused.Paused || paused.Issue != "baseline" {
		t.Fatal("an unauthenticated baseline did not persist the recovery state", paused, err)
	}
	reads.expect(t, 0, 0)
}

func TestConditionalSyncRestoreStartsWithFullRead(t *testing.T) {
	var reads conditionalReads
	target := fixtureS3(t, reads.observe)
	cloud, store, ctx := cloudFixture(t, 174)
	service := NewSyncService(cloud)
	saveSyncFixture(t, ctx, service, target)
	editSync(t, store, ctx, func(snapshot *workspace.Snapshot) { snapshot.Hosts = []workspace.Host{syncHost("restore baseline")} })
	syncNow(t, service, ctx)
	history, err := service.History(ctx)
	if err != nil || len(history) != 1 {
		t.Fatal("missing seed history", err)
	}
	editSync(t, store, ctx, func(snapshot *workspace.Snapshot) { snapshot.Hosts[0].Notes = "newer than restore" })
	syncNow(t, service, ctx)
	reads.expect(t, 2, 0)
	preview, err := service.PreviewRestore(ctx, history[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := service.Apply(ctx, preview.ID, nil); err != nil || result.State != "restored" {
		t.Fatal("restore did not apply", result, err)
	}
	reads.expect(t, 0, 0)
	settings, err := service.Settings(ctx)
	if err != nil || !settings.Paused {
		t.Fatal("restore did not pause sync", err)
	}
	if err := service.Resume(ctx, settings.Version); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareSyncTick(t, ctx, service)
	if err != nil || plan.remoteUnchanged || plan.local.Snapshot.Hosts[0].Notes != "" || plan.remote.Snapshot.Hosts[0].Notes != "newer than restore" {
		t.Fatal("restore did not remain a new local edit against the remote baseline", err)
	}
	reads.expect(t, 1, 0)
	if result, err := applySyncTick(t, ctx, service, plan); err != nil || result.State != "equal" {
		t.Fatal("resuming sync did not publish the restored content", result, err)
	}
	reads.expect(t, 0, 0)
}

func TestSyncSeedRaceDoesNotReplaceCurrent(t *testing.T) {
	target := fixtureS3(t, nil)
	cloud, store, ctx := cloudFixture(t, 175)
	otherCloud, otherStore, otherCtx := cloudFixture(t, 176)
	service, other := NewSyncService(cloud), NewSyncService(otherCloud)
	saveSyncFixture(t, ctx, service, target)
	saveSyncFixture(t, otherCtx, other, target)
	editSync(t, store, ctx, func(snapshot *workspace.Snapshot) { snapshot.Hosts = []workspace.Host{syncHost("winner")} })
	editSync(t, otherStore, otherCtx, func(snapshot *workspace.Snapshot) { snapshot.Hosts = []workspace.Host{syncHost("second device")} })
	stale, err := prepareSyncTick(t, otherCtx, other)
	if err != nil || stale.remoteUnchanged || stale.etag != "" {
		t.Fatal("expected the second device to see an empty target", err)
	}
	syncNow(t, service, ctx)
	if _, err := applySyncTick(t, otherCtx, other, stale); !errors.Is(err, configsync.ErrChanged) {
		t.Fatal("exclusive seed overwrote the first device's publication", err)
	}
	current, err := prepareSyncTick(t, otherCtx, other)
	if err != nil || current.remoteUnchanged || len(current.remote.Snapshot.Hosts) != 1 || current.remote.Snapshot.Hosts[0].Name != "winner" {
		t.Fatal("the winner's configuration was not preserved", err)
	}
}

func TestSyncPreviewReleasesSettingsSecretsBeforeApply(t *testing.T) {
	target := fixtureS3(t, nil)
	cloud, _, ctx := cloudFixture(t, 185)
	service := NewSyncService(cloud)
	saveSyncFixture(t, ctx, service, target)
	preview, err := service.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	plan, found := service.plans[preview.ID]
	service.mu.Unlock()
	if !found || plan.saved.Password != nil || plan.saved.Credential != nil {
		t.Fatal("pending preview retained settings secrets")
	}
	if _, err := service.Apply(ctx, preview.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.BackupNow(ctx); err != nil {
		t.Fatal(err)
	}
}
