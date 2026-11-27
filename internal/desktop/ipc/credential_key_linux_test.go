//go:build linux

package ipc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

func TestLinuxLostKeyCannotReenrollWorkspace(t *testing.T) {
	if os.Getenv("YAARE_DISPOSABLE_DBUS") != "1" || os.Getenv("YAARE_TEST_LINUX_KEYRING") != "1" ||
		!strings.HasPrefix(filepath.Base(filepath.Dir(os.Getenv("XDG_RUNTIME_DIR"))), "yaare-keyring-test.") {
		t.Skip("requires the isolated keyring test script")
	}
	ctx := context.Background()
	for _, marked := range []bool{false, true} {
		dir := t.TempDir()
		root := func() (string, error) { return dir, nil }
		store := sqlite.New(root)
		t.Cleanup(func() { store.Close() })
		if err := store.Initialize(ctx); err != nil {
			t.Fatal(err)
		}
		protected := DesktopCredentialProtector(store, root)
		var first secrets.Protector = secrets.CurrentUser{}
		if marked {
			first = protected
		}
		keyID := workspace.NewID()
		cipher, err := first.Protect([]byte("fixture"), "key-v1:"+keyID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.RegisterKey(ctx, workspace.SavedKey{ID: keyID, Name: "fixture", Format: "OpenSSH", Cipher: cipher}); err != nil {
			t.Fatal(err)
		}
		if err = exec.Command("secret-tool", "clear", "application", "app.yaare", "purpose", "credential-master-v1").Run(); err != nil {
			t.Fatal(err)
		}
		if _, err = protected.Protect([]byte("second"), "another"); !errors.Is(err, secrets.ErrWorkspaceKey) {
			t.Fatalf("missing key accepted (marked=%t): %v", marked, err)
		}
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			t.Fatal(err)
		}
		var unlocked, locked []dbus.ObjectPath
		err = conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets").Call("org.freedesktop.Secret.Service.SearchItems", 0, map[string]string{"application": "app.yaare", "purpose": "credential-master-v1"}).Store(&unlocked, &locked)
		conn.Close()
		if err != nil || len(unlocked)+len(locked) != 0 {
			t.Fatalf("a failed write created a replacement master key: %v", err)
		}
	}
}
