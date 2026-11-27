package ipc

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
)

func TestConcurrentHostAdditionsDoNotRequireAnOrderChoice(t *testing.T) {
	a, b, c, d := syncHost("existing A"), syncHost("existing B"), syncHost("new local"), syncHost("new remote")
	base := syncmodel.Payload{Version: 1, Snapshot: workspace.Empty(), Preferences: json.RawMessage(`{}`)}
	base.Snapshot.Hosts = []workspace.Host{a, b}
	local, remote := base, base
	local.Snapshot.Hosts = []workspace.Host{b, a, c}
	remote.Snapshot.Hosts = []workspace.Host{a, b, d}
	merged, conflicts, err := syncmodel.Merge(base, local, remote, nil)
	if err != nil || len(conflicts) > 0 || len(merged.Snapshot.Hosts) != 4 || merged.Snapshot.Hosts[0].ID != b.ID || merged.Snapshot.Hosts[3].ID != d.ID {
		t.Fatal("addition conflicted with independent reorder", conflicts, err)
	}
	base.Snapshot.Hosts = nil
	local.Snapshot.Hosts = []workspace.Host{c}
	remote.Snapshot.Hosts = []workspace.Host{d}
	merged, conflicts, err = syncmodel.Merge(base, local, remote, nil)
	if err != nil || len(conflicts) > 0 || len(merged.Snapshot.Hosts) != 2 {
		t.Fatal("initial independent hosts caused an order conflict", conflicts, err)
	}
}

func TestHostGroupOrderSurvivesSynchronization(t *testing.T) {
	base := syncmodel.Payload{Version: 1, Snapshot: workspace.Empty(), Preferences: json.RawMessage(`{}`)}
	base.Snapshot.Groups = []string{workspace.Ungrouped, "Test 1", "Test 2", "Test 10"}
	same, conflicts, err := syncmodel.Merge(base, base, base, nil)
	if err != nil || len(conflicts) != 0 || !reflect.DeepEqual(same.Snapshot.Groups, base.Snapshot.Groups) {
		t.Fatalf("sync changed creation order: %v, %v, %v", same.Snapshot.Groups, conflicts, err)
	}
	local, remote := base, base
	local.Snapshot.Groups = []string{workspace.Ungrouped, "Test 10", "Test 1", "Test 2"}
	remote.Snapshot.Groups = []string{workspace.Ungrouped, "Test 1", "Test 2", "Test 10", "Test 2/New"}
	want := []string{workspace.Ungrouped, "Test 10", "Test 1", "Test 2", "Test 2/New"}
	merged, conflicts, err := syncmodel.Merge(base, local, remote, nil)
	if err != nil || len(conflicts) != 0 || !reflect.DeepEqual(merged.Snapshot.Groups, want) {
		t.Fatalf("sync discarded order or addition: %v, %v, %v", merged.Snapshot.Groups, conflicts, err)
	}
	reloaded, err := syncmodel.Canonicalize(merged)
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot.Groups, want) {
		t.Fatalf("archive changed group order: %v, %v", reloaded.Snapshot.Groups, err)
	}
}

func TestCommandAndCommandGroupOrderSurvivesSynchronization(t *testing.T) {
	c1 := workspace.Command{ID: workspace.NewID(), Name: "First", Command: "echo 1", Group: "Ops", Icon: "linux"}
	c2 := workspace.Command{ID: workspace.NewID(), Name: "Second", Command: "echo 2", Group: "DB", Icon: "linux"}
	c3 := workspace.Command{ID: workspace.NewID(), Name: "Third", Command: "echo 3", Group: "Ops", Icon: "linux"}
	base := syncmodel.Payload{Version: syncmodel.Version, Snapshot: workspace.Empty(), Preferences: json.RawMessage(`{}`)}
	base.Snapshot.CommandGroups = []string{workspace.Ungrouped, "Ops", "DB"}
	base.Snapshot.Commands = []workspace.Command{c1, c2}

	local, remote := base, base
	local.Snapshot.CommandGroups = []string{workspace.Ungrouped, "DB", "Ops"}
	local.Snapshot.Commands = []workspace.Command{c2, c1}
	remote.Snapshot.CommandGroups = []string{workspace.Ungrouped, "Ops", "DB", "Net"}
	remote.Snapshot.Commands = []workspace.Command{c1, c2, c3}

	merged, conflicts, err := syncmodel.Merge(base, local, remote, nil)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("independent command order + addition conflicted: %v %v", conflicts, err)
	}
	wantGroups := []string{workspace.Ungrouped, "DB", "Ops", "Net"}
	if !reflect.DeepEqual(merged.Snapshot.CommandGroups, wantGroups) {
		t.Fatalf("command group order lost: got %v want %v", merged.Snapshot.CommandGroups, wantGroups)
	}
	if len(merged.Snapshot.Commands) != 3 || merged.Snapshot.Commands[0].ID != c2.ID || merged.Snapshot.Commands[1].ID != c1.ID || merged.Snapshot.Commands[2].ID != c3.ID {
		t.Fatalf("command order lost: %+v", merged.Snapshot.Commands)
	}
	reloaded, err := syncmodel.Canonicalize(merged)
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot.Commands, merged.Snapshot.Commands) || !reflect.DeepEqual(reloaded.Snapshot.CommandGroups, wantGroups) {
		t.Fatalf("syncmodel.Canonicalize altered command orders: %v", err)
	}
}

func TestHostOrderMergeRoundTripAndConflict(t *testing.T) {
	a, b, c := syncHost("a"), syncHost("b"), syncHost("c")
	a.Pinned = true
	c.Pinned = true
	base := syncmodel.Payload{Version: 1, Snapshot: workspace.Empty(), Preferences: json.RawMessage(`{}`)}
	base.Snapshot.Hosts = []workspace.Host{a, b, c}
	local, remote := base, base
	local.Snapshot.Hosts = []workspace.Host{c, b, a}
	local.Snapshot.PinOrder = []string{c.ID, a.ID}
	remote.Snapshot.Hosts = []workspace.Host{a, b, c}
	remote.Snapshot.Hosts[1].Notes = "remote edit"
	merged, conflicts, e := syncmodel.Merge(base, local, remote, nil)
	if e != nil || len(conflicts) > 0 || merged.Snapshot.Hosts[0].ID != c.ID || merged.Snapshot.Hosts[1].Notes != "remote edit" || !reflect.DeepEqual(merged.Snapshot.PinOrder, local.Snapshot.PinOrder) {
		t.Fatal(merged, conflicts, e)
	}
	raw, e := json.Marshal(merged)
	if e != nil {
		t.Fatal(e)
	}
	var portable syncmodel.Payload
	if e = json.Unmarshal(raw, &portable); e != nil {
		t.Fatal(e)
	}
	canonical, e := syncmodel.Canonicalize(portable)
	if e != nil || !reflect.DeepEqual(canonical.Snapshot.Hosts, merged.Snapshot.Hosts) || !reflect.DeepEqual(canonical.Snapshot.PinOrder, merged.Snapshot.PinOrder) {
		t.Fatal(canonical, e)
	}
	remote.Snapshot.Hosts = []workspace.Host{b, a, c}
	_, conflicts, e = syncmodel.Merge(base, local, remote, nil)
	if e != nil || len(conflicts) != 1 || conflicts[0].ID != "order/hosts" {
		t.Fatal(conflicts, e)
	}
	selected, conflicts, e := syncmodel.Merge(base, local, remote, map[string]string{"order/hosts": "remote"})
	if e != nil || len(conflicts) > 0 || selected.Snapshot.Hosts[0].ID != b.ID {
		t.Fatal(selected, conflicts, e)
	}
	remote.Snapshot.Hosts = []workspace.Host{a, b}
	merged, _, e = syncmodel.Merge(base, base, remote, nil)
	if e != nil || len(merged.Snapshot.Hosts) != 2 || len(merged.Snapshot.PinOrder) != 1 {
		t.Fatal(merged, e)
	}
}

func TestRealHostOrderConfigurationSync(t *testing.T) {
	runRealSyncTargets(t, "host-order", func(t *testing.T, target configsync.Config, credential string) {
		ca, sa, ctxA := cloudFixture(t, 101)
		cb, sb, ctxB := cloudFixture(t, 102)
		a, b := NewSyncService(ca), NewSyncService(cb)
		for _, device := range []struct {
			s   *SyncService
			ctx context.Context
		}{{a, ctxA}, {b, ctxB}} {
			if _, err := device.s.Save(device.ctx, SyncSaveRequest{Target: target, Credential: credential, Password: "Config-Encryption-Test-Only!"}); err != nil {
				t.Fatal(err)
			}
		}
		h1, h2, h3 := syncHost("one"), syncHost("two"), syncHost("three")
		h1.Pinned, h3.Pinned = true, true
		editSync(t, sa, ctxA, func(p *workspace.Snapshot) { p.Hosts = []workspace.Host{h1, h2, h3} })
		syncNow(t, a, ctxA)
		syncNow(t, b, ctxB)
		editSync(t, sb, ctxB, func(p *workspace.Snapshot) {
			p.Hosts = []workspace.Host{h3, h2, h1}
			p.PinOrder = []string{h3.ID, h1.ID}
		})
		editSync(t, sa, ctxA, func(p *workspace.Snapshot) { p.Hosts[1].Notes = "unrelated edit" })
		syncNow(t, b, ctxB)
		syncNow(t, a, ctxA)
		syncNow(t, b, ctxB)
		for _, s := range []workspace.Snapshot{sa.Load(ctxA).Snapshot, sb.Load(ctxB).Snapshot} {
			if len(s.Hosts) != 3 || s.Hosts[0].ID != h3.ID || s.Hosts[2].ID != h1.ID || s.Hosts[1].Notes != "unrelated edit" || !reflect.DeepEqual(s.PinOrder, []string{h3.ID, h1.ID}) {
				t.Fatal("device sync lost order or unrelated host edit", s)
			}
		}
	})
}

func TestHostOrderEncryptedExportImport(t *testing.T) {
	a, sa, ca, _ := backupFixture(t, 1)
	h1, _, _ := seedBackup(t, a, ca, "first")
	h2, _, _ := seedBackup(t, a, ca, "second")
	h3, _, _ := seedBackup(t, a, ca, "third")
	snapshot := sa.Load(ca).Snapshot
	h1.Pinned, h2.Pinned = true, true
	snapshot.Hosts = []workspace.Host{h3, h1, h2}
	snapshot.PinOrder = []string{h2.ID, h1.ID}
	if _, err := sa.Commit(ca, snapshot.Revision, snapshot); err != nil {
		t.Fatal(err)
	}
	blob, err := a.exportBytes(ca, BackupExportRequest{Password: backupPassword, IncludeCredentials: true})
	if err != nil {
		t.Fatal(err)
	}
	b, sb, cb, _ := backupFixture(t, 2)
	file := chooseBackup(t, b, cb, blob)
	preview, err := b.Preview(cb, file.ID, backupPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Apply(cb, preview.ID); err != nil {
		t.Fatal(err)
	}
	restored := sb.Load(cb).Snapshot
	if !reflect.DeepEqual(restored.Hosts, snapshot.Hosts) || !reflect.DeepEqual(restored.PinOrder, snapshot.PinOrder) {
		t.Fatal("encrypted export/import changed host or pin order", restored)
	}
}
