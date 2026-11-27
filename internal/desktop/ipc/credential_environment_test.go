//go:build windows || linux

package ipc

import (
	"os"
	"runtime"
	"testing"
)

func requireCurrentUserCredentialTests(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "linux" && (os.Getenv("YAARE_TEST_LINUX_KEYRING") != "1" || os.Getenv("YAARE_DISPOSABLE_DBUS") != "1") {
		t.Skip("requires disposable real Linux Secret Service session")
	}
}
