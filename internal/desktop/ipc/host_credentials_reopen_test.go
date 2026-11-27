//go:build windows || linux

package ipc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app"
)

func TestConnectionPasswordSurvivesNewProcessWithOSProtection(t *testing.T) {
	requireCurrentUserCredentialTests(t)
	server := newPasswordSSHServer(t, "OS-Protected-Reopen-Test-Only!", 0)
	store, s, ctx, dir := passwordFixture(t, secrets.CurrentUser{})
	h := passwordHost()
	h.Port = server.port
	h.Username = "fixture"
	h = saveHostPassword(t, s, ctx, h, "OS-Protected-Reopen-Test-Only!", true)
	if !h.HasCredential {
		t.Fatal("password not saved")
	}
	store.Close()
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	command := exec.Command(executable, "-test.run=^TestConnectionPasswordReopenHelper$", "-test.v")
	command.Env = append(os.Environ(), "YAARE_PASSWORD_REOPEN_DIR="+dir, "YAARE_PASSWORD_REOPEN_FINGERPRINT="+server.fingerprint)
	if output, e := command.CombinedOutput(); e != nil {
		t.Fatalf("new process could not resolve saved password: %v\n%s", e, output)
	}
	if server.logins.Load() != 2 {
		t.Fatal("new process did not complete fresh SSH authentication twice")
	}
}

func TestConnectionPasswordReopenHelper(t *testing.T) {
	dir := os.Getenv("YAARE_PASSWORD_REOPEN_DIR")
	if dir == "" {
		t.Skip("child-process helper")
	}
	ctx := testOwner(context.Background())
	store := sqlite.New(func() (string, error) { return dir, nil })
	defer store.Close()
	if e := store.Initialize(ctx); e != nil {
		t.Fatal(e)
	}
	s := NewWorkspaceService(app.NewWorkspace(store))
	r := store.Load(ctx)
	if !r.Ready || len(r.Snapshot.Hosts) != 1 || !r.Snapshot.Hosts[0].HasCredential {
		t.Fatal("saved host unavailable after process restart")
	}
	identities, e := store.Identities(ctx)
	if e != nil || len(identities) != 0 {
		t.Fatal("standalone host turned into identity", e)
	}
	assertResolvedPassword(t, passwordSSH(t, s), ctx, r.Snapshot.Hosts[0], "OS-Protected-Reopen-Test-Only!")
	ssh, closeSSH := NewSSHService(NewKeyService(), func() (string, error) { return filepath.Join(dir, "trust"), nil })
	defer closeSSH()
	ConfigureSSHWorkspace(ssh, s.application)
	trusted := map[string]bool{os.Getenv("YAARE_PASSWORD_REOPEN_FINGERPRINT"): true}
	connectPasswordFixture(t, ssh, ctx, r.Snapshot.Hosts[0], 1, trusted)
	connectPasswordFixture(t, ssh, ctx, r.Snapshot.Hosts[0], 2, trusted)
}
