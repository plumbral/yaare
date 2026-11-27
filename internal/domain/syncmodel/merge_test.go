package syncmodel

import (
	"bytes"
	"encoding/json"
	"testing"

	"yaare/internal/domain/workspace"
)

func TestMergeMaterialOwnership(t *testing.T) {
	input := Payload{
		Version: Version, Snapshot: workspace.Empty(),
		HostCredentials:  []HostCredential{{HostID: "host", Material: json.RawMessage(`{"Password":"login"}`)}},
		ProxyCredentials: []HostCredential{{HostID: "host", Material: json.RawMessage(`"proxy"`)}},
		Keys:             []Key{{ID: "key", Material: json.RawMessage(`{"PrivateKey":"pem"}`)}},
		Identities:       []Identity{{Metadata: workspace.Identity{ID: "identity"}, Material: json.RawMessage(`{"Passphrase":"phrase"}`)}},
		Extensions:       workspace.PortableExtensions{"example": json.RawMessage(`{"value":1}`)},
	}
	input.Snapshot.Hosts = []workspace.Host{{ID: "host"}}
	side := entries(input)
	side.clearOwned()
	for _, id := range []string{"host/host", "key/key", "identity/identity"} {
		if len(side[id]) == 0 || !bytes.Equal(side[id], make([]byte, len(side[id]))) {
			t.Fatalf("temporary %s bytes survived cleanup", id)
		}
	}
	check := func(p Payload) {
		t.Helper()
		if len(p.HostCredentials) != 1 || string(p.HostCredentials[0].Material) != `{"Password":"login"}` ||
			len(p.ProxyCredentials) != 1 || string(p.ProxyCredentials[0].Material) != `"proxy"` ||
			len(p.Keys) != 1 || string(p.Keys[0].Material) != `{"PrivateKey":"pem"}` ||
			len(p.Identities) != 1 || string(p.Identities[0].Material) != `{"Passphrase":"phrase"}` ||
			string(p.Extensions["example"]) != `{"value":1}` {
			t.Fatal("credential material or borrowed extension changed")
		}
	}
	check(input)
	canonical, err := Canonicalize(input)
	if err != nil {
		t.Fatal(err)
	}
	merged, conflicts, err := Merge(canonical, canonical, canonical, nil)
	if err != nil || len(conflicts) != 0 {
		t.Fatal("unexpected merge result", conflicts, err)
	}
	check(canonical)
	ClearPayload(canonical)
	check(input)
	check(merged)
	ClearPayload(merged)
	check(input)

	duplicate := input
	duplicate.Keys = []Key{input.Keys[0], input.Keys[0]}
	result, err := Canonicalize(duplicate)
	if err == nil {
		t.Fatal("duplicate accepted")
	}
	ClearPayload(result)
	check(input)
}
