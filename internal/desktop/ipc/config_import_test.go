package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yaare/internal/app/vault"

	"yaare/internal/adapters/sshconfig"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

func TestConfigPreviewOwnershipPrivatePathsAndAtomicCommit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := sqlite.New(func() (string, error) { return filepath.Join(root, "store"), nil })
	defer store.Close()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	service := NewSSHConfigService(store)
	if _, err := service.Preview(ctx, false); err == nil {
		t.Fatal("unowned read accepted")
	}
	if _, err := service.Commit(ctx, "x", nil); err == nil {
		t.Fatal("unowned commit accepted")
	}
	keyDir := filepath.Join(root, "private-location")
	os.MkdirAll(keyDir, 0700)
	keyPath := filepath.Join(keyDir, "id_example")
	os.WriteFile(keyPath, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nPRIVATE_CONTENT_MUST_NOT_RETURN\n"), 0600)
	path := filepath.Join(root, "config")
	body := "Host gate\n HostName 127.0.0.1\n User tester\n IdentityFile \"" + filepath.ToSlash(keyPath) + "\"\nHost target\n HostName localhost\n User deploy\n ProxyJump gate\n"
	os.WriteFile(path, []byte(body), 0600)
	page, other := new(callerScope), new(callerScope)
	preview, err := service.previewFile(ctx, page, path, sshconfig.Options{Home: root, Username: "local"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(preview)
	if strings.Contains(string(encoded), "private-location") || strings.Contains(string(encoded), "PRIVATE_CONTENT") || len(preview.Rows) != 2 {
		t.Fatalf("preview exposed private data or missing rows")
	}
	choices := []ConfigImportChoice{{ID: preview.Rows[0].ID, KeyIndex: 0}, {ID: preview.Rows[1].ID, KeyIndex: -1}}
	if _, err = service.commitPlan(ctx, other, preview.Token, choices); err == nil {
		t.Fatal("cross-page preview accepted")
	}
	if _, err = service.commitPlan(ctx, page, preview.Token, choices[1:]); err == nil {
		t.Fatal("missing jump selection accepted")
	}
	if len(store.Load(ctx).Snapshot.Hosts) != 0 {
		t.Fatal("partial import")
	}
	result, err := service.commitPlan(ctx, page, preview.Token, choices)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Hosts) != 2 || len(result.Snapshot.Hosts[1].JumpHostIDs) != 1 {
		t.Fatal(result)
	}
	if _, err = service.commitPlan(ctx, page, preview.Token, choices); err == nil {
		t.Fatal("preview reused after commit")
	}
	preview, err = service.previewFile(ctx, page, path, sshconfig.Options{Home: root, Username: "local"})
	if err != nil || !preview.Rows[0].Duplicate || !preview.Rows[1].Duplicate {
		t.Fatal(preview, err)
	}
}

func TestDefaultConfigPreviewAndImport(t *testing.T) {
	_, store, ctx := cloudFixture(t, 88)
	service := NewSSHConfigService(store)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := service.Preview(ctx, false); err == nil || err.Error() != "未找到 ~/.ssh/config。" {
		t.Fatalf("expected missing default file message, got %v", err)
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("Host sample\n HostName 127.0.0.1\n User test\n Port 2222\n"), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview(ctx, false)
	if err != nil || len(preview.Rows) != 1 || preview.Rows[0].Error != "" {
		t.Fatalf("failed default file preview: %v, %v", preview, err)
	}
	_, err = service.Commit(ctx, preview.Token, []ConfigImportChoice{{ID: preview.Rows[0].ID, KeyIndex: -1}})
	if err != nil {
		t.Fatal(err)
	}
	hosts := store.Load(ctx).Snapshot.Hosts
	if len(hosts) != 1 || hosts[0].Name != "sample" || hosts[0].Port != 2222 {
		t.Fatal("imported default file was not saved")
	}
}
func TestConfigJumpCycleAndMissingHost(t *testing.T) {
	for _, hosts := range [][]sshconfig.Host{{{Alias: "a", Jumps: []string{"a"}}}, {{Alias: "a", Jumps: []string{"missing"}}}, {{Alias: "a", Jumps: []string{"b", "b"}}, {Alias: "b"}}} {
		all := map[string]sshconfig.Host{}
		for _, host := range hosts {
			all[host.Alias] = host
		}
		if _, err := importRoute(hosts[0], all, map[string]bool{}, 0); err == nil {
			t.Fatal("invalid route accepted", hosts)
		}
	}
}

func TestConfigPreviewMatchIsolationAndUnsupportedKey(t *testing.T) {
	_, store, ctx := cloudFixture(t, 88)
	service := NewSSHConfigService(store)
	root := t.TempDir()
	path := filepath.Join(root, "config")
	body := "Host affected safe uncertain\n User deploy\nHost via\n ProxyJump affected\nHost via-uncertain\n ProxyJump uncertain\nMatch originalhost affected\n Port 2222\nMatch originalhost uncertain exec untrusted-command\n Port 2223\nHost safe\n IdentityFile ~/encrypted.pem\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "encrypted.pem"), []byte("-----BEGIN ENCRYPTED PRIVATE KEY-----\nSYNTHETIC\n"), 0600); err != nil {
		t.Fatal(err)
	}
	page := callerPage(ctx)
	preview, err := service.previewFile(ctx, page, path, sshconfig.Options{Home: root, Username: "local"})
	if err != nil || len(preview.Rows) != 5 {
		t.Fatal(preview, err)
	}
	rows := make(map[string]ConfigImportRow, len(preview.Rows))
	for _, row := range preview.Rows {
		rows[row.Name] = row
	}
	affected, safe, via := rows["affected"], rows["safe"], rows["via"]
	if affected.Error != "" || affected.Port != 2222 || via.Error != "" || rows["uncertain"].Error == "" || rows["via-uncertain"].Error == "" ||
		safe.Error != "" || safe.Port != 22 || len(safe.KeyAvailable) != 1 || safe.KeyAvailable[0] || len(safe.Warnings) != 1 || safe.Warnings[0] != vault.ErrEncryptedPKCS8.Error() {
		t.Fatal(preview.Rows)
	}
	if _, err := service.commitPlan(ctx, page, preview.Token, []ConfigImportChoice{{ID: safe.ID, KeyIndex: 0}}); !errors.Is(err, vault.ErrEncryptedPKCS8) {
		t.Fatal("unsupported key associated", err)
	}
	choices := []ConfigImportChoice{{ID: safe.ID, KeyIndex: -1}, {ID: affected.ID, KeyIndex: -1}, {ID: via.ID, KeyIndex: -1}}
	result, err := service.commitPlan(ctx, page, preview.Token, choices)
	if err != nil || len(result.Snapshot.Hosts) != 3 {
		t.Fatal(result, err)
	}
	imported := map[string]workspace.Host{}
	for _, host := range result.Snapshot.Hosts {
		imported[host.Name] = host
	}
	gate, jumps := imported["affected"], imported["via"].JumpHostIDs
	if gate.Port != 2222 || len(jumps) != 1 || jumps[0] != gate.ID || imported["safe"].Port != 22 {
		t.Fatal("matched values or jump route not preserved in import", imported)
	}
}
