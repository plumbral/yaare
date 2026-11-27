package sqlite

import (
	"os"
	"path/filepath"
	"testing"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

func TestServerSystemCachePersistenceAndRouteInvalidation(t *testing.T) {
	root := t.TempDir()
	s := openTest(t, root)
	jump := hostForTest()
	jump.ID, jump.Name = "jump", "jump"
	applyTest(t, s, workspace.Change{Kind: "saveHost", Host: jump})
	host := hostForTest()
	host.JumpHostIDs = []string{jump.ID}
	applyTest(t, s, workspace.Change{Kind: "saveHost", Host: host})
	rev := s.Load(testContext).Snapshot.Revision
	route := workspace.ServerSystemRoute(host, func(id string) (workspace.Host, bool) { return jump, id == jump.ID })
	debian := workspace.ServerSystem{Family: "linux", ID: "debian", Version: "12", Label: "Debian GNU/Linux 12"}
	ubuntu := workspace.ServerSystem{Family: "linux", ID: "ubuntu", Version: "24.04", Label: "Ubuntu 24.04"}
	for _, entry := range []struct {
		at   int64
		info workspace.ServerSystem
	}{{10, debian}, {20, ubuntu}, {30, workspace.ServerSystem{Family: "linux", Label: "Linux"}}, {25, debian}} {
		if err := s.SaveServerSystem(testContext, host.ID, route, entry.at, entry.info); err != nil {
			t.Fatal(err)
		}
	}
	if s.Load(testContext).Snapshot.Revision != rev {
		t.Fatal("observed metadata changed syncable workspace revision")
	}
	read := func(store *Store) map[string]workspace.ServerSystem {
		v, e := store.ServerSystems(testContext)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	if read(s)[host.ID] != ubuntu {
		t.Fatal("newer specific result was replaced")
	}
	file := filepath.Join(root, "server-systems.json")
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveServerSystem(testContext, host.ID, route, 40, ubuntu); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(file)
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("unchanged observation rewrote cache", err)
	}
	s.Close()
	s = openTest(t, root)
	if read(s)[host.ID] != ubuntu {
		t.Fatal("observation lost after restart")
	}
	jump.Address = "127.0.0.2"
	applyTest(t, s, workspace.Change{Kind: "saveHost", Host: jump})
	if len(read(s)) != 0 {
		t.Fatal("route change did not invalidate old observation")
	}
	if err = s.SaveServerSystem(testContext, host.ID, route, 50, debian); err != nil || len(read(s)) != 0 {
		t.Fatal("late result accepted for old destination", err)
	}
}

func TestSetHostSystemIconPersistsSystemAndProtectsCloudWithoutNoopBump(t *testing.T) {
	root := t.TempDir()
	s := openTest(t, root)
	svc := app.NewWorkspace(s)

	sysHost := hostForTest()
	sysHost.ID, sysHost.Name, sysHost.Icon = "sys-host", "System Host", "linux"
	cloudHost := hostForTest()
	cloudHost.ID, cloudHost.Name, cloudHost.Address, cloudHost.Icon = "cloud-host", "Cloud Host", "127.0.0.9", "cloud-aws"

	if _, err := svc.Apply(testContext, workspace.Change{Revision: 0, Kind: "saveHost", Host: sysHost}); err != nil {
		t.Fatal(err)
	}
	rev := svc.Load(testContext).Snapshot.Revision
	if _, err := svc.Apply(testContext, workspace.Change{Revision: rev, Kind: "saveHost", Host: cloudHost}); err != nil {
		t.Fatal(err)
	}

	rev = svc.Load(testContext).Snapshot.Revision
	res, err := svc.Apply(testContext, workspace.Change{Revision: rev, Kind: "setHostSystemIcon", ID: sysHost.ID, Name: "debian"})
	if err != nil || res.Snapshot.Revision != rev+1 || res.Snapshot.Hosts[0].Icon != "debian" {
		t.Fatalf("expected persisted debian icon: rev=%d->%d icon=%q err=%v", rev, res.Snapshot.Revision, res.Snapshot.Hosts[0].Icon, err)
	}

	rev = res.Snapshot.Revision
	noopSame, err := svc.Apply(testContext, workspace.Change{Revision: rev, Kind: "setHostSystemIcon", ID: sysHost.ID, Name: "debian"})
	if err != nil || noopSame.Snapshot.Revision != rev {
		t.Fatalf("same icon bumped revision: %d -> %d (%v)", rev, noopSame.Snapshot.Revision, err)
	}
	noopCloud, err := svc.Apply(testContext, workspace.Change{Revision: rev, Kind: "setHostSystemIcon", ID: cloudHost.ID, Name: "ubuntu"})
	if err != nil || noopCloud.Snapshot.Revision != rev || noopCloud.Snapshot.Hosts[1].Icon != "cloud-aws" {
		t.Fatalf("cloud icon was overwritten or bumped revision: rev=%d->%d icon=%q err=%v", rev, noopCloud.Snapshot.Revision, noopCloud.Snapshot.Hosts[1].Icon, err)
	}

	s.Close()
	s = openTest(t, root)
	reopened := s.Load(testContext).Snapshot
	if reopened.Hosts[0].Icon != "debian" || reopened.Hosts[1].Icon != "cloud-aws" {
		t.Fatalf("unexpected icons after restart: %q, %q", reopened.Hosts[0].Icon, reopened.Hosts[1].Icon)
	}
}
