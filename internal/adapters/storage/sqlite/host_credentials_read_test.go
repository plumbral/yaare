package sqlite

import (
	"context"
	"errors"
	"testing"

	"yaare/internal/domain/workspace"
)

func TestHostCredentialDistinguishesMissingRecordAndReadFailure(t *testing.T) {
	store := openTest(t, t.TempDir())
	_, err := store.HostCredential(testContext, "missing")
	var problem *workspace.Error
	if !errors.As(err, &problem) || problem.Code != "CREDENTIAL_UNAVAILABLE" {
		t.Fatalf("expected missing credentials, got %v", err)
	}
	cancelled, cancel := context.WithCancel(testContext)
	cancel()
	_, err = store.HostCredential(cancelled, "missing")
	if !errors.As(err, &problem) || problem.Code != "STORAGE_READ_FAILED" {
		t.Fatalf("expected a storage failure, got %v", err)
	}
}
