package sshclient

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"

	filefs "yaare/internal/adapters/files"
)

type bashTestFiles struct {
	filefs.Local
	writable string
	edits    *sync.Mutex
}

type bashTestInfo struct {
	fs.FileInfo
	mode fs.FileMode
}

func (i bashTestInfo) Mode() fs.FileMode { return i.mode }

func (f bashTestFiles) Lstat(name string) (fs.FileInfo, error) {
	info, err := f.Local.Lstat(name)
	if err != nil {
		return nil, err
	}
	mode := info.Mode().Type() | 0600
	if f.writable == name {
		mode |= 0020
	}
	return bashTestInfo{info, mode}, nil
}

func bashTestRoot(t *testing.T) bashTestFiles {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return bashTestFiles{Local: filefs.Local{Root: root}, edits: new(sync.Mutex)}
}

func (f bashTestFiles) lock() (func(), error) {
	f.edits.Lock()
	return f.edits.Unlock, nil
}

func TestManagedBashIntegrationRestoresLoginProfile(t *testing.T) {
	for _, create := range []bool{false, true} {
		f := bashTestRoot(t)
		original := "PROMPT_COMMAND='user_prompt'"
		if !create {
			if err := f.Root.WriteFile(".bash_login", []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
		}
		ctx := context.Background()
		plan := func(scope string) bashPlan {
			t.Helper()
			p, err := planBashIntegration(ctx, f, "/", 1, scope)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		install := plan("first")
		if install.Action != "install" || create != (install.ProfileChange == "create") || plan("other").Revision == install.Revision {
			t.Fatal("incorrect initial preview or session binding")
		}
		if _, err := f.Local.Lstat(install.KeyPath); !errors.Is(err, fs.ErrNotExist) {
			t.Fatal("preview wrote account key")
		}
		if err := applyBashPlan(ctx, f, install); err != nil {
			t.Fatal(err)
		}
		remove := plan("first")
		if remove.Action != "remove" || remove.KeyChange != "delete" || create != (remove.ProfileChange == "delete") || remove.Revision == install.Revision {
			t.Fatal("incorrect removal preview")
		}
		if !create {
			if remove.ProfilePath != "/.bash_login" || remove.profile.Content != original+install.Block {
				t.Fatal("existing profile or its text changed")
			}
			if err := f.Root.WriteFile(".bash_login", []byte(remove.profile.Content+"\nuser_prompt_change\n"), 0600); err != nil {
				t.Fatal(err)
			}
			stale := remove
			remove = plan("first")
			if stale.Revision == remove.Revision || !errors.Is(applyBashPlan(ctx, f, stale), errBashChanged) {
				t.Fatal("stale preview accepted")
			}
		}
		if err := applyBashPlan(ctx, f, remove); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Local.Lstat(remove.KeyPath); !errors.Is(err, fs.ErrNotExist) {
			t.Fatal("private key not removed")
		}
		if create {
			if _, err := f.Local.Lstat(remove.ProfilePath); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("new empty profile left behind")
			}
		} else if raw, err := f.Root.ReadFile(".bash_login"); err != nil || string(raw) != original+"\nuser_prompt_change\n" {
			t.Fatal("unmanaged changes not preserved")
		}
	}
}

func TestBashPreviewRefusesUnsafeAndForeignFiles(t *testing.T) {
	for _, c := range []struct{ name, content, writable string }{
		{bashKeyFile, "unrelated account file\n", ""},
		{bashKeyFile, "yaare-bash-v1 ../elsewhere 0 " + strings.Repeat("0", 64) + "\n", ""},
		{bashKeyFile, "yaare-bash-v1 .profile 1 " + strings.Repeat("1", 64) + "\n", "/" + bashKeyFile},
		{".profile", "unchanged", "/.profile"},
		{".profile", bashStartMarker + "\nuser_modified\n" + bashEndMarker, ""},
	} {
		f := bashTestRoot(t)
		f.writable = c.writable
		if err := f.Root.WriteFile(c.name, []byte(c.content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := planBashIntegration(context.Background(), f, "/", 1, "session"); !errors.Is(err, errBashIntegration) {
			t.Fatal("unsafe file accepted", c.name, err)
		}
	}
}

func TestBashPreviewDoesNotFollowSymbolicLinks(t *testing.T) {
	f := bashTestRoot(t)
	if err := f.Root.WriteFile("dotfiles", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.Root.Symlink("dotfiles", ".profile"); err != nil {
		t.Skip("symbolic links unavailable", err)
	}
	if _, err := planBashIntegration(context.Background(), f, "/", 1, "session"); !errors.Is(err, errBashIntegration) {
		t.Fatal("symbolic link accepted", err)
	}
}

func TestBashIntegrationIsScopedAcrossConcurrentConnections(t *testing.T) {
	f := bashTestRoot(t)
	ctx := context.Background()
	if err := f.Root.WriteFile(".profile", []byte("unaltered user text"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"install", "remove"} {
		for _, crossScope := range []bool{true, false} {
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, scope := range []string{"one", "two"} {
				preview, err := manageBashFiles(ctx, f, "/", 1, scope, "", "", true, nil)
				if err != nil || preview.Action != action {
					t.Fatal("unexpected preview", err)
				}
				go func() {
					<-start
					applyScope := scope
					if crossScope {
						applyScope = "another connection"
					}
					_, err := manageBashFiles(ctx, f, "/", 1, applyScope, action, preview.Revision, true, f.lock)
					results <- err
				}()
			}
			close(start)
			count := 0
			for range 2 {
				if err := <-results; err == nil {
					count++
				} else if !errors.Is(err, errBashChanged) {
					t.Fatal("unexpected apply error", err)
				}
			}
			if crossScope && count != 0 || !crossScope && count != 1 {
				t.Fatalf("invalid number of successful commits: %d", count)
			}
		}
	}
	raw, err := f.Root.ReadFile(".profile")
	if err != nil || string(raw) != "unaltered user text" {
		t.Fatal("original content not restored", err)
	}
}

func TestBashIntegrationRepairsInterruptedInstallWithoutChangingProfile(t *testing.T) {
	f := bashTestRoot(t)
	ctx := context.Background()
	initial, err := planBashIntegration(ctx, f, "/", 1, "session")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Root.WriteFile(".profile", []byte("user just created this file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := applyBashPlan(ctx, f, initial); err == nil {
		t.Fatal("newly created profile overwritten")
	}
	cleanup, err := manageBashFiles(ctx, f, "/", 1, "session", "", "", false, nil)
	if err != nil || cleanup.Action != "remove" || cleanup.KeyChange != "delete" || cleanup.ProfileChange != "none" || cleanup.Block != "" {
		t.Fatal("orphan key cleanup did not describe actual changes", err)
	}
	if _, err = manageBashFiles(ctx, f, "/", 1, "session", cleanup.Action, cleanup.Revision, false, f.lock); err != nil {
		t.Fatal(err)
	}
	raw, err := f.Root.ReadFile(".profile")
	if err != nil || string(raw) != "user just created this file" {
		t.Fatal("user file changed", err)
	}
	if _, err := f.Root.Lstat(bashKeyFile); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("orphan key retained", err)
	}
}
