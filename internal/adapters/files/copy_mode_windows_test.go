package files

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCopyModeWindowsACL(t *testing.T) {
	for _, scenario := range []string{"new", "overwrite-readonly", "locked-target"} {
		t.Run(scenario, func(t *testing.T) {
			source, src := sandbox(t)
			local, dst := sandbox(t)
			const name = "a"
			if err := os.WriteFile(filepath.Join(src, name), []byte("replacement"), 0600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dst, name)
			private, err := privateDescriptor()
			if err != nil {
				t.Fatal(err)
			}
			var expected *windows.SECURITY_DESCRIPTOR
			if scenario != "new" {
				if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
				expected = readCopyACL(t, local, name)
			} else {
				if err := os.WriteFile(filepath.Join(dst, "naturally-inherited"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				expected = readCopyACL(t, local, "naturally-inherited")
			}
			if scenario == "overwrite-readonly" {
				if err := os.Chmod(target, 0400); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(target, 0600) })
			}
			if scenario == "locked-target" {
				handle, err := local.openCopyObject(name, windows.FILE_GENERIC_READ, windows.FILE_SHARE_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer windows.CloseHandle(handle)
			}
			ctx := context.Background()
			items, total, err := Plan(ctx, source, "/", []string{name})
			if err != nil {
				t.Fatal(err)
			}
			stage := copyStagePath(name, "acl", 0)
			observed := false
			err = Copy(ctx, source, local, items, "/", "acl", total, overwriteCopy, func(p Progress) {
				if p.Done == 0 || p.Files != 0 || observed {
					return
				}
				observed = true
				if err := local.privateDir(path.Dir(stage), false); err != nil {
					t.Fatal(err)
				}
				if !sameACL(private, readCopyACL(t, local, stage)) {
					t.Fatal("stage has inherited access")
				}
			})
			if !observed || ((err != nil) != (scenario == "locked-target")) {
				t.Fatalf("unexpected copy result: %v", err)
			}
			observedACL := readCopyACL(t, local, name)
			if !sameACL(expected, observedACL) || !sameFilePrincipals(expected, observedACL) {
				t.Fatal("destination ACL changed")
			}
			info, err := local.Lstat(name)
			if err != nil {
				t.Fatal(err)
			}
			if (info.Mode().Perm()&0200 == 0) != (scenario == "overwrite-readonly") {
				t.Fatal("incorrect readonly mode")
			}
			want := "replacement"
			if scenario == "locked-target" {
				want = "old"
			}
			if got, err := os.ReadFile(target); err != nil || string(got) != want {
				t.Fatal("incorrect destination bytes", err)
			}
			if _, err := local.Lstat(path.Dir(stage)); !os.IsNotExist(err) {
				t.Fatal("staging directory not removed", err)
			}
			if probes, err := filepath.Glob(filepath.Join(dst, ".yaare-access-*")); err != nil || len(probes) != 0 {
				t.Fatal("ACL probe was not removed", probes, err)
			}
		})
	}
}

func TestCopyModeWindowsDifferentOwner(t *testing.T) {
	source, src := sandbox(t)
	dest, dst := sandbox(t)
	const name = "other-owner.txt"
	if err := os.WriteFile(filepath.Join(src, name), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dst, name)
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	original := readCopyACL(t, dest, name)
	owner, _, err := original.Owner()
	if err != nil {
		t.Fatal(err)
	}
	other, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	if owner.Equals(other) {
		other, err = windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
		if err != nil {
			t.Fatal(err)
		}
	}
	expected, err := original.ToAbsolute()
	if err != nil {
		t.Fatal(err)
	}
	if err := expected.SetOwner(other, false); err != nil {
		t.Fatal(err)
	}
	descriptor, err := expected.ToSelfRelative()
	if err != nil {
		t.Fatal(err)
	}
	handle, err := dest.openCopyObject(name, windows.WRITE_OWNER, shareAll, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if err != nil {
		t.Fatal(err)
	}
	setErr := windows.SetKernelObjectSecurity(handle, windows.OWNER_SECURITY_INFORMATION, descriptor)
	_ = windows.CloseHandle(handle)
	if setErr != nil {
		if !errors.Is(setErr, windows.ERROR_INVALID_OWNER) && !errors.Is(setErr, windows.ERROR_ACCESS_DENIED) && !errors.Is(setErr, windows.ERROR_PRIVILEGE_NOT_HELD) {
			t.Fatal(setErr)
		}
		stage := copyStagePath(name, "foreign-owner", 0)
		writer, err := createCopyStage(dest, stage, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		defer removeCopyStage(dest, stage)
		saved := readCopyACL(t, dest, stage)
		handle, err := dest.openCopyObject(stage, windows.WRITE_OWNER|windows.WRITE_DAC|windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, shareAll, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = applyCopySecurity(handle, descriptor)
		_ = windows.CloseHandle(handle)
		if err == nil {
			t.Fatal("ownership change unexpectedly accepted")
		}
		actual := readCopyACL(t, dest, stage)
		if !sameACL(saved, actual) || !sameFilePrincipals(saved, actual) {
			t.Fatal("denied ownership change modified staged access")
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "original" {
			t.Fatal("denied ownership change replaced the destination", err)
		}
		t.Log("Token cannot assign another owner; verified denial and unchanged file")
		return
	}
	before := readCopyACL(t, dest, name)
	if sameFilePrincipals(before, original) {
		t.Fatal("fixture did not change owner")
	}
	ctx := context.Background()
	items, total, err := Plan(ctx, source, "/", []string{name})
	if err != nil {
		t.Fatal(err)
	}
	if err = Copy(ctx, source, dest, items, "/", "foreign-owner", total, overwriteCopy, func(Progress) {}); err != nil {
		t.Fatal("could not preserve the authorized destination owner", err)
	}
	after := readCopyACL(t, dest, name)
	if !sameACL(before, after) || !sameFilePrincipals(before, after) {
		t.Fatal("replacement changed access or ownership")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "replacement" {
		t.Fatal("replacement failed", err)
	}
}

func readCopyACL(t *testing.T, f Local, p string) *windows.SECURITY_DESCRIPTOR {
	t.Helper()
	handle, err := f.openCopyObject(p, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, shareAll, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	acl, err := objectACL(handle)
	if err != nil {
		t.Fatal(err)
	}
	return acl
}
