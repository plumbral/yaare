package files

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

func posixSFTPFixture(t *testing.T, root string) Remote {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	server, err := sftp.NewServer(serverConn, sftp.WithServerWorkingDirectory(root))
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve() }()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = server.Close()
		<-served
	})
	client, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return Remote{Client: client}
}

func TestCopyStageInSetgidDirectory(t *testing.T) {
	sourcePath := t.TempDir()
	dest, destPath := sandbox(t)
	if err := os.WriteFile(filepath.Join(sourcePath, "document.txt"), []byte("isolated SFTP fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(destPath, fs.ModeSetgid|0775); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	remote := posixSFTPFixture(t, sourcePath)
	items, total, err := Plan(ctx, remote, ".", []string{"document.txt"})
	if err != nil {
		t.Fatal(err)
	}
	stage := copyStagePath("/document.txt", "setgid", 0)
	observed := false
	err = Copy(ctx, remote, dest, items, "/", "setgid", total, overwriteCopy, func(progress Progress) {
		if progress.Done > 0 && progress.Files == 0 && !observed {
			observed = true
			assertMode(t, dest, path.Dir(stage), fs.ModeDir|fs.ModeSetgid|0700)
		}
	})
	if err != nil || !observed {
		t.Fatal("private directory with inherited setgid was rejected", err)
	}
	if got, err := os.ReadFile(filepath.Join(destPath, "document.txt")); err != nil || string(got) != "isolated SFTP fixture" {
		t.Fatal("download missing", err)
	}
}

type ignoredStageChmod struct {
	FS
	initialMode fs.FileMode
	attempts    *int
}

func (f ignoredStageChmod) Mkdir(p string) error {
	if err := f.FS.Mkdir(p); err != nil {
		return err
	}
	return f.FS.Chmod(p, f.initialMode)
}

func (f ignoredStageChmod) Chmod(string, fs.FileMode) error {
	*f.attempts++
	return nil
}

func (f ignoredStageChmod) privateDir(p string, create bool) error {
	if create {
		return createRemotePrivateDir(f, p)
	}
	return verifyPrivateDir(f, p)
}

func TestCopyStageServerIgnoresChmod(t *testing.T) {
	for _, scenario := range []struct {
		name string
		mode fs.FileMode
	}{
		{"already private", 0700},
		{"others can traverse", 0711},
		{"others can read", 0755},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			remote := posixSFTPFixture(t, t.TempDir())
			attempts := 0
			server := ignoredStageChmod{FS: remote, initialMode: scenario.mode, attempts: &attempts}
			stage := copyStagePath("document.txt", "chmod", 0)
			writer, err := createCopyStage(server, stage, 1)
			if scenario.mode == 0700 {
				if err != nil || attempts != 0 {
					t.Fatal("private directory should not require chmod", attempts, err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || writer != nil || attempts != 1 {
				t.Fatal("ignored chmod must not allow writing", attempts, err)
			}
			if _, err := remote.Lstat(path.Dir(stage)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("failed staging directory not removed", err)
			}
		})
	}
}

func TestCopyModeSFTP(t *testing.T) {
	for _, scenario := range []string{"overwrite", "new", "cancel", "chmod-failure", "rename-failure"} {
		t.Run(scenario, func(t *testing.T) {
			source, src := sandbox(t)
			dst := t.TempDir()
			remote := posixSFTPFixture(t, dst)
			data := bytes.Repeat([]byte("transfer"), 20000)
			if err := os.WriteFile(filepath.Join(src, "file"), data, 0755); err != nil {
				t.Fatal(err)
			}
			if scenario != "new" {
				if err := os.WriteFile(filepath.Join(dst, "file"), []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(dst, "file"), 0754|fs.ModeSetuid); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			items, total, err := Plan(ctx, source, "/", []string{"file"})
			if err != nil {
				t.Fatal(err)
			}
			var dest FS = remote
			if scenario == "chmod-failure" || scenario == "rename-failure" {
				dest = faultyCopyMode{FS: remote, fail: scenario}
			}
			stage := copyStagePath("file", "modes", 0)
			observed := false
			err = Copy(ctx, source, dest, items, ".", "modes", total, overwriteCopy, func(p Progress) {
				if p.Done == 0 || p.Files != 0 || observed {
					return
				}
				observed = true
				assertMode(t, remote, path.Dir(stage), fs.ModeDir|0700)
				assertMode(t, remote, stage, 0600)
				if scenario == "cancel" {
					cancel()
				}
			})
			if !observed {
				t.Fatal("no private stage observed", err)
			}
			expected, mode := data, fs.FileMode(0754)
			if scenario == "new" {
				mode = 0755
			}
			if scenario == "cancel" || scenario == "chmod-failure" || scenario == "rename-failure" {
				expected, mode = []byte("old"), 0754|fs.ModeSetuid
				if err == nil || scenario == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("failure ignored", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			assertMode(t, remote, "file", mode)
			if got, err := os.ReadFile(filepath.Join(dst, "file")); err != nil || !bytes.Equal(got, expected) {
				t.Fatal("incorrect destination bytes", err)
			}
			if _, err := remote.Lstat(path.Dir(stage)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("staging directory was not removed", err)
			}
		})
	}
}

func TestNewCopyUsesSourcePermissionsOnlyAtCommit(t *testing.T) {
	for _, scenario := range []struct {
		name string
		mode fs.FileMode
		want fs.FileMode
	}{
		{"index.html", 0664, 0644},
		{"deploy.sh", 0775 | fs.ModeSetuid | fs.ModeSetgid, 0755},
		{"private.txt", 0600, 0600},
	} {
		for _, download := range []bool{false, true} {
			direction := "upload"
			if download {
				direction = "download via resume"
			}
			t.Run(scenario.name+"/"+direction, func(t *testing.T) {
				localSource, sourcePath := sandbox(t)
				localDest, destPath := sandbox(t)
				data := bytes.Repeat([]byte("mode check\n"), 10000)
				fullPath := filepath.Join(sourcePath, scenario.name)
				if err := os.WriteFile(fullPath, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(fullPath, scenario.mode); err != nil {
					t.Fatal(err)
				}
				var source, dest FS = localSource, posixSFTPFixture(t, destPath)
				base, target := "/", "."
				if download {
					source, dest = posixSFTPFixture(t, sourcePath), localDest
					base, target = ".", "/"
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				items, total, err := Plan(ctx, source, base, []string{scenario.name})
				if err != nil {
					t.Fatal(err)
				}
				stage := copyStagePath(path.Join(target, scenario.name), "new-mode", 0)
				observed := false
				observe := func(progress Progress) {
					if observed || progress.Done == 0 || progress.Files != 0 {
						return
					}
					observed = true
					assertMode(t, dest, path.Dir(stage), fs.ModeDir|0700)
					assertMode(t, dest, stage, 0600)
					if download {
						cancel()
					}
				}
				if download {
					checkpoint := &CopyCheckpoint{}
					err = CopyResumable(ctx, source, dest, items, target, "new-mode", total, checkpoint, overwriteCopy, observe, func(string) {})
					if !errors.Is(err, context.Canceled) || checkpoint.File == nil || checkpoint.File.Offset <= 0 {
						t.Fatal("no paused download data", err)
					}
					err = CopyResumable(context.Background(), source, dest, items, target, "new-mode", total, checkpoint, overwriteCopy, func(Progress) {}, func(string) {})
				} else {
					err = Copy(ctx, source, dest, items, target, "new-mode", total, overwriteCopy, observe)
				}
				if err != nil || !observed {
					t.Fatal("copy did not finish with a private staging file", err)
				}
				assertMode(t, dest, path.Join(target, scenario.name), scenario.want)
				if read, err := os.ReadFile(filepath.Join(destPath, scenario.name)); err != nil || !bytes.Equal(read, data) {
					t.Fatal("incorrect destination bytes", err)
				}
			})
		}
	}
}

func TestResumeModeSFTP(t *testing.T) {
	for _, action := range []string{"resume", "abort"} {
		t.Run(action, func(t *testing.T) {
			source, src := sandbox(t)
			dst := t.TempDir()
			remote := posixSFTPFixture(t, dst)
			data := bytes.Repeat([]byte("0123456789"), 10000)
			if err := os.WriteFile(filepath.Join(src, "file"), data, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dst, "file"), []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(dst, "file"), 0640); err != nil {
				t.Fatal(err)
			}
			items, total, err := Plan(context.Background(), source, "/", []string{"file"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cp := &CopyCheckpoint{}
			err = CopyResumable(ctx, source, remote, items, ".", "resume-mode", total, cp, overwriteCopy, func(p Progress) {
				if p.Done > 0 {
					cancel()
				}
			}, func(string) {})
			if !errors.Is(err, context.Canceled) || cp.File == nil || cp.File.Offset == 0 || cp.File.Offset >= total {
				t.Fatal("missing paused stage", err)
			}
			stage := cp.File.Temporary
			assertMode(t, remote, path.Dir(stage), fs.ModeDir|0700)
			assertMode(t, remote, stage, 0600)
			assertMode(t, remote, "file", 0640)
			expected := []byte("old")
			if action == "abort" {
				err = cp.Abort(remote)
			} else {
				err = CopyResumable(context.Background(), source, remote, items, ".", "resume-mode", total, cp, overwriteCopy, func(Progress) {}, func(string) {})
				expected = data
			}
			if err != nil {
				t.Fatal(err)
			}
			assertMode(t, remote, "file", 0640)
			if got, err := os.ReadFile(filepath.Join(dst, "file")); err != nil || !bytes.Equal(got, expected) {
				t.Fatal("incorrect destination bytes", err)
			}
			if _, err := remote.Lstat(path.Dir(stage)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("staging directory was not removed", err)
			}
		})
	}
}

func assertMode(t *testing.T, f FS, p string, expected fs.FileMode) {
	t.Helper()
	info, err := f.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != expected {
		t.Fatalf("%s: mode %v, want %v", p, info.Mode(), expected)
	}
}

type faultyCopyMode struct {
	FS
	fail string
}

func (f faultyCopyMode) Chmod(p string, mode fs.FileMode) error {
	if f.fail == "chmod-failure" {
		return fs.ErrPermission
	}
	return f.FS.Chmod(p, mode)
}

func (f faultyCopyMode) Rename(string, string, bool) error { return fs.ErrPermission }

func (f faultyCopyMode) commitCopy(ctx context.Context, stage, target string, baseline FileVersion, sourceMode fs.FileMode) error {
	return commitModeCopy(ctx, f, stage, target, baseline, sourceMode)
}
