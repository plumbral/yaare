package sshclient

import (
	"strings"
	"testing"
)

func TestBroadcastCommandFramingAndInvalidControls(t *testing.T) {
	data, e := BroadcastInput("  printf '中文'\r\n\r\npwd", true)
	if e != nil || string(data) != "  printf '中文'\r\rpwd\r" {
		t.Fatal("multiline framing", e)
	}
	data, e = BroadcastInput("printf '🦂'", false)
	if e != nil || string(data) != "printf '🦂'" {
		t.Fatal("fill framing", e)
	}
	for _, value := range []string{"", "\t\n", "ls\x00", "ls\x1b[A", "bare\rcarriage", "bad\u2028separator", strings.Repeat("a", 65536)} {
		if _, e = BroadcastInput(value, true); e == nil {
			t.Fatal("invalid accepted")
		}
	}
	if _, e = BroadcastInput("pwd\nwhoami", false); e == nil {
		t.Fatal("multiline fill can execute")
	}
}
