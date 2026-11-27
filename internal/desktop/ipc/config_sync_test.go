package ipc

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"

	"yaare/internal/adapters/configsync"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

func syncHost(name string) workspace.Host {
	return workspace.Host{ID: workspace.NewID(), Name: name, Address: "127.0.0.1", Port: 22, Username: "test", Authentication: "password", Icon: "linux", System: "Linux", Group: workspace.Ungrouped}
}
func editSync(t *testing.T, store *sqlite.Store, ctx context.Context, edit func(*workspace.Snapshot)) {
	t.Helper()
	p := store.Load(ctx).Snapshot
	edit(&p)
	if _, e := store.Commit(ctx, p.Revision, p); e != nil {
		t.Fatal(e)
	}
}
func syncNow(t *testing.T, s *SyncService, ctx context.Context) SyncPreview {
	t.Helper()
	p, e := s.Preview(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Conflicts) > 0 {
		t.Fatal("unexpected merge conflict", p.Conflicts)
	}
	if _, e = s.Apply(ctx, p.ID, nil); e != nil {
		t.Fatal(e)
	}
	return p
}

func TestEntryMergeDeletionPreferenceAndIdentityConflict(t *testing.T) {
	a, b := syncHost("A"), syncHost("B")
	base := syncmodel.Payload{Version: 1, Snapshot: workspace.Empty(), Preferences: json.RawMessage(`{"mode":"dark","fontSize":12}`)}
	base.Snapshot.Hosts = []workspace.Host{a, b}
	copyPayload := func(p syncmodel.Payload) syncmodel.Payload {
		raw, _ := json.Marshal(p)
		var copy syncmodel.Payload
		_ = json.Unmarshal(raw, &copy)
		return copy
	}
	local, remote := copyPayload(base), copyPayload(base)
	local.Snapshot.Hosts[0].Notes = "local"
	remote.Snapshot.Hosts = remote.Snapshot.Hosts[:1]
	remote.Preferences = json.RawMessage(`{"mode":"light","fontSize":12}`)
	merged, conflicts, e := syncmodel.Merge(base, local, remote, nil)
	if e != nil || len(conflicts) > 0 || len(merged.Snapshot.Hosts) != 1 || merged.Snapshot.Hosts[0].Notes != "local" || !strings.Contains(string(merged.Preferences), "light") {
		t.Fatal(merged, conflicts, e)
	}
	remote = copyPayload(base)
	remote.Snapshot.Hosts[0].Notes = "remote"
	_, conflicts, e = syncmodel.Merge(base, local, remote, nil)
	if e != nil || len(conflicts) != 1 {
		t.Fatal(conflicts, e)
	}
	merged, conflicts, e = syncmodel.Merge(base, local, remote, map[string]string{conflicts[0].ID: "remote"})
	remoteChosen := false
	for _, host := range merged.Snapshot.Hosts {
		if host.ID == a.ID {
			remoteChosen = host.Notes == "remote"
		}
	}
	if e != nil || len(conflicts) != 0 || !remoteChosen {
		t.Fatal(merged, conflicts, e)
	}
	remote = copyPayload(base)
	remote.Snapshot.Hosts = remote.Snapshot.Hosts[1:]
	_, conflicts, e = syncmodel.Merge(base, local, remote, nil)
	if e != nil || len(conflicts) != 1 || !conflicts[0].RemoteDeleted {
		t.Fatal(conflicts, e)
	}
	duplicated := copyPayload(base)
	duplicated.Snapshot.Hosts = append(duplicated.Snapshot.Hosts, a)
	if _, e = syncmodel.Canonicalize(duplicated); e == nil {
		t.Fatal("duplicate ID collapsed before validation")
	}
}

func TestMergeCrossEntityReferentialIntegrityConflicts(t *testing.T) {
	copyPayload := func(p syncmodel.Payload) syncmodel.Payload {
		raw, _ := json.Marshal(p)
		var out syncmodel.Payload
		_ = json.Unmarshal(raw, &out)
		return out
	}

	base := syncmodel.Payload{Version: syncmodel.Version, Snapshot: workspace.Empty(), Preferences: json.RawMessage(`{}`)}
	base.Snapshot.Groups = []string{workspace.Ungrouped, "Prod"}
	local, remote := copyPayload(base), copyPayload(base)
	local.Snapshot.Groups = []string{workspace.Ungrouped}
	added := syncHost("prod-web")
	added.Group = "Prod"
	remote.Snapshot.Hosts = []workspace.Host{added}
	_, conflicts, err := syncmodel.Merge(base, local, remote, nil)
	if err != nil || len(conflicts) != 1 || conflicts[0].ID != "group/Prod" || !conflicts[0].LocalDeleted {
		t.Fatalf("expected group/Prod conflict, got %+v err=%v", conflicts, err)
	}
	keepGroup, conflicts, err := syncmodel.Merge(base, local, remote, map[string]string{"group/Prod": "remote"})
	if err != nil || len(conflicts) != 0 || workspace.Validate(keepGroup.Snapshot, nil) != nil {
		t.Fatalf("keeping group failed validation: %+v %v", conflicts, err)
	}
	dropGroup, conflicts, err := syncmodel.Merge(base, local, remote, map[string]string{"group/Prod": "local"})
	if err != nil || len(conflicts) != 0 || dropGroup.Snapshot.Hosts[0].Group != workspace.Ungrouped || workspace.Validate(dropGroup.Snapshot, nil) != nil {
		t.Fatalf("dropping group did not safely reassign host to ungrouped: %+v %v", dropGroup.Snapshot, err)
	}

	keyID := workspace.NewID()
	idID := workspace.NewID()
	base2 := syncmodel.Payload{
		Version:     syncmodel.Version,
		Snapshot:    workspace.Empty(),
		Preferences: json.RawMessage(`{}`),
		Keys:        []syncmodel.Key{{ID: keyID, Name: "deploy-key", Format: "OpenSSH", Material: json.RawMessage(`{"PrivateKey":"k"}`)}},
		Identities:  []syncmodel.Identity{{Metadata: workspace.Identity{ID: idID, Name: "deploy-id", Username: "ops", Authentication: "key", KeyID: keyID}, Material: json.RawMessage(`{}`)}},
	}
	local2, remote2 := copyPayload(base2), copyPayload(base2)
	local2.Keys = nil
	local2.Identities = nil
	h := syncHost("uses-id")
	h.IdentityID = idID
	h.Username = "ops"
	remote2.Snapshot.Hosts = []workspace.Host{h}
	_, conflicts, err = syncmodel.Merge(base2, local2, remote2, nil)
	if err != nil || len(conflicts) != 2 {
		t.Fatalf("expected both identity and transitive key conflicts, got %+v err=%v", conflicts, err)
	}
	restored, conflicts, err := syncmodel.Merge(base2, local2, remote2, map[string]string{
		"identity/" + idID: "remote",
		"key/" + keyID:     "remote",
	})
	if err != nil || len(conflicts) != 0 || len(restored.Identities) != 1 || len(restored.Keys) != 1 {
		t.Fatalf("restoring identity+key failed: %+v %v", conflicts, err)
	}
	detached, conflicts, err := syncmodel.Merge(base2, local2, remote2, map[string]string{
		"identity/" + idID: "local",
		"key/" + keyID:     "local",
	})
	if err != nil || len(conflicts) != 0 || detached.Snapshot.Hosts[0].IdentityID != "" {
		t.Fatalf("detaching deleted identity failed: %+v %v", detached.Snapshot.Hosts, err)
	}

	hA, hB := syncHost("JumpA"), syncHost("JumpB")
	base3 := syncmodel.Payload{Version: syncmodel.Version, Snapshot: workspace.Empty(), Preferences: json.RawMessage(`{}`)}
	base3.Snapshot.Hosts = []workspace.Host{hA, hB}
	local3, remote3 := copyPayload(base3), copyPayload(base3)
	local3.Snapshot.Hosts[0].JumpHostIDs = []string{hB.ID}
	remote3.Snapshot.Hosts[1].JumpHostIDs = []string{hA.ID}
	_, conflicts, err = syncmodel.Merge(base3, local3, remote3, nil)
	if err != nil || len(conflicts) != 2 {
		t.Fatalf("expected jump cycle to synthesize conflicts for both hosts, got %+v err=%v", conflicts, err)
	}
	resolvedCycle, conflicts, err := syncmodel.Merge(base3, local3, remote3, map[string]string{
		"host/" + hA.ID: "local",
		"host/" + hB.ID: "local",
	})
	if err != nil || len(conflicts) != 0 || workspace.ValidateNetworks(resolvedCycle.Snapshot.Hosts) != nil {
		t.Fatalf("breaking jump cycle failed: %+v %v", conflicts, err)
	}
}
func TestRealBackgroundSyncWithoutWindowAndIdleNoWrites(t *testing.T) {
	runRealSyncTargets(t, "runner", func(t *testing.T, target configsync.Config, credential string) {
		aCloud, storeA, ctxA := cloudFixture(t, 81)
		bCloud, storeB, ctxB := cloudFixture(t, 82)
		a, b := NewSyncService(aCloud), NewSyncService(bCloud)
		for _, device := range []struct {
			s   *SyncService
			ctx context.Context
		}{{a, ctxA}, {b, ctxB}} {
			if _, e := device.s.Save(device.ctx, SyncSaveRequest{Target: target, Credential: credential, Password: "Config-Encryption-Test-Only!"}); e != nil {
				t.Fatal(e)
			}
		}
		editSync(t, storeA, ctxA, func(p *workspace.Snapshot) { p.Hosts = []workspace.Host{syncHost("background runner fixture")} })
		syncNow(t, a, ctxA)
		stop := StartConfigurationSync(b)
		defer stop()
		deadline := time.Now().Add(8 * time.Second)
		for len(storeB.Load(context.Background()).Snapshot.Hosts) != 1 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if len(storeB.Load(context.Background()).Snapshot.Hosts) != 1 {
			t.Fatal("background sync depended on frontend")
		}
		stop()
		stop()
		before, c, e := b.load(ctxB)
		if e != nil {
			t.Fatal(e)
		}
		p, e := b.prepare(ctxB, before, c, "", true)
		if e != nil {
			t.Fatal(e)
		}
		transport, e := b.target(c)
		if e != nil {
			t.Fatal(e)
		}
		defer transport.Close()
		if _, e = b.applyPlan(ctxB, p, transport, nil); e != nil {
			t.Fatal(e)
		}
		after, _, _ := b.load(ctxB)
		if after.Version != before.Version {
			t.Fatal("equal sync rewrote persistent state")
		}
	})
}
func TestRealConfigurationTwoDevicesConflictRestoreAndEncryptedBaseline(t *testing.T) {
	providers := []string{"webdav", "s3"}
	if os.Getenv("YAARE_SELFHOSTED_TEST") == "1" {
		providers = []string{"yaare"}
	} else if os.Getenv("YAARE_SYNC_STORAGE_TEST") != "1" {
		t.Skip("isolated Docker storage required")
	}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			ca, sa, ctxA := cloudFixture(t, 41)
			cb, sb, ctxB := cloudFixture(t, 42)
			a, b := NewSyncService(ca), NewSyncService(cb)
			target := configsync.Config{Provider: provider, Endpoint: "http://127.0.0.1:4195", Username: "yaare-test", Prefix: "devices-" + workspace.NewID(), Bucket: "config-test", Region: "us-east-1"}
			credential := "Yaare-Sync-Test-Only"
			if provider == "yaare" {
				target = configsync.Config{Provider: "yaare", Endpoint: "http://127.0.0.1:14180"}
				credential = os.Getenv("YAARE_SYNC_TEST_TOKEN")
				storage, err := configsync.NewHTTP(target, []byte(credential))
				if err != nil {
					t.Fatal(err)
				}
				_, err = storage.Head(ctxA, "current.yaaresync")
				storage.Close()
				if err != configsync.ErrMissing {
					t.Fatal("test requires a fresh isolated sync store", err)
				}
			}
			if provider == "s3" {
				target.Endpoint = "http://127.0.0.1:4194"
				credential = "Yaare-S3-Test-Only"
			}
			for _, device := range []struct {
				s   *SyncService
				ctx context.Context
			}{{a, ctxA}, {b, ctxB}} {
				_, e := device.s.Save(device.ctx, SyncSaveRequest{Target: target, Credential: credential, Password: "Config-Encryption-Test-Only!"})
				if e != nil {
					t.Fatal(e)
				}
			}
			first, second := syncHost("First configuration server fixture"), syncHost("Second configuration server fixture")
			editSync(t, sa, ctxA, func(p *workspace.Snapshot) { p.Hosts = []workspace.Host{first, second} })
			syncNow(t, a, ctxA)
			syncNow(t, b, ctxB)
			if len(sb.Load(ctxB).Snapshot.Hosts) != 2 {
				t.Fatal("second device did not receive hosts")
			}
			editSync(t, sa, ctxA, func(p *workspace.Snapshot) {
				for i := range p.Hosts {
					if p.Hosts[i].ID == first.ID {
						p.Hosts[i].Notes = "A's independent edit"
					}
				}
			})
			editSync(t, sb, ctxB, func(p *workspace.Snapshot) {
				for i := range p.Hosts {
					if p.Hosts[i].ID == second.ID {
						p.Hosts[i].Notes = "B's independent edit"
					}
				}
			})
			syncNow(t, a, ctxA)
			syncNow(t, b, ctxB)
			syncNow(t, a, ctxA)
			equal := func() {
				t.Helper()
				left, _, _ := a.capture(ctxA, mustSaved(t, a, ctxA))
				right, _, _ := b.capture(ctxB, mustSaved(t, b, ctxB))
				if !reflect.DeepEqual(left, right) {
					t.Fatal("devices differ after merge")
				}
			}
			equal()
			editSync(t, sa, ctxA, func(p *workspace.Snapshot) { p.Hosts = p.Hosts[:1] })
			editSync(t, sb, ctxB, func(p *workspace.Snapshot) {
				for i := range p.Hosts {
					if p.Hosts[i].ID == second.ID {
						p.Hosts[i].Notes = "edit on the other device before deletion"
					}
				}
			})
			syncNow(t, a, ctxA)
			deleted, err := b.Preview(ctxB)
			if err != nil || len(deleted.Conflicts) != 1 || !deleted.Conflicts[0].RemoteDeleted {
				t.Fatal("delete versus edit was not presented as a conflict", deleted.Conflicts, err)
			}
			if _, err = b.Apply(ctxB, deleted.ID, map[string]string{deleted.Conflicts[0].ID: "remote"}); err != nil {
				t.Fatal(err)
			}
			if len(sb.Load(ctxB).Snapshot.Hosts) != 1 {
				t.Fatal("deleted host resurrected")
			}
			editSync(t, sa, ctxA, func(p *workspace.Snapshot) { p.Hosts[0].Notes = "conflict A" })
			editSync(t, sb, ctxB, func(p *workspace.Snapshot) { p.Hosts[0].Notes = "conflict B" })
			syncNow(t, a, ctxA)
			plan, e := b.Preview(ctxB)
			if e != nil || len(plan.Conflicts) != 1 {
				t.Fatal(plan, e)
			}
			if _, e = b.Apply(testOwner(context.Background()), plan.ID, nil); e == nil {
				t.Fatal("foreign owner consumed preview")
			}
			if result, e := b.Apply(ctxB, plan.ID, map[string]string{plan.Conflicts[0].ID: "remote"}); e != nil || !result.Pulled {
				t.Fatal(result, e)
			}
			if _, e = b.Apply(ctxB, plan.ID, nil); e == nil {
				t.Fatal("preview replayed")
			}
			syncNow(t, a, ctxA)
			equal()
			if e = a.BackupNow(ctxA); e != nil {
				t.Fatal(e)
			}
			history, e := a.History(ctxA)
			if e != nil || len(history) < 2 {
				t.Fatal(history, e)
			}
			var restore SyncPreview
			for index := len(history) - 1; index >= 0; index-- {
				restore, e = a.PreviewRestore(ctxA, history[index].Name)
				if e != nil {
					t.Fatal(e)
				}
				if restore.Remote.Hosts == 2 {
					break
				}
			}
			if restore.Remote.Hosts != 2 {
				t.Fatal("pre-deletion configuration was not recoverable")
			}
			before, _ := a.Settings(ctxA)
			if result, e := a.Apply(ctxA, restore.ID, nil); e != nil || !result.Pulled {
				t.Fatal(result, e)
			}
			after, _ := a.Settings(ctxA)
			if !after.Paused || after.Version <= before.Version {
				t.Fatal("restore did not pause", after)
			}
			if len(sa.Load(ctxA).Snapshot.Hosts) != 2 || len(sb.Load(ctxB).Snapshot.Hosts) != 1 {
				t.Fatal("restore did not recover the deleted host only on the restoring device")
			}
			record, e := sa.ReadSync(ctxA)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(record.Settings), "Encryption-Test") || strings.Contains(string(record.State), "conflict A") {
				t.Fatal("plaintext persisted in settings or baseline")
			}
			saved := mustSaved(t, a, ctxA)
			transport, e := a.target(saved)
			if e != nil {
				t.Fatal(e)
			}
			defer transport.Close()
			blob, _, e := transport.Get(ctxA, "current.yaaresync")
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(blob), first.Name) || strings.Contains(string(blob), "conflict A") {
				t.Fatal("storage received plaintext")
			}
			if _, e = syncapp.OpenDocument(blob, "Wrong-Encryption-Password!"); e == nil {
				t.Fatal("wrong encryption password accepted")
			}
			if e := a.runBackgroundTick(ctxA); e != nil {
				t.Fatal(e)
			}
			pausedBlob, _, e := transport.Get(ctxA, "current.yaaresync")
			if e != nil || !reflect.DeepEqual(pausedBlob, blob) {
				t.Fatal("paused restore changed the remote configuration", e)
			}
			if e := a.Resume(ctxA, before.Version); e == nil {
				t.Fatal("stale revision resumed the scheduler")
			}
			if e := a.Resume(ctxA, after.Version); e != nil {
				t.Fatal(e)
			}
			if e := a.runBackgroundTick(ctxA); e != nil {
				t.Fatal(e)
			}
			syncNow(t, b, ctxB)
			equal()
			if len(sb.Load(ctxB).Snapshot.Hosts) != 2 {
				t.Fatal("explicit resume did not propagate the restored data")
			}
		})
	}
}
func mustSaved(t *testing.T, s *SyncService, ctx context.Context) syncSaved {
	t.Helper()
	_, c, e := s.load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
