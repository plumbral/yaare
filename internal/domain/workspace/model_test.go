package workspace

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTypedBoundaryAndValidation(t *testing.T) {
	var change Change
	data := `{"kind":"saveHost","host":{"id":"host","name":"Test","address":"example.com","port":22,"username":"root","group":"Test","password":"secret-password","passphrase":"secret-phrase","privateKey":"secret-key"}}`
	if err := json.Unmarshal([]byte(data), &change); err != nil {
		t.Fatal(err)
	}
	next, _, err := Next(Empty(), change, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(next)
	if strings.Contains(string(encoded), "secret-") || strings.Contains(string(encoded), "passphrase") {
		t.Fatal("non-whitelisted data retained")
	}
	for _, bad := range []Host{{ID: "bad", Name: "Test", Address: "example.com", Port: 0, Username: "root", Group: "Test"}, {ID: "bad", Name: "Test", Address: "example.com", Port: 22, Username: "root", Group: "Test", Authentication: "key", KeyID: "missing"}} {
		if _, _, err := Next(Empty(), Change{Kind: "saveHost", Host: bad}, nil); err == nil {
			t.Fatal("invalid host accepted")
		}
	}
	if _, _, err := Next(Empty(), Change{Kind: "saveCommands", Commands: []Command{{ID: "x", Name: "x", Command: "ls\x1b[2J"}}}, nil); err == nil {
		t.Fatal("terminal escape sequence accepted")
	}
	if _, _, err := Next(Empty(), Change{Kind: "deleteGroup", Name: Ungrouped}, nil); err == nil {
		t.Fatal("default group deleted")
	}
	if !ValidGroup("中文分组") || ValidGroup("bad\u2028name") {
		t.Fatal("group validation")
	}
}

func TestPrivateKeyModeWithoutStoredKeyMaterial(t *testing.T) {
	current := Empty()
	host := Host{ID: "inline", Name: "Inline", Address: "example.com", Port: 22, Username: "root", Group: Ungrouped, Authentication: "key"}
	next, _, err := Next(current, Change{Kind: "saveHost", Host: host}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.Hosts[0].Authentication != "key" || next.Hosts[0].KeyID != "" {
		t.Fatal("secret-free key mode was not retained")
	}
	host.KeyID = "unregistered"
	if _, _, err := Next(current, Change{Kind: "saveHost", Host: host}, nil); err == nil {
		t.Fatal("unregistered key reference accepted")
	}
}
