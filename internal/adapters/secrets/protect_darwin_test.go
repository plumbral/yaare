package secrets

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ebitengine/purego"
)

func TestMacDisposableKeychain(t *testing.T) {
	if os.Getenv("YAARE_TEST_MAC_KEYCHAIN") != "1" {
		t.Skip("set YAARE_TEST_MAC_KEYCHAIN=1 in a macOS test account")
	}
	api, err := macKeychain()
	if err != nil {
		t.Fatal(err)
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		t.Fatal(err)
	}
	defer purego.Dlclose(lib)
	var create func(string, uint32, *byte, uint8, uintptr, *uintptr) int32
	var remove, lock func(uintptr) int32
	var unlock func(uintptr, uint32, *byte, uint8) int32
	purego.RegisterLibFunc(&create, lib, "SecKeychainCreate")
	purego.RegisterLibFunc(&remove, lib, "SecKeychainDelete")
	purego.RegisterLibFunc(&lock, lib, "SecKeychainLock")
	purego.RegisterLibFunc(&unlock, lib, "SecKeychainUnlock")
	pass := make([]byte, 32)
	if _, err := rand.Read(pass); err != nil {
		t.Fatal(err)
	}
	defer clear(pass)
	var chain uintptr
	if status := create(filepath.Join(t.TempDir(), "yaare-test.keychain"), uint32(len(pass)), &pass[0], 0, 0, &chain); status != 0 || chain == 0 {
		t.Fatalf("create disposable keychain: %d", status)
	}
	defer api.release(chain)
	defer func() {
		if status := remove(chain); status != 0 {
			t.Errorf("delete disposable keychain: %d", status)
		}
	}()
	if _, err := api.masterKey(chain, false); !errors.Is(err, errKeychainMissing) {
		t.Fatalf("read from empty keychain: %v", err)
	}
	const clients = 4
	var keys [clients][]byte
	var errs [clients]error
	var wait sync.WaitGroup
	for i := range clients {
		wait.Go(func() { keys[i], errs[i] = api.masterKey(chain, true) })
	}
	wait.Wait()
	for i := range clients {
		defer clear(keys[i])
		if errs[i] != nil || len(keys[i]) != 32 || !bytes.Equal(keys[0], keys[i]) {
			t.Fatalf("concurrent first write returned inconsistent key: %v", errs[i])
		}
	}
	v, err := newRecordVault(keys[0], macRecord)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("fixture credential")
	sealed, err := v.Protect(plain, "a")
	if err != nil || bytes.Contains(sealed, plain) {
		t.Fatal("seal failed", err)
	}
	if _, err = v.Unprotect(sealed, "b"); !errors.Is(err, macRecord.invalid) {
		t.Fatal("wrong record", err)
	}
	if status := lock(chain); status != 0 {
		t.Fatalf("lock: %d", status)
	}
	for _, write := range []bool{false, true} {
		if _, err := api.masterKey(chain, write); !errors.Is(err, errKeychainLocked) {
			t.Fatalf("locked keychain: %v", err)
		}
	}
	if status := unlock(chain, uint32(len(pass)), &pass[0], 1); status != 0 {
		t.Fatalf("unlock: %d", status)
	}
	key, err := api.masterKey(chain, false)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	v, err = newRecordVault(key, macRecord)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := v.Unprotect(sealed, "a")
	if err != nil || !bytes.Equal(recovered, plain) {
		t.Fatal("unlock round trip", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := v.Unprotect(sealed, "a"); !errors.Is(err, macRecord.invalid) {
		t.Fatal("tampered record", err)
	}
}
