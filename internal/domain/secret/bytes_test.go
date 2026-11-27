package secret

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestStringCompatibilityAndOwnership(t *testing.T) {
	for _, value := range []string{"", "päss密码", "quote\" slash\\ newline\n", "\x00<>&"} {
		old, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var b Bytes
		if err := json.Unmarshal(old, &b); err != nil {
			t.Fatal(err)
		}
		clear(old)
		if string(b) != value {
			t.Fatal("decoded secret changed with source")
		}
		encoded, err := json.Marshal(b)
		want, _ := json.Marshal(value)
		if err != nil || !bytes.Equal(encoded, want) {
			t.Fatal("wire format changed")
		}
		prev := b
		if err := json.Unmarshal([]byte(`"replacement"`), &b); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(prev, make([]byte, len(prev))) {
			t.Fatal("old secret retained")
		}
		last := b
		b.Clear()
		if b != nil || !bytes.Equal(last, make([]byte, len(last))) {
			t.Fatal("secret retained after clear")
		}
	}
	data, err := json.Marshal(Bytes(nil))
	if err != nil || string(data) != `""` {
		t.Fatal("nil secret must encode like empty string")
	}
}
