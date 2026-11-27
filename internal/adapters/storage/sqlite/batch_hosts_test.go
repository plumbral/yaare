package sqlite

import (
	"context"
	"reflect"
	"testing"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

func TestBatchHostsAtomicRevisionAndFailure(t *testing.T) {
	ctx := context.Background()
	store := openTest(t, t.TempDir())
	service := app.NewWorkspace(store)
	a := hostForTest()
	a.ID = "batch-a"
	b := hostForTest()
	b.ID = "batch-b"
	b.Port = 2222
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: a})
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: b})
	before := store.Load(ctx).Snapshot
	updated := append([]workspace.Host{}, before.Hosts...)
	for i := range updated {
		updated[i].TimeoutSeconds = 45
		updated[i].Notes = "same"
		updated[i].Icon = "rockylinux"
	}
	result, err := service.Apply(ctx, workspace.Change{Kind: "saveHosts", Revision: before.Revision, Hosts: updated})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.Revision != before.Revision+1 || result.Snapshot.Hosts[1].Port != 2222 {
		t.Fatal("batch did not commit exactly once or preserve unrelated field")
	}
	stable := store.Load(ctx).Snapshot
	invalid := append([]workspace.Host{}, stable.Hosts...)
	invalid[0].Notes = "must rollback"
	invalid[1].IdentityID = "identity-does-not-exist"
	if _, err = service.Apply(ctx, workspace.Change{Kind: "saveHosts", Revision: stable.Revision, Hosts: invalid}); err == nil {
		t.Fatal("invalid identity accepted")
	}
	if got := store.Load(ctx).Snapshot; !reflect.DeepEqual(got, stable) {
		t.Fatal("partial batch persisted")
	}
	if _, err = service.Apply(ctx, workspace.Change{Kind: "saveHosts", Revision: before.Revision, Hosts: updated}); err == nil {
		t.Fatal("stale batch accepted")
	}
	if _, err = service.Apply(ctx, workspace.Change{Kind: "saveHosts", Revision: stable.Revision, Hosts: []workspace.Host{updated[0], updated[0]}}); err == nil {
		t.Fatal("duplicate target accepted")
	}
	invalid[1] = updated[1]
	invalid[1].JumpHostIDs = []string{invalid[1].ID}
	if _, err = service.Apply(ctx, workspace.Change{Kind: "saveHosts", Revision: stable.Revision, Hosts: invalid}); err == nil {
		t.Fatal("self jump accepted")
	}
	if got := store.Load(ctx).Snapshot; !reflect.DeepEqual(got, stable) {
		t.Fatal("rejected batch changed store")
	}
}

func TestBatchDeleteRespectsJumpReferences(t *testing.T) {
	ctx := context.Background()
	store := openTest(t, t.TempDir())
	service := app.NewWorkspace(store)
	a := hostForTest()
	a.ID = "jump"
	b := hostForTest()
	b.ID = "target"
	b.JumpHostIDs = []string{a.ID}
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: a})
	applyTest(t, store, workspace.Change{Kind: "saveHost", Host: b})
	before := store.Load(ctx).Snapshot
	if _, err := service.Apply(ctx, workspace.Change{Kind: "deleteHosts", Revision: before.Revision, IDs: []string{a.ID}}); err == nil {
		t.Fatal("in-use jump deleted")
	}
	if got := store.Load(ctx).Snapshot; !reflect.DeepEqual(got, before) {
		t.Fatal("failed delete mutated store")
	}
	result, err := service.Apply(ctx, workspace.Change{Kind: "deleteHosts", Revision: before.Revision, IDs: []string{a.ID, b.ID}})
	if err != nil || len(result.Snapshot.Hosts) != 0 || result.Snapshot.Revision != before.Revision+1 {
		t.Fatal("whole selected chain should delete atomically", err)
	}
}
