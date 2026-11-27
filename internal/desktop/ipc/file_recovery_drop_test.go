package ipc

import (
	"context"
	"github.com/egoist/mygo"
	"testing"
	"time"
)

func TestRecoveryDropRequiresExactNativeSelectionAndPage(t *testing.T) {
	service, closeService := NewFileService(nil)
	defer closeService()
	page, other := new(callerScope), new(callerScope)
	paths := []string{`C:\private\source-a`, `C:\private\source-b`}
	service.recoverable = map[string]transferRecord{"recover": {DropPaths: paths}}
	native := func(owner *callerScope, paths []string) string {
		event := &mygo.FileDropEvent{Paths: append([]string(nil), paths...)}
		service.captureDrop(owner, event)
		return event.Paths[0]
	}
	token := native(page, []string{paths[1], paths[0]})
	if err := service.authorizeRecoveredDrop(other, "recover", token); err == nil {
		t.Fatal("foreign page claimed drop")
	}
	if err := service.authorizeRecoveredDrop(page, "recover", token); err != nil {
		t.Fatal(err)
	}
	if service.recoverable["recover"].dropPage != page || len(service.recoverable["recover"].authorizedDrop) != 2 {
		t.Fatal("authorization not bound")
	}
	if err := service.authorizeRecoveredDrop(page, "recover", token); err == nil {
		t.Fatal("native grant replayed")
	}
	wrong := native(page, []string{paths[0], `C:\private\different`})
	if err := service.authorizeRecoveredDrop(page, "recover", wrong); err == nil {
		t.Fatal("different source accepted")
	}
	if service.recoverable["recover"].dropPage != nil {
		t.Fatal("stale authorization retained")
	}
	expired := native(page, paths)
	grant := service.drops[expired]
	grant.at = time.Now().Add(-3 * time.Minute)
	service.drops[expired] = grant
	if err := service.authorizeRecoveredDrop(page, "recover", expired); err == nil {
		t.Fatal("expired drop accepted")
	}
	if err := service.AuthorizeRecoveredDrop(context.Background(), "recover", token); err == nil {
		t.Fatal("unowned page authorized source")
	}
}

func TestRecoveryPickerMatchesAllSourcesWithoutGrantingParent(t *testing.T) {
	service, closeService := NewFileService(nil)
	defer closeService()
	page := new(callerScope)
	paths := []string{`C:\private\one.txt`, `C:\private\folder`}
	service.recoverable = map[string]transferRecord{"picker": {DropPaths: paths}}
	service.mu.Lock()
	defer service.mu.Unlock()
	for _, selected := range [][]string{{paths[0]}, {`C:\private`}, {paths[0], paths[0]}} {
		if err := service.authorizeRecoveryPathsLocked(page, "picker", selected); err == nil {
			t.Fatal("incomplete or different selection accepted")
		}
	}
	if err := service.authorizeRecoveryPathsLocked(page, "picker", []string{paths[1], paths[0]}); err != nil {
		t.Fatal(err)
	}
	if len(service.recoverable["picker"].authorizedDrop) != 2 {
		t.Fatal("original sources not granted")
	}
	if err := service.authorizeRecoveryPathsLocked(page, "missing", paths); err == nil {
		t.Fatal("removed record authorized")
	}
}
