package ipc

import (
	"bytes"
	"context"
	"encoding/json"

	"os"
	"path/filepath"
	"testing"
)

func TestTerminalUploadSelectionIsPrivateSequentialAndDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "selected.bin")
	data := bytes.Repeat([]byte{0, 127, 128, 255}, 20000)
	os.WriteFile(name, data, 0600)
	s, shutdown := NewFileService(nil)
	defer shutdown()
	page := new(callerScope)
	scope := TerminalFileScope{"session", 1, "lease"}
	selection, e := s.terminalUploadsFromPaths(page, scope, []string{name})
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(selection)
	if bytes.Contains(encoded, []byte(dir)) {
		t.Fatal("OS path exposed")
	}
	if len(selection.Files) != 1 || selection.Files[0].Name != "selected.bin" {
		t.Fatal(selection)
	}
	if _, e = s.TerminalUploadRead(context.Background(), selection.ID, "selected.bin"); e == nil {
		t.Fatal("unowned caller read selected data")
	}
	source := s.terminalUploads[selection.ID]
	var result []byte
	for {
		part, e := source.read("selected.bin")
		if e != nil {
			t.Fatal(e)
		}
		if len(part.Data) > 32768 {
			t.Fatal("unbounded packet")
		}
		result = append(result, part.Data...)
		if part.Final {
			break
		}
	}
	if !bytes.Equal(result, data) {
		t.Fatal("selected bytes changed")
	}
	if _, e = source.read("selected.bin"); e == nil {
		t.Fatal("source replayed")
	}
	second, e := s.terminalUploadsFromPaths(page, scope, []string{name})
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(name, []byte("changed"), 0600)
	if _, e = s.terminalUploads[second.ID].read("selected.bin"); e == nil {
		t.Fatal("changed source sent")
	}
	s.mu.Lock()
	s.closeTerminalFilesLocked(new(callerScope), false)
	if len(s.terminalUploads) != 2 {
		t.Fatal("foreign page closed selection")
	}
	s.closeTerminalFilesLocked(page, false)
	s.mu.Unlock()
	if len(s.terminalUploads) != 0 {
		t.Fatal("window sources retained")
	}
}
func TestTerminalFileRPCsRequireOwnedLeaseAndPage(t *testing.T) {
	s, shutdown := NewFileService(nil)
	defer shutdown()
	ctx := context.Background()
	scope := TerminalFileScope{"session", 1, "lease"}
	if _, e := s.TerminalReceiveChooseFolder(ctx, scope); e == nil {
		t.Fatal("unowned chooser")
	}
	if _, e := s.TerminalReceivePrepare(ctx, TerminalReceiveRequest{Scope: scope}); e == nil {
		t.Fatal("unowned prepare")
	}
	if e := s.TerminalReceiveWrite(ctx, "id", 0, []byte("data")); e == nil {
		t.Fatal("unowned write")
	}
	if e := s.TerminalReceiveCommit(ctx, "id"); e == nil {
		t.Fatal("unowned commit")
	}
	if e := s.TerminalFilesCancel(ctx, scope); e == nil {
		t.Fatal("unowned cancel")
	}
}
