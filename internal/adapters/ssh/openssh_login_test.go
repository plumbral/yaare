package sshclient

import (
	"strings"
	"testing"
)

func TestRealOpenSSHNativeLoginMOTD(t *testing.T) {
	const motd = "Yaare OpenSSH fixture MOTD"
	const result = "LOGIN_COMMAND_RAN"
	for login := 0; login < 2; login++ {
		r := realFixture(t, "yaaretest")
		r.wait(t, "connected")
		eventually(t, func() bool { return strings.Contains(r.text(), motd) })
		if !r.session.Write([]byte("printf 'LOGIN_%s\\n' COMMAND_RAN\r")) {
			t.Fatal("shell rejected input after its login message")
		}
		eventually(t, func() bool { return strings.Contains(r.text(), result) })
		out := r.text()
		if strings.Count(out, motd) != 1 || strings.Index(out, motd) >= strings.Index(out, result) {
			t.Fatalf("login %d did not show the server MOTD once before command output", login+1)
		}
		r.session.Close()
		r.ended(t)
	}
}
