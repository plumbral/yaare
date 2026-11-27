package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"yaare/internal/adapters/secrets"
	"yaare/internal/domain/workspace"
)

func TestConnectionCredentialPlan(t *testing.T) {
	protector, err := secrets.NewServerVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, auth, key, identity string
		agent                     bool
		input                     ConnectionInput
		action, code              string
	}{
		{name: "retain password", auth: "password", input: ConnectionInput{RememberPassword: true}, action: "keep"},
		{name: "remove password", auth: "password", action: "clear"},
		{name: "replace password", auth: "password", input: ConnectionInput{RememberPassword: true, Password: []byte("example")}, action: "replace"},
		{name: "unchecked password", auth: "password", input: ConnectionInput{Password: []byte("example")}, code: "CREDENTIAL_INVALID"},
		{name: "retain managed key passphrase", auth: "key", key: "shared-key", action: "keep"},
		{name: "replace identity key passphrase", auth: "key", identity: "identity", input: ConnectionInput{Passphrase: []byte("example")}, action: "replace"},
		{name: "agent clears host secret", auth: "key", agent: true, action: "clear"},
		{name: "identity owns password", auth: "password", identity: "identity", input: ConnectionInput{RememberPassword: true}, action: "clear"},
		{name: "agent refuses passphrase", auth: "key", agent: true, input: ConnectionInput{Passphrase: []byte("example")}, code: "CREDENTIAL_INVALID"},
		{name: "existing key and pasted material", auth: "key", key: "shared-key", input: ConnectionInput{PrivateKey: []byte("unused")}, code: "CREDENTIAL_INVALID"},
		{name: "two key sources", auth: "key", input: ConnectionInput{PrivateKey: []byte("unused"), KeyPath: "unused"}, code: "CREDENTIAL_INVALID"},
		{name: "proxy password without proxy", auth: "password", input: ConnectionInput{ProxyPassword: []byte("example")}, code: "PROXY_AUTH_INVALID"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			host := workspace.Host{ID: "host", Address: "localhost", Port: 22, Username: "user", Authentication: scenario.auth,
				KeyID: scenario.key, IdentityID: scenario.identity, UseAgent: scenario.agent}
			h, change, err := PrepareConnection(protector, host, scenario.input)
			if scenario.code != "" {
				var cause *workspace.Error
				if !errors.As(err, &cause) || cause.Code != scenario.code || h.ID != "" || change.HostID != "" {
					t.Fatal("invalid input yielded an unexpected result")
				}
				return
			}
			if err != nil || change.Action != scenario.action || change.HostID != h.ID {
				t.Fatal("unexpected credential action", change.Action, err)
			}
			if scenario.action != "replace" {
				if len(change.Credential.Cipher) != 0 {
					t.Fatal("unexpected new ciphertext")
				}
				return
			}
			if !workspace.CredentialMatches(h, change.Credential) {
				t.Fatal("credential is not bound to the saved endpoint")
			}
			cipher := change.Credential.Cipher
			raw, err := protector.Unprotect(cipher, HostCredentialPurpose(change.Credential))
			defer clear(raw)
			var value HostPassword
			if err != nil || bytes.Contains(cipher, []byte("example")) || json.Unmarshal(raw, &value) != nil || string(value.Password) != "example" {
				t.Fatal("invalid sealed credential")
			}
		})
	}
}
