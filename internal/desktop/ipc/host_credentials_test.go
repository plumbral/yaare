package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"

	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

func passwordFixture(t *testing.T, p secrets.Protector) (*sqlite.Store, *WorkspaceService, context.Context, string) {
	t.Helper()
	dir := t.TempDir()
	ctx := testOwner(context.Background())
	store := sqlite.New(func() (string, error) { return dir, nil })
	if e := store.Initialize(ctx); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	return store, NewWorkspaceService(app.NewWorkspace(store), p), ctx, dir
}
func passwordProtector(t *testing.T, n byte) secrets.Protector {
	t.Helper()
	p, e := newTestVault(bytes.Repeat([]byte{n}, 32))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func passwordHost() workspace.Host {
	return workspace.NormalizeHost(workspace.Host{ID: "password-host", Name: "Password host", Address: "127.0.0.1", Port: 22222, Username: "yaaretest", Authentication: "password", Group: workspace.Ungrouped})
}
func saveHostPassword(t *testing.T, s *WorkspaceService, ctx context.Context, h workspace.Host, password string, remember bool) workspace.Host {
	t.Helper()
	r := s.application.Load(ctx)
	result, e := s.SaveConnection(ctx, SaveConnectionRequest{Revision: r.Snapshot.Revision, Host: h, RememberPassword: remember, Password: password})
	if e != nil {
		t.Fatal(e)
	}
	for _, h := range result.Snapshot.Hosts {
		if h.ID == result.EntityID {
			return h
		}
	}
	t.Fatal("saved host missing")
	return workspace.Host{}
}
func passwordRequest(h workspace.Host) SSHConnectRequest {
	return SSHConnectRequest{HostID: h.ID, Address: h.Address, Port: h.Port, Username: h.Username, Authentication: h.Authentication, KeyID: h.KeyID, IdentityID: h.IdentityID, UseAgent: h.UseAgent, UseSavedCredential: true, TimeoutSeconds: 5}
}
func assertResolvedPassword(t *testing.T, s *SSHService, ctx context.Context, h workspace.Host, want string) {
	t.Helper()
	cfg, release, e := s.resolveSSHConfig(ctx, passwordRequest(h))
	defer release()
	if e != nil || string(cfg.Password) != want {
		t.Fatal("saved password resolution failed", e)
	}
	cfg.ClearSecrets()
}
func passwordSSH(t *testing.T, s *WorkspaceService) *SSHService {
	t.Helper()
	ssh, close := NewSSHService(NewKeyService())
	t.Cleanup(close)
	ssh.credentialProtector = s.protector
	ConfigureSSHWorkspace(ssh, s.application)
	return ssh
}

func TestConnectionPasswordOwnershipReplacementClearAndNoReadLeak(t *testing.T) {
	p := passwordProtector(t, 34)
	store, s, ctx, dir := passwordFixture(t, p)
	ssh := passwordSSH(t, s)
	h := passwordHost()
	old := "Host-Specific-Password-Only-Old!"
	next := "Host-Specific-Password-Only-New!"
	if _, e := s.SaveConnection(context.Background(), SaveConnectionRequest{Host: h, RememberPassword: true, Password: old}); e == nil {
		t.Fatal("unowned password save accepted")
	}
	h = saveHostPassword(t, s, ctx, h, old, true)
	if !h.HasCredential || h.IdentityID != "" {
		t.Fatal("password incorrectly requires identity")
	}
	identities, e := store.Identities(ctx)
	if e != nil || len(identities) != 0 {
		t.Fatal("password created an identity", e)
	}
	sealed, e := store.HostCredential(ctx, h.ID)
	if e != nil || bytes.Contains(sealed.Cipher, []byte(old)) {
		t.Fatal("plaintext saved", e)
	}
	if _, e = p.Unprotect(sealed.Cipher, "another-host"); e == nil {
		t.Fatal("unbound ciphertext")
	}
	raw, _ := json.Marshal(store.Load(ctx))
	if bytes.Contains(raw, []byte(old)) || bytes.Contains(raw, []byte("Cipher")) {
		t.Fatal("read IPC leaked secret")
	}
	assertResolvedPassword(t, ssh, ctx, h, old)
	h.Name = "Renamed without retyping password"
	h = saveHostPassword(t, s, ctx, h, "", true)
	assertResolvedPassword(t, ssh, ctx, h, old)
	before := store.Load(ctx).Snapshot.Revision
	if _, e = s.SaveConnection(ctx, SaveConnectionRequest{Revision: before - 1, Host: h, RememberPassword: true, Password: next}); e == nil {
		t.Fatal("stale password edit committed")
	}
	if store.Load(ctx).Snapshot.Revision != before {
		t.Fatal("stale edit changed configuration")
	}
	assertResolvedPassword(t, ssh, ctx, h, old)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.SaveConnection(cancelled, SaveConnectionRequest{Revision: before, Host: h, RememberPassword: true, Password: next}); e == nil {
		t.Fatal("cancelled save committed")
	}
	assertResolvedPassword(t, ssh, ctx, h, old)
	h = saveHostPassword(t, s, ctx, h, next, true)
	assertResolvedPassword(t, ssh, ctx, h, next)
	r := passwordRequest(h)
	r.UseSavedCredential = false
	r.Password = "One-Shot-Only!"
	cfg, release, e := ssh.resolveSSHConfig(ctx, r)
	release()
	if e != nil || string(cfg.Password) != r.Password {
		t.Fatal("manual override failed", e)
	}
	assertResolvedPassword(t, ssh, ctx, h, next)
	for _, change := range []func(*SSHConnectRequest){func(r *SSHConnectRequest) { r.HostID = "other" }, func(r *SSHConnectRequest) { r.Address = "192.0.2.1" }, func(r *SSHConnectRequest) { r.Username = "other" }, func(r *SSHConnectRequest) { r.Password = "unexpected" }, func(r *SSHConnectRequest) { r.IdentityID = "unexpected" }} {
		r := passwordRequest(h)
		change(&r)
		_, release, e := ssh.resolveSSHConfig(ctx, r)
		release()
		if e == nil {
			t.Fatal("credential used outside its saved scope")
		}
	}
	if e = filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "workspace.lock" {
			return nil
		}
		b, e := os.ReadFile(path)
		if e == nil && (bytes.Contains(b, []byte(old)) || bytes.Contains(b, []byte(next))) {
			return errors.New("plaintext in database or automatic snapshot")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	h = saveHostPassword(t, s, ctx, h, "", false)
	if h.HasCredential {
		t.Fatal("clear did not remove availability")
	}
	if _, e = store.HostCredential(ctx, h.ID); e == nil {
		t.Fatal("cleared password remains")
	}
	_, release, e = ssh.resolveSSHConfig(ctx, passwordRequest(h))
	release()
	if e == nil {
		t.Fatal("cleared password still usable")
	}
}

func TestConnectionPasswordTransactionsIdentitySwitchAndDestinationEdits(t *testing.T) {
	p := passwordProtector(t, 35)
	store, s, ctx, _ := passwordFixture(t, p)
	ssh := passwordSSH(t, s)
	h := saveHostPassword(t, s, ctx, passwordHost(), "Original-Host-Only!", true)
	broken := h
	broken.IdentityID = "missing-identity"
	if _, e := s.SaveConnection(ctx, SaveConnectionRequest{Revision: store.Load(ctx).Snapshot.Revision, Host: broken}); e == nil {
		t.Fatal("invalid identity committed")
	}
	assertResolvedPassword(t, ssh, ctx, h, "Original-Host-Only!")
	report := store.Load(ctx)
	dup, e := s.application.Apply(ctx, workspace.Change{Revision: report.Snapshot.Revision, Kind: "duplicateHost", ID: h.ID})
	if e != nil {
		t.Fatal(e)
	}
	for _, copy := range dup.Snapshot.Hosts {
		if copy.ID != h.ID && copy.HasCredential {
			t.Fatal("duplicate silently copied password")
		}
	}
	identityService := NewIdentityService(store, NewPersistentKeyService(store))
	identityService.protector = p
	i := SaveIdentityRequest{ID: "shared-identity", Name: "Shared", Username: h.Username, Authentication: "password", ReplaceSecret: true, Password: "Shared-Identity-Only!"}
	if e = identityService.Save(ctx, i); e != nil {
		t.Fatal(e)
	}
	h.IdentityID = i.ID
	h = saveHostPassword(t, s, ctx, h, "", false)
	if h.HasCredential {
		t.Fatal("identity switch retained host password")
	}
	if _, e = store.Identity(ctx, i.ID); e != nil {
		t.Fatal("host edit deleted shared identity", e)
	}
	h.IdentityID = ""
	h = saveHostPassword(t, s, ctx, h, "Replacement-Host-Only!", true)
	if _, e = store.Identity(ctx, i.ID); e != nil {
		t.Fatal("host password changed shared identity", e)
	}
	for _, change := range []func(*workspace.Host){func(h *workspace.Host) { h.Username = "other-user" }, func(h *workspace.Host) { h.Address = "192.0.2.10" }, func(h *workspace.Host) { h.Port++ }} {
		h = saveHostPassword(t, s, ctx, h, "Scoped-Password!", true)
		change(&h)
		r, e := s.application.Apply(ctx, workspace.Change{Revision: store.Load(ctx).Snapshot.Revision, Kind: "saveHost", Host: h})
		if e != nil {
			t.Fatal(e)
		}
		for _, host := range r.Snapshot.Hosts {
			if host.ID == h.ID {
				h = host
			}
		}
		if h.HasCredential {
			t.Fatal("metadata edit carried password to a new destination/account")
		}
	}
	h = saveHostPassword(t, s, ctx, h, "Delete-Host-Only!", true)
	if _, e = s.application.Apply(ctx, workspace.Change{Revision: store.Load(ctx).Snapshot.Revision, Kind: "deleteHost", ID: h.ID}); e != nil {
		t.Fatal(e)
	}
	if _, e = store.HostCredential(ctx, h.ID); e == nil {
		t.Fatal("deleted host retained credential")
	}
	if _, e = store.Identity(ctx, i.ID); e != nil {
		t.Fatal("host deletion damaged shared identity", e)
	}
}

func TestHostPasswordsPortableEncryptionScopeAndAtomicMerge(t *testing.T) {
	a, sa, ctxA, _ := passwordFixture(t, passwordProtector(t, 36))
	b, sb, ctxB, _ := passwordFixture(t, passwordProtector(t, 37))
	h := saveHostPassword(t, sa, ctxA, passwordHost(), "Portable-Host-Password-Only!", true)
	pa := syncapp.NewPortableWorkspace(a, sa.protector)
	payload, _, e := pa.Capture(ctxA, true)
	if e != nil {
		t.Fatal(e)
	}
	defer syncmodel.ClearPayload(payload)
	if len(payload.HostCredentials) != 1 {
		t.Fatal("password missing from credential-inclusive capture")
	}
	archive, e := sealBackup(payload, "Export-Encryption-Test-Only!", true)
	if e != nil || bytes.Contains(archive, []byte("Portable-Host-Password")) {
		t.Fatal("archive not encrypted", e)
	}
	data, e := (syncapp.NewPortableWorkspace(b, sb.protector)).Materialize(ctxB, payload)
	if e != nil {
		t.Fatal(e)
	}
	if e = b.CloudReplace(ctxB, b.Load(ctxB).Snapshot.Revision, data); e != nil {
		t.Fatal(e)
	}
	assertResolvedPassword(t, passwordSSH(t, sb), ctxB, h, "Portable-Host-Password-Only!")
	first, _ := a.HostCredential(ctxA, h.ID)
	second, _ := b.HostCredential(ctxB, h.ID)
	if bytes.Equal(first.Cipher, second.Cipher) {
		t.Fatal("portable import reused source ciphertext")
	}
	metadata, _, e := pa.Capture(ctxA, false)
	if e != nil || len(metadata.HostCredentials) != 0 || metadata.Snapshot.Hosts[0].HasCredential {
		t.Fatal("credential exclusion leaked password metadata", e)
	}
	current, _ := b.CloudRead(ctxB)
	metaData, e := (syncapp.NewPortableWorkspace(b, sb.protector)).Materialize(ctxB, metadata)
	if e != nil {
		t.Fatal(e)
	}
	preserveHostCredentials(&metaData, current)
	if len(metaData.HostCredentials) != 1 {
		t.Fatal("metadata-only sync removed matching local password")
	}
	metaData.Snapshot.Hosts[0].Username = "different"
	preserveHostCredentials(&metaData, current)
	if len(metaData.HostCredentials) != 0 {
		t.Fatal("metadata-only restore attached password to different account")
	}
	clone := func(p syncmodel.Payload) syncmodel.Payload {
		raw, _ := json.Marshal(p)
		var v syncmodel.Payload
		json.Unmarshal(raw, &v)
		return v
	}
	local, remote := clone(payload), clone(payload)
	local.HostCredentials[0].Material = json.RawMessage(`{"Password":"Changed-Local-Only!"}`)
	remote.Snapshot.Hosts[0].Username = "different"
	_, conflicts, e := syncmodel.Merge(payload, local, remote, nil)
	if e != nil || len(conflicts) != 1 || conflicts[0].Kind != "host" {
		t.Fatal("host and password were merged independently", e)
	}
	bad := clone(payload)
	bad.HostCredentials = append(bad.HostCredentials, bad.HostCredentials[0])
	if _, e = syncmodel.Canonicalize(bad); e == nil {
		t.Fatal("duplicate host password accepted")
	}
}

func TestProxyPasswordPromptSavesOnlyForMatchingSavedProxy(t *testing.T) {
	protector := passwordProtector(t, 74)
	store, service, ctx, _ := passwordFixture(t, protector)
	host := passwordHost()
	host.Proxy = workspace.ProxyConfig{Kind: "socks5", Address: "proxy.example", Port: 1080, Username: "test", UseAuthentication: true}
	if _, err := service.SaveConnection(ctx, SaveConnectionRequest{Host: host, Revision: store.Load(ctx).Snapshot.Revision}); err != nil {
		t.Fatal(err)
	}
	ssh, stop := NewSSHService(NewPersistentKeyService(store))
	defer stop()
	ConfigureSSHWorkspace(ssh, service.application)
	ssh.credentialProtector = protector
	request := SSHConnectRequest{HostID: host.ID, Proxy: host.Proxy}
	cfg, release, err := ssh.resolveSSHConfig(ctx, request)
	release()
	if err != nil || cfg.SaveProxyPassword == nil {
		t.Fatal("no credential save hook for a saved host", err)
	}
	const password = "test-proxy-password"
	if err = cfg.SaveProxyPassword([]byte(password)); err != nil {
		t.Fatal(err)
	}
	cfg, release, err = ssh.resolveSSHConfig(ctx, request)
	release()
	if err != nil || string(cfg.ProxyPassword) != password {
		t.Fatal("saved proxy password did not resolve", err)
	}
	request.Proxy.Address = "other.example"
	cfg, release, err = ssh.resolveSSHConfig(ctx, request)
	release()
	if err != nil || len(cfg.ProxyPassword) != 0 || cfg.SaveProxyPassword != nil {
		t.Fatal("saved proxy password crossed a proxy boundary", err)
	}
	request.HostID = workspace.NewID()
	request.Proxy = host.Proxy
	cfg, release, err = ssh.resolveSSHConfig(ctx, request)
	release()
	if err != nil || len(cfg.ProxyPassword) != 0 || cfg.SaveProxyPassword != nil {
		t.Fatal("temporary connection used a persistent proxy credential", err)
	}
}

func TestHostPasswordsUnifiedSyncAndBackupRestore(t *testing.T) {
	target := fixtureS3(t, nil)
	ca, sa, ctxA := cloudFixture(t, 141)
	cb, sb, ctxB := cloudFixture(t, 142)
	wa := NewWorkspaceService(app.NewWorkspace(sa), ca.protector)
	wb := NewWorkspaceService(app.NewWorkspace(sb), cb.protector)
	h := saveHostPassword(t, wa, ctxA, passwordHost(), "Synced-Host-Password-First!", true)
	a, b := NewSyncService(ca), NewSyncService(cb)
	for _, d := range []struct {
		s   *SyncService
		ctx context.Context
	}{{a, ctxA}, {b, ctxB}} {
		if _, e := d.s.Save(d.ctx, SyncSaveRequest{Target: target, Credential: storageTestSecret, Password: storageTestPassword}); e != nil {
			t.Fatal(e)
		}
	}
	syncNow(t, a, ctxA)
	syncNow(t, b, ctxB)
	assertResolvedPassword(t, passwordSSH(t, wb), ctxB, h, "Synced-Host-Password-First!")
	h = saveHostPassword(t, wa, ctxA, h, "Synced-Host-Password-Replaced!", true)
	syncNow(t, a, ctxA)
	syncNow(t, b, ctxB)
	assertResolvedPassword(t, passwordSSH(t, wb), ctxB, h, "Synced-Host-Password-Replaced!")
	backupA := NewBackupService(ca)
	backupB, storeC, ctxC, _ := backupFixture(t, 143)
	backupBlob, e := backupA.exportBytes(ctxA, BackupExportRequest{Password: backupPassword, IncludeCredentials: true})
	if e != nil {
		t.Fatal(e)
	}
	file := chooseBackup(t, backupB, ctxC, backupBlob)
	preview, e := backupB.Preview(ctxC, file.ID, backupPassword)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = backupB.Apply(ctxC, preview.ID); e != nil {
		t.Fatal(e)
	}
	wc := NewWorkspaceService(app.NewWorkspace(storeC), backupB.cloud.protector)
	assertResolvedPassword(t, passwordSSH(t, wc), ctxC, h, "Synced-Host-Password-Replaced!")
	metadata, e := backupA.exportBytes(ctxA, BackupExportRequest{Password: backupPassword, IncludeCredentials: false})
	if e != nil {
		t.Fatal(e)
	}
	file = chooseBackup(t, backupB, ctxC, metadata)
	preview, e = backupB.Preview(ctxC, file.ID, backupPassword)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = backupB.Apply(ctxC, preview.ID); e != nil {
		t.Fatal(e)
	}
	assertResolvedPassword(t, passwordSSH(t, wc), ctxC, h, "Synced-Host-Password-Replaced!")
	h = saveHostPassword(t, wa, ctxA, h, "", false)
	syncNow(t, a, ctxA)
	syncNow(t, b, ctxB)
	if sb.Load(ctxB).Snapshot.Hosts[0].HasCredential {
		t.Fatal("password deletion did not synchronize")
	}
}
