package secrets

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func TestRecordScopeLifetime(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	readCount := 0
	scope := &recordScope{format: serverRecord, loadKey: func(create bool) ([]byte, error) {
		readCount++
		if !create {
			t.Error("first access should be a write")
		}
		return key, nil
	}}
	defer scope.release()
	if _, err := scope.Unprotect([]byte("invalid header"), "id"); err == nil || readCount != 0 {
		t.Fatal("malformed input accessed key store")
	}
	var wait sync.WaitGroup
	for range 10 {
		wait.Go(func() {
			sealed, err := scope.Protect([]byte("payload"), "id")
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := scope.Unprotect(sealed, "wrong id"); err == nil {
				t.Error("accepted another record's ciphertext")
			}
			got, err := scope.Unprotect(sealed, "id")
			if err != nil || string(got) != "payload" {
				t.Errorf("round trip failed: %v", err)
			}
		})
	}
	wait.Wait()
	scope.release()
	if readCount != 1 || !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatal("expected one key read and a wiped key")
	}
	if _, err := scope.Protect([]byte("payload"), "id"); err == nil || readCount != 1 {
		t.Fatal("released scope reopened")
	}
}

func TestRecordScopeMissingReadDoesNotCreateKey(t *testing.T) {
	missing := errors.New("missing key")
	var attempts []bool
	scope := &recordScope{format: serverRecord, loadKey: func(create bool) ([]byte, error) {
		attempts = append(attempts, create)
		if !create {
			return nil, missing
		}
		return bytes.Repeat([]byte{1}, 32), nil
	}}
	defer scope.release()
	sealed := append([]byte(serverRecord.header), make([]byte, 12+16)...)
	if _, err := scope.Unprotect(sealed, "id"); !errors.Is(err, missing) {
		t.Fatal(err)
	}
	if _, err := scope.Protect([]byte("payload"), "id"); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0] || !attempts[1] {
		t.Fatalf("unexpected key creation requests %v", attempts)
	}
}
