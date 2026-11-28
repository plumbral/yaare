package sshclient

import (
	"context"
	"testing"
	"time"
)

func TestRealOpenSSHBashIntegrationLockSpansSSHConnections(t *testing.T) {
	first := realFixture(t, "yaaretest")
	first.wait(t, "connected")
	second := realFixture(t, "yaaretest")
	second.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	home, err := first.session.bashCommandOutput(ctx, `cd -P "$HOME" && pwd`, 4096)
	if err != nil || !validDirectory(home) {
		t.Fatal("fixture home unavailable", err)
	}

	lockCtx, cancelLock := context.WithCancel(ctx)
	defer cancelLock()
	unlock, err := first.session.lockBashDirectory(lockCtx, home)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	waitCtx, stopWaiting := context.WithTimeout(ctx, 200*time.Millisecond)
	alsoLocked, err := second.session.lockBashDirectory(waitCtx, home)
	stopWaiting()
	if err == nil {
		alsoLocked()
		t.Fatal("two independent connections acquired the same home directory")
	}
	cancelLock()
	retry, err := second.session.lockBashDirectory(ctx, home)
	if err != nil {
		t.Fatal("cancelled apply retained the remote lock", err)
	}
	retry()
}
