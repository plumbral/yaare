//go:build windows

package ipc

import (
	"context"
	"testing"
	"yaare/internal/adapters/storage/sqlite"
)

func TestAgentIdentityResolutionAndExclusiveSources(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := sqlite.New(func() (string, error) { return dir, nil })
	defer store.Close()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	keys := NewPersistentKeyService(store)
	identities := NewIdentityService(store, keys)
	request := SaveIdentityRequest{ID: "agent-user", Name: "系统 Agent", Username: "deploy", Authentication: "key", UseAgent: true, ReplaceSecret: true}
	if err := identities.save(ctx, request); err != nil {
		t.Fatal(err)
	}
	service, closeService := NewSSHService(keys)
	defer closeService()
	ConfigureSSHIdentities(service, identities)
	cfg, release, err := service.resolveSSHConfig(ctx, SSHConnectRequest{Address: "127.0.0.1", Port: 22, IdentityID: request.ID})
	defer release()
	if err != nil || !cfg.UseAgent || cfg.Username != "deploy" || cfg.PrivateKeyPath != "" || len(cfg.PrivateKey) != 0 || len(cfg.Password) != 0 {
		t.Fatal("Agent metadata was not resolved exclusively", err)
	}
	for _, r := range []SSHConnectRequest{
		{Authentication: "password", UseAgent: true},
		{Authentication: "key", UseAgent: true, Password: "manual"},
		{Authentication: "key", UseAgent: true, KeyID: "key"},
		{Authentication: "key", UseAgent: true, PrivateKey: "inline"},
	} {
		if _, release, e := service.resolveSSHConfig(ctx, r); e == nil {
			release()
			t.Fatal("mixed source accepted")
		}
	}
	request.Password = "manual"
	if err = identities.save(ctx, request); err == nil {
		t.Fatal("Agent identity accepted secret")
	}
	if _, err = service.AgentInfo(ctx); err == nil {
		t.Fatal("unowned page queried system agent")
	}
}
