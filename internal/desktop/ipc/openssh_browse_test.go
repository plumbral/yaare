package ipc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"testing"
	"time"
)

func TestRealOpenSSHRelayConflictKeepsReviewedTarget(t *testing.T) {
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" {
		t.Skip("requires isolated Docker fixture")
	}
	ctx, cancel := context.WithTimeout(testOwner(t.Context()), 30*time.Second)
	defer cancel()
	page := callerPage(ctx)
	source, target := realBroadcastSession(t), realBroadcastSession(t)
	ssh, closeSSH := NewSSHService(nil)
	defer closeSSH()
	ssh.runs["source"] = &ownedSSH{name: "Source A", page: page, attempt: 1, session: source.session}
	ssh.runs["target"] = &ownedSSH{name: "Target B", page: page, attempt: 1, session: target.session}
	files, closeFiles := NewFileService(ssh)
	defer closeFiles()
	client, release, err := source.session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	home, err := client.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := path.Join(home, fmt.Sprintf("yaare-conflict-%d", time.Now().UnixNano()))
	if err = client.Mkdir(root); err != nil {
		t.Fatal(err)
	}
	defer client.RemoveDirectory(root)
	for _, dir := range []string{"from", "to"} {
		base := path.Join(root, dir)
		if err = client.Mkdir(base); err != nil {
			t.Fatal(err)
		}
		defer client.RemoveDirectory(base)
		for _, name := range []string{"a", "b"} {
			filePath := path.Join(base, name)
			f, e := client.Create(filePath)
			if e != nil {
				t.Fatal(e)
			}
			_, e = f.Write([]byte(dir))
			closeErr := f.Close()
			if e != nil || closeErr != nil {
				t.Fatal(e, closeErr)
			}
			defer client.Remove(filePath)
		}
	}
	id, err := files.Transfer(ctx, TransferRequest{Source: FileLocation{Side: "remote", SessionID: "source", Attempt: 1, Path: path.Join(root, "from")},
		Destination: FileLocation{Side: "remote", SessionID: "target", Attempt: 1, Path: path.Join(root, "to")}, Names: []string{"a", "b"}, Policy: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(state, previous string) FileTask {
		t.Helper()
		for ctx.Err() == nil {
			for _, task := range files.taskSnapshot(page).Tasks {
				if task.ID != id {
					continue
				}
				if task.State == "failed" {
					t.Fatal(task.Code)
				}
				if task.State == state && (previous == "" || task.ConflictID != previous) {
					return task
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("timed out waiting for", state)
		return FileTask{}
	}
	first := wait("conflict", "")
	if !strings.HasPrefix(first.SourceLabel, "Source A · ") || !strings.HasPrefix(first.DestinationLabel, "Target B · ") ||
		!strings.Contains(first.DestinationLabel, "@") || strings.ContainsRune(first.DestinationLabel, 0) || first.ConflictID == "" ||
		first.ConflictSource != path.Join(root, "from", first.Current) || first.ConflictTarget != path.Join(root, "to", first.Current) {
		t.Fatal("incomplete public conflict identity")
	}
	if err = client.Remove(first.ConflictTarget); err != nil {
		t.Fatal(err)
	}
	if err = files.ResolveConflict(ctx, id, first.ConflictID, "overwrite"); err != nil {
		t.Fatal(err)
	}
	second := wait("conflict", first.ConflictID)
	if second.ConflictTarget == first.ConflictTarget || files.ResolveConflict(ctx, id, first.ConflictID, "overwrite") == nil {
		t.Fatal("old overwrite applied to a different file")
	}
	changed, err := client.Create(second.ConflictTarget)
	if err != nil {
		t.Fatal(err)
	}
	_, err = changed.Write([]byte("updated during review"))
	closeErr := changed.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	if err = files.ResolveConflict(ctx, id, second.ConflictID, "overwrite"); err != nil {
		t.Fatal(err)
	}
	revised := wait("conflict", second.ConflictID)
	if revised.ConflictTarget != second.ConflictTarget {
		t.Fatal("changed target must be reviewed again")
	}
	if err = files.ResolveConflict(ctx, id, revised.ConflictID, "skip"); err != nil {
		t.Fatal(err)
	}
	wait("completed", "")
	f, err := client.Open(second.ConflictTarget)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(data) != "updated during review" {
		t.Fatal("unapproved destination changed")
	}
}

func TestRealOpenSSHBrowseOwnershipAndIndependentDownload(t *testing.T) {
	if os.Getenv("YAARE_OPENSSH_TEST") != "1" {
		t.Skip("requires isolated Docker fixture")
	}
	terminal := realBroadcastSession(t)
	ssh, closeSSH := NewSSHService(nil)
	t.Cleanup(closeSSH)
	files, closeFiles := NewFileService(ssh)
	t.Cleanup(closeFiles)
	page := &callerScope{}
	ssh.runs["browse-fixture"] = &ownedSSH{page: page, attempt: 2, session: terminal.session}
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), callerScopeKey{}, page), 20*time.Second)
	defer cancel()
	location := FileLocation{Side: "remote", SessionID: "browse-fixture", Attempt: 2}
	home, err := files.List(ctx, location)
	if err != nil || !strings.HasPrefix(home.Path, "/") {
		t.Fatalf("list home: %q, %v", home.Path, err)
	}
	location.Path = path.Join(home.Path, "files")
	if _, err = files.List(ctx, location); err != nil {
		t.Fatal(err)
	}
	foreign := context.WithValue(ctx, callerScopeKey{}, &callerScope{})
	if _, err = files.List(foreign, location); err == nil {
		t.Fatal("foreign page accessed a warm browse channel")
	}
	old := location
	old.Attempt--
	if _, err = files.List(ctx, old); err == nil {
		t.Fatal("stale attempt accessed a warm browse channel")
	}
	client, closeClient, err := terminal.session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	name := fmt.Sprintf("yaare-browse-%d.txt", time.Now().UnixNano())
	remote := path.Join(location.Path, name)
	content := bytes.Repeat([]byte("Yaare browse / SFTP fixture\n"), 8192)
	file, err := client.Create(remote)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Remove(remote); err != nil {
			t.Errorf("remove isolated test file: %v", err)
		}
	}()
	_, err = file.Write(content)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("write fixture file: %v, %v", err, closeErr)
	}
	download, err := client.Open(remote)
	if err != nil {
		t.Fatal(err)
	}
	defer download.Close()
	buffer := make([]byte, len(content))
	const firstRead = 32768
	if _, err = io.ReadFull(download, buffer[:firstRead]); err != nil {
		t.Fatal(err)
	}
	if !terminal.session.Write([]byte("printf '%s%s\\n' YAARE_BROWSE_ PTY_OK\r")) {
		t.Fatal("PTY stopped accepting input during initial SFTP read")
	}
	listed, err := files.List(ctx, location)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range listed.Entries {
		found = found || entry.Name == name && entry.Size == int64(len(content))
	}
	if !found {
		t.Fatal("warm browse did not return newly written file metadata")
	}
	if _, err = io.ReadFull(download, buffer[firstRead:]); err != nil || !bytes.Equal(buffer, content) {
		t.Fatalf("independent download did not survive browse: %v", err)
	}
	waitBroadcastOutput(t, func() bool { return strings.Contains(terminal.text(), "YAARE_BROWSE_PTY_OK") })
}
