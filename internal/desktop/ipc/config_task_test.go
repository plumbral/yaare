package ipc

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	syncapp "yaare/internal/app/sync"

	"yaare/internal/adapters/configsync"
	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

type testSyncEvents chan SyncStatusEvent

func (events testSyncEvents) Send(event SyncStatusEvent) error {
	events <- event
	return nil
}

func startStatusWatch(t *testing.T, ctx context.Context, service *SyncService) testSyncEvents {
	t.Helper()
	watchCtx, cancel := context.WithCancel(ctx)
	events, done := make(testSyncEvents, 32), make(chan error, 1)
	go func() { done <- service.watchStatus(watchCtx, events) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("status watch did not stop on cancellation")
		}
	})
	return events
}

func receiveStatus(t *testing.T, events testSyncEvents) SyncStatusEvent {
	t.Helper()
	select {
	case status := <-events:
		return status
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for sync status")
		return SyncStatusEvent{}
	}
}

func TestConfigurationSyncWatchEmitsOnlyChanges(t *testing.T) {
	cloud, store, ctx := cloudFixture(t, 166)
	service := NewSyncService(cloud)
	if err := service.watchStatus(context.Background(), make(testSyncEvents, 1)); err == nil {
		t.Fatal("watch accepted an unowned caller")
	}
	first, second := startStatusWatch(t, ctx, service), startStatusWatch(t, testOwner(ctx), service)
	for _, events := range []testSyncEvents{first, second} {
		if event := receiveStatus(t, events); event != (SyncStatusEvent{}) {
			t.Fatalf("expected initial unconfigured status: %+v", event)
		}
	}
	record, err := service.record(sqlite.SyncRecord{}, syncSaved{Target: configsync.Config{Provider: "webdav"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSync(ctx, record); err != nil {
		t.Fatal(err)
	}
	for _, events := range []testSyncEvents{first, second} {
		if event := receiveStatus(t, events); !event.Task.Configured || event.Settings.Version != 1 || event.Applied != 0 {
			t.Fatalf("unexpected settings update: %+v", event)
		}
	}
	service.setTask("sync", "prepare", "")
	for _, events := range []testSyncEvents{first, second} {
		if event := receiveStatus(t, events); event.Task.Stage != "prepare" {
			t.Fatalf("unexpected task stage: %q", event.Task.Stage)
		}
	}
	service.setTask("sync", "prepare", "")
	service.taskStage("prepare")
	store.Retry(ctx)
	editSync(t, store, ctx, func(snapshot *workspace.Snapshot) { snapshot.Hosts = append(snapshot.Hosts, syncHost("local")) })
	select {
	case event := <-first:
		t.Fatalf("unchanged sync status should not publish: %+v", event)
	case event := <-second:
		t.Fatalf("unchanged sync status should not publish: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}

	data, err := store.CloudRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	record.Version++
	if err := store.RestoreSync(ctx, data.Snapshot.Revision, data, record); err != nil {
		t.Fatal(err)
	}
	for _, events := range []testSyncEvents{first, second} {
		if event := receiveStatus(t, events); !event.Task.Paused || !event.Settings.Paused || event.Applied != 1 {
			t.Fatalf("restore did not publish a committed configuration change: %+v", event)
		}
	}
	if err := service.Resume(ctx, 2); err != nil {
		t.Fatal(err)
	}
	for _, events := range []testSyncEvents{first, second} {
		if event := receiveStatus(t, events); event.Task.Paused || event.Settings.Version != 3 || event.Applied != 1 {
			t.Fatalf("resume did not publish the new settings: %+v", event)
		}
	}
	data, err = store.CloudRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreCloud(ctx, data.Snapshot.Revision, data, sqlite.IntegrationPause{Version: 2}); err == nil {
		t.Fatal("stale import should not commit")
	}
	if err := store.RestoreCloud(ctx, data.Snapshot.Revision, data, sqlite.IntegrationPause{Version: 3}); err != nil {
		t.Fatal(err)
	}
	for _, events := range []testSyncEvents{first, second} {
		if event := receiveStatus(t, events); !event.Task.Paused || event.Settings.Version != 4 || event.Applied != 2 {
			t.Fatalf("portable import should notify only after a successful commit: %+v", event)
		}
	}
}

func TestUnsupportedRemoteBackupTargetIsInactive(t *testing.T) {
	cloud, store, ctx := cloudFixture(t, 165)
	service := NewSyncService(cloud)
	unsupported := configsync.Config{Provider: "sftp"}
	if _, err := service.Save(ctx, SyncSaveRequest{Target: unsupported}); err == nil {
		t.Fatal("removed provider can still be saved")
	}
	record, err := service.record(sqlite.SyncRecord{}, syncSaved{Target: unsupported}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSync(ctx, record); err != nil {
		t.Fatal(err)
	}
	view, err := service.Settings(ctx)
	if err != nil || view.Target.Provider != "" || view.Version != 1 {
		t.Fatalf("removed target should be inactive and replaceable: %+v, %v", view, err)
	}
	status, err := service.TaskStatus(ctx)
	if err != nil || status.Configured {
		t.Fatalf("removed target should have no background task: %+v, %v", status, err)
	}
}

func TestSavedSyncTargetRunsAndReportsPublicationWithoutDuplicateBackup(t *testing.T) {
	uploading, release := make(chan struct{}, 1), make(chan struct{})
	target := fixtureS3(t, func(w http.ResponseWriter, r *http.Request, storage http.Handler) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/backups/") {
			select {
			case uploading <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		storage.ServeHTTP(w, r)
	})
	cloud, _, ctx := cloudFixture(t, 164)
	service := NewSyncService(cloud)
	if _, err := service.Save(ctx, SyncSaveRequest{Target: target, Credential: storageTestSecret, Password: storageTestPassword}); err != nil {
		t.Fatal(err)
	}
	events := startStatusWatch(t, ctx, service)
	if event := receiveStatus(t, events); !event.Task.Configured || event.Task.Stage != "" {
		t.Fatalf("expected idle configured status: %+v", event)
	}
	stop := StartConfigurationSync(service)
	defer stop()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	select {
	case <-uploading:
	case <-time.After(5 * time.Second):
		t.Fatal("saving a valid target did not start the background sync")
	}
	for {
		status := receiveStatus(t, events).Task
		if status.Kind == "sync" && status.Stage == "upload" && status.LastBackup == 0 {
			break
		}
	}
	close(release)
	released = true
	for {
		status := receiveStatus(t, events).Task
		if status.Stage == "" && status.LastBackup > 0 && status.LastSync > 0 {
			break
		}
	}
	history, err := service.History(ctx)
	if err != nil || len(history) != 1 {
		t.Fatalf("sync should produce one history object: %v, %v", history, err)
	}
}

type countingProtector struct {
	base               secrets.Protector
	protect, unprotect atomic.Int32
}

func (c *countingProtector) Protect(data []byte, id string) ([]byte, error) {
	c.protect.Add(1)
	return c.base.Protect(data, id)
}

func (c *countingProtector) Unprotect(data []byte, id string) ([]byte, error) {
	c.unprotect.Add(1)
	return c.base.Unprotect(data, id)
}

func TestIdleBackgroundTickShortCircuitsAndReusesPrefetchedRemote(t *testing.T) {
	var reads conditionalReads
	target := fixtureS3(t, reads.observe)
	cloud, store, ctx := cloudFixture(t, 167)
	counter := &countingProtector{base: cloud.protector}
	cloud.protector = counter
	service := NewSyncService(cloud)
	saveSyncFixture(t, ctx, service, target)

	identity := workspace.Identity{ID: workspace.NewID(), Name: "idle-id", Username: "ops", Authentication: "password"}
	sealed, err := counter.base.Protect([]byte(`{"Password":"Secret-Idle-Test!"}`), identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveIdentity(ctx, workspace.SealedIdentity{Identity: identity, Cipher: sealed}, false); err != nil {
		t.Fatal(err)
	}
	syncNow(t, service, ctx)
	reads.expect(t, 1, 0)

	counter.protect.Store(0)
	counter.unprotect.Store(0)
	if err = service.runBackgroundTick(ctx); err != nil {
		t.Fatal(err)
	}
	reads.expect(t, 1, 1)
	if gotU, gotP := counter.unprotect.Load(), counter.protect.Load(); gotU != 1 || gotP != 0 {
		t.Fatalf("idle tick decrypted credentials or materialized: unprotect=%d protect=%d", gotU, gotP)
	}

	otherCloud, otherStore, otherCtx := cloudFixture(t, 168)
	other := NewSyncService(otherCloud)
	saveSyncFixture(t, otherCtx, other, target)
	syncNow(t, other, otherCtx)
	editSync(t, otherStore, otherCtx, func(s *workspace.Snapshot) { s.Hosts = []workspace.Host{syncHost("from-other")} })
	syncNow(t, other, otherCtx)
	reads.total.Store(0)
	reads.conditional.Store(0)

	if err = service.runBackgroundTick(ctx); err != nil {
		t.Fatal(err)
	}
	reads.expect(t, 1, 1)
	if hosts := store.Load(ctx).Snapshot.Hosts; len(hosts) != 1 || hosts[0].Name != "from-other" {
		t.Fatalf("prefetched remote edit not applied: %+v", hosts)
	}
}

func TestLocalEditDebouncedWakeAndInteractivePreemptsBackground(t *testing.T) {
	var stallNext atomic.Bool
	stalled, releaseStall := make(chan struct{}, 1), make(chan struct{})
	target := fixtureS3(t, func(w http.ResponseWriter, r *http.Request, storage http.Handler) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/current.yaaresync") && stallNext.CompareAndSwap(true, false) {
			select {
			case stalled <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
				return
			case <-releaseStall:
			}
		}
		storage.ServeHTTP(w, r)
	})
	defer close(releaseStall)

	cloud, store, ctx := cloudFixture(t, 169)
	service := NewSyncService(cloud)
	service.debounceDelay = 30 * time.Millisecond
	saveSyncFixture(t, ctx, service, target)
	syncNow(t, service, ctx)

	stop := StartConfigurationSync(service)
	defer stop()

	editSync(t, store, ctx, func(s *workspace.Snapshot) { s.Hosts = []workspace.Host{syncHost("debounced-host")} })
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		transport, err := service.target(mustSaved(t, service, ctx))
		if err == nil {
			blob, _, getErr := transport.Get(ctx, "current.yaaresync")
			transport.Close()
			if getErr == nil {
				if doc, openErr := syncapp.OpenDocument(blob, storageTestPassword); openErr == nil && len(doc.Workspace.Snapshot.Hosts) == 1 && doc.Workspace.Snapshot.Hosts[0].Name == "debounced-host" {
					break
				}
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	transport, err := service.target(mustSaved(t, service, ctx))
	if err != nil {
		t.Fatal(err)
	}
	blob, _, err := transport.Get(ctx, "current.yaaresync")
	transport.Close()
	if err != nil {
		t.Fatal(err)
	}
	if doc, err := syncapp.OpenDocument(blob, storageTestPassword); err != nil || len(doc.Workspace.Snapshot.Hosts) != 1 || doc.Workspace.Snapshot.Hosts[0].Name != "debounced-host" {
		t.Fatalf("debounced local edit did not publish promptly: %v", err)
	}

	stallNext.Store(true)
	service.runner.Wake()
	select {
	case <-stalled:
	case <-time.After(3 * time.Second):
		t.Fatal("background poll did not enter stalled GET")
	}
	start := time.Now()
	if _, err := service.Preview(ctx); err != nil {
		t.Fatalf("interactive Preview failed after preempting background poll: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("interactive Preview waited too long for background poll: %v", elapsed)
	}
	status, err := service.TaskStatus(ctx)
	if err != nil || status.Issue != "" {
		t.Fatalf("preempted background poll left error issue: %+v %v", status, err)
	}
}
