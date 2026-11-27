package sshclient

import (
	"bytes"
	"context"
	"testing"
	"time"
)

type transferInput struct{ bytes.Buffer }

func (w *transferInput) Close() error { return nil }
func TestTerminalTransferOwnsBinaryInputAndBlocksOtherCommands(t *testing.T) {
	input := &transferInput{}
	s := NewSession(context.Background(), nil, func(Event) error { return nil })
	defer s.Close()
	s.stdin = input
	s.connected = true
	s.directory.Ready = true
	token, e := s.BeginTerminalTransfer()
	if e != nil {
		t.Fatal(e)
	}
	if s.Write([]byte("must not interleave")) {
		t.Fatal("normal input entered protocol")
	}
	if s.ChangeDirectory("/tmp") == nil {
		t.Fatal("cd interrupted transfer")
	}
	if s.WriteTerminalTransfer("wrong", []byte{0, 255}) {
		t.Fatal("foreign lease wrote bytes")
	}
	if !s.WriteTerminalTransfer(token, []byte{0, 128, 255}) {
		t.Fatal("binary write rejected")
	}
	if !bytes.Equal(input.Bytes(), []byte{0, 128, 255}) {
		t.Fatal("binary data changed")
	}
	if s.EndTerminalTransfer("wrong", true) {
		t.Fatal("foreign lease ended transfer")
	}
	if !s.EndTerminalTransfer(token, true) || s.TerminalTransferActive() {
		t.Fatal("lease not released")
	}
	if !s.Write([]byte("after")) {
		t.Fatal("normal input did not resume")
	}
	if input.Len() != 3+16+5 {
		t.Fatal("cancel sequence length", input.Len())
	}
}

func TestTerminalTransferKeepsRawCompletionBeforePrivatePromptReport(t *testing.T) {
	events := make(chan Event, 4)
	s := NewSession(context.Background(), nil, func(event Event) error {
		event.Data = append([]byte(nil), event.Data...)
		events <- event
		return nil
	})
	defer s.Close()
	s.connected = true
	s.stdin = &transferInput{}
	s.terminalTransfer = "lease"
	s.directoryDecoder.token = "fixture"
	close(s.outputReady)
	raw := []byte("OO\x1b]777;yaare;fixture;/home/test\x07prompt$ ")
	if n, e := (remoteWriter{s}).Write(raw); e != nil || n != len(raw) {
		t.Fatal("write", n, e)
	}
	read := func() Event {
		select {
		case event := <-events:
			return event
		case <-time.After(time.Second):
			t.Fatal("prompt/bytes not delivered")
			return Event{}
		}
	}
	first, second := read(), read()
	if first.Type != "output" || !bytes.Equal(first.Data, raw) || second.Type != "directory" || !second.Directory.Ready {
		t.Fatal("prompt overtook completion", first, second)
	}
	if _, e := (remoteWriter{s}).Write([]byte("\x1b]7;file:///other\x07")); e != nil {
		t.Fatal(e)
	}
	s.flushOutput()
	if event := read(); event.Type != "output" {
		t.Fatal("ordinary OSC path from protocol became directory metadata", event)
	}
}
