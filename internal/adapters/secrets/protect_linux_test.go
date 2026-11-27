//go:build linux

package secrets

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestLinuxCipherBindsRecordAndRejectsDamage(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	plain := []byte("synthetic-secret-中文")
	first, err := sealLinuxCredential(plain, "identity:a", key)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := sealLinuxCredential(plain, "identity:a", key)
	if bytes.Equal(first, second) || bytes.Contains(first, plain) {
		t.Fatal("cipher not randomized or plaintext retained")
	}
	decoded, err := openLinuxCredential(first, "identity:a", key)
	if err != nil || !bytes.Equal(decoded, plain) {
		t.Fatal("round trip failed", err)
	}
	if _, err = openLinuxCredential(first, "identity:b", key); err == nil {
		t.Fatal("record ID not authenticated")
	}
	other := append([]byte(nil), key...)
	other[0] ^= 1
	if _, err = openLinuxCredential(first, "identity:a", other); err == nil {
		t.Fatal("wrong account key accepted")
	}
	for _, at := range []int{0, 4, len(first) - 1} {
		bad := append([]byte(nil), first...)
		bad[at] ^= 1
		if _, err = Unprotect(bad[:4], "identity:a"); err == nil {
			t.Fatal("short record accepted")
		}
		if _, err = openLinuxCredential(bad, "identity:a", key); err == nil {
			t.Fatal("damaged record accepted")
		}
	}
}
func TestLinuxMissingBusNeverFallsBack(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "absent-bus"))
	if _, err := Protect([]byte("secret"), "record"); !errors.Is(err, errLinuxKeyring) {
		t.Fatal("unavailable keyring not reported", err)
	}
	if _, err := Protect(nil, "record"); err == nil {
		t.Fatal("empty credential accepted")
	}
}

func requireLinuxTestKeyring(t *testing.T) {
	t.Helper()
	if os.Getenv("YAARE_TEST_LINUX_KEYRING") != "1" || os.Getenv("YAARE_DISPOSABLE_DBUS") != "1" {
		t.Skip("requires disposable real Secret Service session")
	}
}
func TestLinuxRealKeyringProtectionAndLock(t *testing.T) {
	requireLinuxTestKeyring(t)
	plain := []byte("synthetic-keyring-only-secret")
	sealed, err := Protect(plain, "keyring-test")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("plaintext cipher")
	}
	decoded, err := Unprotect(sealed, "keyring-test")
	if err != nil || !bytes.Equal(decoded, plain) {
		t.Fatal("real service roundtrip failed", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			out, e := Protect(plain, "parallel")
			if e != nil {
				t.Error(e)
				return
			}
			back, e := Unprotect(out, "parallel")
			if e != nil || !bytes.Equal(back, plain) {
				t.Error("parallel service access failed", e)
			}
		})
	}
	wg.Wait()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	service := conn.Object(secretDestination, "/org/freedesktop/secrets")
	var collection dbus.ObjectPath
	if err = service.Call(secretService+".ReadAlias", 0, "default").Store(&collection); err != nil {
		t.Fatal(err)
	}
	if err = service.Call(secretService+".Lock", 0, []dbus.ObjectPath{collection}).Err; err != nil {
		t.Fatal(err)
	}
	if _, err = Unprotect(sealed, "keyring-test"); !errors.Is(err, errLinuxLocked) {
		t.Fatal("key cache bypassed locked service", err)
	}
	if _, err = Protect(plain, "locked-test"); !errors.Is(err, errLinuxLocked) {
		t.Fatal("locked service saved secret", err)
	}
}
func TestLinuxKeyringProcessHelper(t *testing.T) {
	if os.Getenv("YAARE_KEYRING_HELPER") != "1" {
		t.Skip("child process only")
	}
	sealed, err := Protect([]byte("process-secret"), "process-record")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("YAARE_KEYRING_OUTPUT"), sealed, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLinuxRealKeyringConcurrentFirstCreation(t *testing.T) {
	requireLinuxTestKeyring(t)
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		name := filepath.Join(dir, string(rune('a'+i)))
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestLinuxKeyringProcessHelper$")
			cmd.Env = append(os.Environ(), "YAARE_KEYRING_HELPER=1", "YAARE_KEYRING_OUTPUT="+name)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("child failed: %v %s", err, out)
			}
		})
	}
	wg.Wait()
	for i := 0; i < 6; i++ {
		sealed, err := os.ReadFile(filepath.Join(dir, string(rune('a'+i))))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := Unprotect(sealed, "process-record")
		if err != nil || string(plain) != "process-secret" {
			t.Fatal("concurrent creation replaced master key", err)
		}
	}
}
