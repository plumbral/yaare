package sshclient

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDirectoryFramesFragmentationBoundsAndEndpoint(t *testing.T) {
	frame := "\x1b]777;yaare;attempt-one;/tmp/%E4%B8%AD%E6%96%87%20%27%24%28literal%29\x07"
	for split := 0; split <= len(frame); split++ {
		d := directoryDecoder{token: "attempt-one", host: "example.test"}
		a := d.feed([]byte(frame[:split]), 0)
		b := d.feed([]byte(frame[split:]), 0)
		if b == nil {
			b = a
		}
		if b == nil || b.Path != "/tmp/中文 '$(literal)" || !b.Ready {
			t.Fatalf("split %d: %+v", split, b)
		}
	}
	d := directoryDecoder{token: "attempt-two", host: "example.test"}
	for _, frame := range []string{"\x1b]777;yaare;attempt-one;/stale\x07", "\x1b]7;file://other.test/tmp\x07", "\x1b]7;file://example.test/tmp%0Aevil\x07", "\x1b]777;yaare;attempt-two;/invalid%FF\x07", "\x1b]777;yaare;attempt-two;/" + strings.Repeat("a", 20000) + "\x07"} {
		if d.feed([]byte(frame), 0) != nil {
			t.Fatal("invalid report accepted")
		}
		if len(d.frame) > 16384 {
			t.Fatal("unbounded decoder")
		}
	}
	if got := d.feed([]byte("\x1b]7;file://example.test/valid%20path\x1b\\"), 0); got == nil || got.Path != "/valid path" || got.Ready {
		t.Fatal("standard OSC7", got)
	}
	if got := d.feed([]byte("\x1b]777;yaare;attempt-two;/new\x07"), 0); got == nil || !got.Ready {
		t.Fatal("decoder did not recover", got)
	}
}

type directoryInput struct{ bytes.Buffer }

func (*directoryInput) Close() error { return nil }

func TestPersistentDirectoryFramesRequireVerifiedNonceAndTag(t *testing.T) {
	nonce, tag := strings.Repeat("ab", 16), strings.Repeat("cd", 32)
	frame := func(nonce, tag, path string) []byte {
		return []byte("\x1b]777;yaare;bash;" + nonce + ";" + tag + ";" + path + "\a")
	}
	d := directoryDecoder{}
	if got := d.feed(frame(nonce, tag, "/first"), 4); got != nil || d.persistent == nil || d.persistent.epoch != 4 {
		t.Fatal("installed hook reported readiness before verification", got)
	}
	if got := d.feed(frame(nonce, tag, "/new%20path"), 5); got != nil || d.persistent.path != "/new path" || d.persistent.epoch != 5 {
		t.Fatal("newer prompt lost during verification", got)
	}
	d.persistent.verified = true
	for _, raw := range [][]byte{frame(strings.Repeat("da", 16), tag, "/x"), frame(nonce, strings.Repeat("ff", 32), "/x"), frame(nonce, tag, "/tmp/%07")} {
		if got := d.feed(raw, 5); got != nil {
			t.Fatal("accepted unverified directory", got)
		}
	}
	if got := d.feed(frame(nonce, tag, "/verified"), 5); got == nil || !got.Ready || !got.Integrated || got.Path != "/verified" {
		t.Fatal("missing verified prompt", got)
	}
}

type hookInput chan string

func (input hookInput) Write(p []byte) (int, error) { input <- string(p); return len(p), nil }
func (hookInput) Close() error                      { return nil }

func TestEnableShellIntegrationWaitsForAcknowledgementAndRunsOnce(t *testing.T) {
	s := NewSession(t.Context(), nil, func(Event) error { return nil })
	defer s.Close()
	input := make(hookInput, 4)
	s.connected, s.stdin, s.directoryDecoder.token = true, input, "attempt01"
	request, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { first <- s.EnableShellIntegration(request) }()
	if cmd := waitBrowseSignal(t, input); cmd != directoryHookCommand("attempt01") {
		t.Fatalf("unexpected install command %q", cmd)
	}
	cancel()
	if err := waitBrowseSignal(t, first); err != context.Canceled {
		t.Fatalf("cancelled request: %v", err)
	}
	started := make(chan error, 2)
	for range 2 {
		go func() { started <- s.EnableShellIntegration(t.Context()) }()
	}
	s.reportDirectory(*s.directoryDecoder.feed([]byte("\x1b]7;file:///test\a"), 1), 1)
	select {
	case err := <-started:
		t.Fatalf("public OSC 7 acknowledged installation: %v", err)
	default:
	}
	s.reportDirectory(*s.directoryDecoder.feed([]byte("\x1b]777;yaare;attempt01;/test\a"), 1), 1)
	for range 2 {
		if err := waitBrowseSignal(t, started); err != nil {
			t.Fatal(err)
		}
	}
	s.directoryBusy()
	if err := s.EnableShellIntegration(t.Context()); err != nil || len(input) != 0 {
		t.Fatalf("duplicate install after prompt became busy: %v, writes: %d", err, len(input))
	}
	osc := s.directoryDecoder.feed([]byte("\x1b]7;file:///test/other\a"), 2)
	if osc == nil || !osc.Integrated || osc.Ready {
		t.Fatal("unverified directory report changed integration status", osc)
	}
}

func TestDirectoryHookPreservesBashPromptCommands(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil || exec.CommandContext(t.Context(), bash, "--version").Run() != nil {
		t.Skip("requires Bash")
	}
	hook := strings.TrimSuffix(directoryHookCommand("123abc"), "\r")
	for _, setup := range []string{`PROMPT_COMMAND='printf before'`, `PROMPT_COMMAND=('printf be' 'printf fore')`} {
		script := setup + "\n" + hook + "\n" + hook + "\n" + `cd / && for entry in "${PROMPT_COMMAND[@]}"; do eval "$entry"; done`
		cmd := exec.CommandContext(t.Context(), bash, "--noprofile", "--norc", "-s")
		cmd.Stdin = strings.NewReader(script)
		cmd.Env = append(os.Environ(), "BASH_ENV=")
		out, err := cmd.CombinedOutput()
		if err != nil || string(out) != "before\x1b]777;yaare;123abc;/\a" {
			t.Fatalf("%s: %q, %v", setup, out, err)
		}
	}
}

func TestDirectoryFragmentStartedBeforeInputCannotRestoreReady(t *testing.T) {
	s := NewSession(context.Background(), nil, func(Event) error { return nil })
	s.stdin = &directoryInput{}
	s.connected = true
	s.directory = DirectoryState{Path: "/home", Ready: true}
	s.directoryDecoder = directoryDecoder{token: "fragment-attempt"}
	close(s.outputReady)
	defer func() {
		s.outputMu.Lock()
		_ = s.flushLocked()
		s.outputMu.Unlock()
		s.Close()
	}()
	w := remoteWriter{s}
	_, _ = w.Write([]byte("\x1b]777;yaare;fragment-attempt;/home"))
	if !s.Write([]byte("pending user input")) {
		t.Fatal("input rejected")
	}
	_, _ = w.Write([]byte("\x07"))
	s.mu.Lock()
	ready := s.directory.Ready
	s.mu.Unlock()
	if ready {
		t.Fatal("fragmented old prompt restored readiness over newer input")
	}
}

func TestDirectoryNavigationQuotesAndPreservesBusyInput(t *testing.T) {
	s := NewSession(context.Background(), nil, func(Event) error { return nil })
	defer s.Close()
	input := &directoryInput{}
	s.stdin = input
	s.connected = true
	s.directory = DirectoryState{Path: "/home", Ready: true}
	value := "/tmp/中文 '$(touch SHOULD_NOT_EXIST); folder"
	if err := s.ChangeDirectory(value); err != nil {
		t.Fatal(err)
	}
	if input.String() != " cd -- "+shellQuote(value)+"\r" {
		t.Fatal("path was not quoted as data", input.String())
	}
	before := input.String()
	if s.ChangeDirectory("/tmp") == nil || input.String() != before {
		t.Fatal("busy shell received another command")
	}
	s.directory.Ready = true
	s.Write([]byte("unsent user line"))
	before = input.String()
	if s.ChangeDirectory("/tmp") == nil || input.String() != before {
		t.Fatal("typed input overwritten")
	}
	for _, p := range []string{"relative", "/x\rreboot", "/x\x1b[2J", "/x\u0085", strings.Repeat("/", 4097)} {
		s.directory.Ready = true
		if s.ChangeDirectory(p) == nil {
			t.Fatal("invalid path accepted")
		}
	}
	s.reportDirectory(DirectoryState{Path: "/late", Ready: true}, 0)
	if s.directory.Ready {
		t.Fatal("old output restored ready state over newer input")
	}
}

func TestWriteDoesNotBlockWhenEventEmitStalls(t *testing.T) {
	outputEntered := make(chan struct{})
	releaseEmit := make(chan struct{})
	directoryEmitted := make(chan DirectoryState, 4)
	s := NewSession(context.Background(), nil, func(event Event) error {
		switch event.Type {
		case "output":
			close(outputEntered)
			<-releaseEmit
		case "directory":
			if event.Directory != nil {
				directoryEmitted <- *event.Directory
			}
		}
		return nil
	})
	defer s.Close()
	input := &directoryInput{}
	s.stdin = input
	s.connected = true
	s.directory = DirectoryState{Path: "/home", Ready: true}
	close(s.outputReady)

	s.outputMu.Lock()
	s.output = append(s.output, []byte("flood")...)
	s.outputMu.Unlock()
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		_ = s.flushOutput()
	}()
	<-outputEntered

	writeDone := make(chan bool, 1)
	go func() {
		writeDone <- s.Write([]byte{3})
	}()
	select {
	case ok := <-writeDone:
		if !ok {
			t.Fatal("Ctrl+C write failed")
		}
	case <-time.After(300 * time.Millisecond):
		close(releaseEmit)
		t.Fatal("Session.Write blocked on stalled eventMu during output flood")
	}
	if !bytes.Equal(input.Bytes(), []byte{3}) {
		close(releaseEmit)
		t.Fatalf("expected Ctrl+C written immediately, got %x", input.Bytes())
	}

	close(releaseEmit)
	<-flushDone
	select {
	case state := <-directoryEmitted:
		if state.Ready || state.Path != "/home" {
			t.Fatalf("unexpected directory state after busy write: %+v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("async directory state notification was not delivered after flood cleared")
	}
	s.stopDirectoryNotifications()
}
