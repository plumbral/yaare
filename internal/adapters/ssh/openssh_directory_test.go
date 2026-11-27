package sshclient

import (
	"strings"
	"testing"
)

func TestRealOpenSSHDirectoryReportsAndSafeNavigation(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	initial := r.wait(t, "directory").Directory
	if initial == nil || !initial.Ready || initial.Path != "/home/yaaretest" {
		t.Fatal("missing initial shell report", initial)
	}
	if !r.session.Write([]byte("mkdir -p -- \"/tmp/yaare-dir-中文 'literal\"; cd -- \"/tmp/yaare-dir-中文 'literal\"\n")) {
		t.Fatal("input rejected")
	}
	var current *DirectoryState
	for current == nil || !current.Ready {
		current = r.wait(t, "directory").Directory
	}
	if current.Path != "/tmp/yaare-dir-中文 'literal" {
		t.Fatal("unicode path", current)
	}
	if err := r.session.ChangeDirectory("/home/yaaretest"); err != nil {
		t.Fatal(err)
	}
	current = nil
	for current == nil || !current.Ready {
		current = r.wait(t, "directory").Directory
	}
	if current.Path != "/home/yaaretest" {
		t.Fatal(current)
	}
	if err := r.session.ChangeDirectory("/tmp/yaare-dir-中文 'literal"); err != nil {
		t.Fatal(err)
	}
	current = nil
	for current == nil || !current.Ready {
		current = r.wait(t, "directory").Directory
	}
	if current.Path != "/tmp/yaare-dir-中文 'literal" {
		t.Fatal("typed API quoting", current)
	}
	if !r.session.Write([]byte("printf 'DIR_INPUT_%s' 'PRESERVED'")) {
		t.Fatal("write")
	}
	if r.session.ChangeDirectory("/tmp") == nil {
		t.Fatal("changed directory over pending input")
	}
	r.session.Write([]byte("\n"))
	eventually(t, func() bool { return strings.Contains(r.text(), "DIR_INPUT_PRESERVED") })
	current = nil
	for current == nil || !current.Ready {
		current = r.wait(t, "directory").Directory
	}
	r.session.Write([]byte("vim -Nu NONE -n /tmp/yaare-directory-readonly-test\n"))
	eventually(t, func() bool { return strings.Contains(r.text(), "\x1b[?1049h") })
	if r.session.ChangeDirectory("/home/yaaretest") == nil {
		t.Fatal("navigation was sent into Vim")
	}
	r.session.Write([]byte("\x1b:q!\r"))
	current = nil
	for current == nil || !current.Ready {
		current = r.wait(t, "directory").Directory
	}
	t.Log("Initial directory, Unicode/quote path, explicit navigation and pending-input protection verified.")
}
