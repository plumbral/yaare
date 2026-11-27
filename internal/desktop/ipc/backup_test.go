package ipc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
	"yaare/internal/app/vault"
	"yaare/internal/domain/syncmodel"

	"golang.org/x/crypto/ssh"
	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

const backupPassword = "Yaare-Backup-Test-Only!"

func backupFixture(t *testing.T, n byte) (*BackupService, *sqlite.Store, context.Context, string) {
	t.Helper()
	cloud, store, ctx := cloudFixture(t, n)
	root := t.TempDir()
	s := NewBackupService(cloud)
	t.Cleanup(func() { s.release(callerPage(ctx)) })
	return s, store, ctx, root
}
func seedBackup(t *testing.T, s *BackupService, ctx context.Context, name string) (workspace.Host, workspace.Identity, string) {
	t.Helper()
	store := s.cloud.store
	p := s.cloud.protector
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKey(private, "")
	privateText := string(pem.EncodeToMemory(block))
	raw, _ := json.Marshal(vault.KeySecret{PrivateKey: []byte(privateText)})
	keyID := workspace.NewID()
	cipher, _ := p.Protect(raw, "key-v1:"+keyID)
	if _, err := store.RegisterKey(ctx, workspace.SavedKey{ID: keyID, Name: name + " key", Format: "OpenSSH", Cipher: cipher}); err != nil {
		t.Fatal(err)
	}
	identity := workspace.Identity{ID: workspace.NewID(), Name: name + " identity", Username: "yaaretest", Authentication: "password"}
	raw, _ = json.Marshal(vault.IdentitySecret{Password: []byte("Identity-Backup-Public-Test!")})
	cipher, _ = p.Protect(raw, identity.ID)
	if err := store.SaveIdentity(ctx, workspace.SealedIdentity{Identity: identity, Cipher: cipher}, false); err != nil {
		t.Fatal(err)
	}
	h := workspace.Host{ID: workspace.NewID(), Name: name, Address: "127.0.0.1", Port: 22222, Username: "root", Authentication: "password", IdentityID: identity.ID, Group: workspace.Ungrouped, System: "Linux", Icon: "linux"}
	next := store.Load(ctx).Snapshot
	next.Hosts = append(next.Hosts, h)
	if _, err := store.Commit(ctx, next.Revision, next); err != nil {
		t.Fatal(err)
	}
	return h, identity, privateText
}
func chooseBackup(t *testing.T, s *BackupService, ctx context.Context, blob []byte) BackupFile {
	t.Helper()
	file, err := s.stageImport(ctx, "测试配置.yaare", blob)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func fixtureCustomIcon(t *testing.T) string {
	t.Helper()
	var icon bytes.Buffer
	if err := png.Encode(&icon, image.NewNRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	return "custom-png:fixture:" + base64.StdEncoding.EncodeToString(icon.Bytes())
}

func TestPortableIconsDeduplicateAndRejectMissingContent(t *testing.T) {
	icon := fixtureCustomIcon(t)
	original := syncmodel.Payload{Snapshot: workspace.Snapshot{Hosts: []workspace.Host{{Icon: icon}, {Icon: icon}}}}
	packed := syncmodel.PackIcons(original)
	if len(packed.IconAssets) != 1 || packed.Snapshot.Hosts[0].Icon != packed.Snapshot.Hosts[1].Icon || original.Snapshot.Hosts[0].Icon != icon {
		t.Fatal("same icon was not deduplicated without mutating the source")
	}
	corrupt := packed
	corrupt.IconAssets = nil
	if err := syncmodel.UnpackIcons(&corrupt); err == nil {
		t.Fatal("missing icon content accepted")
	}
	if err := syncmodel.UnpackIcons(&packed); err != nil || packed.Snapshot.Hosts[1].Icon != icon {
		t.Fatal("icon reference failed to restore", err)
	}
}

func TestBackupRestoresProxyAndRejectsUnknownFields(t *testing.T) {
	a, sa, ca, _ := backupFixture(t, 52)
	host, _, _ := seedBackup(t, a, ca, "portable")
	host.Icon = fixtureCustomIcon(t)
	host.Proxy = workspace.ProxyConfig{Kind: "socks5", Address: "proxy.example", Port: 1080, Username: "user", UseAuthentication: true}
	source := NewWorkspaceService(app.NewWorkspace(sa), a.cloud.protector)
	const password = "mock-proxy-secret"
	if _, err := source.SaveConnection(ca, SaveConnectionRequest{Host: host, Revision: sa.Load(ca).Snapshot.Revision, ProxyPassword: password}); err != nil {
		t.Fatal(err)
	}
	command := workspace.Command{ID: "restored-command", Name: "Check Debian", Command: "pwd", Group: workspace.Ungrouped, Icon: "debian"}
	editSync(t, sa, ca, func(p *workspace.Snapshot) { p.Commands = append(p.Commands, command) })
	blob, err := a.exportBytes(ca, BackupExportRequest{Password: backupPassword, IncludeCredentials: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cloudsync.Open(blob, backupPassword)
	if err != nil || bytes.Contains(raw, []byte("RootPath")) || bytes.Contains(blob, []byte(password)) {
		t.Fatal("backup exposed local data or plaintext", err)
	}
	b, sb, cb, _ := backupFixture(t, 53)
	withUnknown := bytes.Replace(raw, []byte(`"workspace":{`), []byte(`"workspace":{"Unexpected":true,`), 1)
	if bytes.Equal(withUnknown, raw) {
		t.Fatal("workspace field missing from fixture")
	}
	badBlob, err := cloudsync.Seal(withUnknown, backupPassword)
	if err != nil {
		t.Fatal(err)
	}
	badFile := chooseBackup(t, b, cb, badBlob)
	if _, err := b.Preview(cb, badFile.ID, backupPassword); err == nil {
		t.Fatal("unknown workspace field accepted")
	}
	file := chooseBackup(t, b, cb, blob)
	preview, err := b.Preview(cb, file.ID, backupPassword)
	if err != nil || preview.Incoming.Hosts != 1 {
		t.Fatal("failed to preview archive", err)
	}
	if _, err := b.Apply(cb, preview.ID); err != nil {
		t.Fatal("import failed", err)
	}
	data, err := sb.CloudRead(cb)
	if err != nil || len(data.ProxyCredentials) != 1 {
		t.Fatal("portable records were not restored", err)
	}
	if data.Snapshot.Hosts[0].Icon != host.Icon {
		t.Fatal("custom connection icon was not restored")
	}
	found := false
	for _, restored := range data.Snapshot.Commands {
		if restored == command {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("command OS scope was not restored")
	}
	sealed := data.ProxyCredentials[0]
	secret, err := b.cloud.protector.Unprotect(sealed.Cipher, vault.ProxyCredentialPurpose(sealed))
	defer clear(secret)
	if err != nil || string(secret) != password {
		t.Fatal("proxy password was not re-encrypted", err)
	}
}

func TestBackupWithoutCredentialsPreservesTargetVault(t *testing.T) {
	a, _, ca, _ := backupFixture(t, 3)
	_, identity, _ := seedBackup(t, a, ca, "不含认证")
	b, sb, cb, _ := backupFixture(t, 4)
	_, existing, _ := seedBackup(t, b, cb, "现有身份")
	blob, err := a.exportBytes(ca, BackupExportRequest{Password: backupPassword, IncludeCredentials: false})
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := cloudsync.Open(blob, backupPassword)
	if bytes.Contains(plain, []byte("PRIVATE KEY")) || bytes.Contains(plain, []byte("Identity-Backup-Public-Test!")) {
		t.Fatal("excluded credentials present")
	}
	file := chooseBackup(t, b, cb, blob)
	preview, err := b.Preview(cb, file.ID, backupPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Apply(cb, preview.ID); err != nil {
		t.Fatal(err)
	}
	data, _ := sb.CloudRead(cb)
	if len(data.Identities) != 1 || data.Identities[0].ID != existing.ID || data.Snapshot.Hosts[0].IdentityID != "" || data.Snapshot.Hosts[0].KeyID != "" || data.Snapshot.Hosts[0].Username != identity.Username {
		t.Fatal("metadata import changed credentials", data)
	}
}
func TestBackupInvalidFilesDoNotChangeData(t *testing.T) {
	a, _, ca, _ := backupFixture(t, 5)
	seedBackup(t, a, ca, "source")
	blob, _ := a.exportBytes(ca, BackupExportRequest{Password: backupPassword, IncludeCredentials: true})
	b, sb, cb, _ := backupFixture(t, 6)
	before := sb.Load(cb).Snapshot.Revision
	file := chooseBackup(t, b, cb, blob)
	if _, err := b.Preview(cb, file.ID, "Wrong-Backup-Password!"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := b.Preview(cb, file.ID, backupPassword); err != nil {
		t.Fatal(err)
	}
	bad := bytes.Clone(blob)
	bad[len(bad)-1] ^= 1
	file = chooseBackup(t, b, cb, bad)
	if _, err := b.Preview(cb, file.ID, backupPassword); err == nil {
		t.Fatal("tamper accepted")
	}
	plain, _ := cloudsync.Open(blob, backupPassword)
	var env backupEnvelope
	json.Unmarshal(plain, &env)
	env.Version = syncmodel.Version + 1
	raw, _ := json.Marshal(env)
	bad, _ = cloudsync.Seal(raw, backupPassword)
	file = chooseBackup(t, b, cb, bad)
	if _, err := b.Preview(cb, file.ID, backupPassword); err == nil {
		t.Fatal("newer format accepted")
	}
	env.Version = syncmodel.Version
	env.MinReaderVersion = syncmodel.Version + 1
	raw, _ = json.Marshal(env)
	bad, _ = cloudsync.Seal(raw, backupPassword)
	file = chooseBackup(t, b, cb, bad)
	if _, err := b.Preview(cb, file.ID, backupPassword); err == nil {
		t.Fatal("newer reader requirement accepted")
	}
	env.MinReaderVersion = syncmodel.Version
	env.Workspace.Identities = nil
	raw, _ = json.Marshal(env)
	bad, _ = cloudsync.Seal(raw, backupPassword)
	file = chooseBackup(t, b, cb, bad)
	if _, err := b.Preview(cb, file.ID, backupPassword); err == nil {
		t.Fatal("missing identity accepted")
	}
	if sb.Load(cb).Snapshot.Revision != before {
		t.Fatal("invalid preview mutated database")
	}
}

func TestBackupExpiryDiscardAndNativeBoundary(t *testing.T) {
	s, _, ctx, _ := backupFixture(t, 9)
	blob, _ := s.exportBytes(ctx, BackupExportRequest{Password: backupPassword, IncludeCredentials: false})
	file := chooseBackup(t, s, ctx, blob)
	s.mu.Lock()
	s.selections[callerPage(ctx)].expires = time.Now().Add(-time.Minute)
	s.mu.Unlock()
	if _, err := s.Preview(ctx, file.ID, backupPassword); err == nil {
		t.Fatal("expired file accepted")
	}
	file = chooseBackup(t, s, ctx, blob)
	s.Discard(ctx)
	if _, err := s.Preview(ctx, file.ID, backupPassword); err == nil {
		t.Fatal("discarded file accepted")
	}
	if _, err := s.Export(ctx, BackupExportRequest{}); err == nil {
		t.Fatal("browser caller opened native save picker")
	}
	if _, err := s.ChooseImport(ctx); err == nil {
		t.Fatal("browser caller opened native file picker")
	}
	if _, err := s.SaveRestorePoint(ctx, "arbitrary"); err == nil {
		t.Fatal("browser caller saved arbitrary restore point")
	}
}
func TestWriteBackupPreservesDestinationOnCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "配置.yaare")
	os.WriteFile(path, []byte("original"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cloudsync.WriteArchive(ctx, path, []byte("new")); err == nil {
		t.Fatal("cancelled export committed")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Fatal("destination changed on cancellation")
	}
	if err := cloudsync.WriteArchive(context.Background(), path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "new" {
		t.Fatal("export did not commit")
	}
}
