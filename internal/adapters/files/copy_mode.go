package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

const privateCopyMode fs.FileMode = 0600
const newCopyModeMask fs.FileMode = 0755
const privateStageError = "无法确认传输暂存区为私有，未写入文件。"
const copyModeError = "无法安全保留目标文件权限，原文件已保留。"

func copyStagePath(destination, nonce string, index int) string {
	return path.Join(path.Dir(destination), fmt.Sprintf(".yaare-%s-%d.part", nonce, index), "payload")
}

func copyStageLayout(p string) bool {
	name := path.Base(path.Dir(p))
	return path.Base(p) == "payload" && strings.HasPrefix(name, ".yaare-") && strings.HasSuffix(name, ".part")
}

func createCopyStage(f FS, p string, size int64) (io.WriteCloser, error) {
	if !copyStageLayout(p) {
		return nil, problem(privateStageError)
	}
	if err := f.privateDir(path.Dir(p), true); err != nil {
		return nil, err
	}
	output, err := createSized(f, p, size)
	if err != nil {
		_ = f.Remove(path.Dir(p), true)
	}
	return output, err
}

func removeCopyStage(f FS, p string) error {
	if err := f.Remove(p, false); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if copyStageLayout(p) {
		if err := f.Remove(path.Dir(p), true); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func verifyPrivateDir(f FS, p string) error {
	info, err := f.Lstat(p)
	if err != nil || info.Mode().Type() != fs.ModeDir || info.Mode().Perm() != 0700 {
		return problem(privateStageError)
	}
	return nil
}

func createRemotePrivateDir(f FS, p string) error {
	if err := f.Mkdir(p); err != nil {
		return err
	}
	if verifyPrivateDir(f, p) == nil {
		return nil
	}
	_ = f.Chmod(p, 0700)
	if err := verifyPrivateDir(f, p); err != nil {
		_ = f.Remove(p, true)
		return err
	}
	return nil
}

func (f Remote) privateDir(p string, create bool) error {
	if create {
		return createRemotePrivateDir(f, p)
	}
	return verifyPrivateDir(f, p)
}

func transferPayloadMode(mode fs.FileMode) bool {
	return mode.IsRegular() && mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) == 0 && mode.Perm()&0600 == 0600
}

func copyDestinationUnchanged(f FS, target string, baseline FileVersion) error {
	current, err := version(f.Lstat(target))
	if err != nil {
		return err
	}
	if !baseline.Matches(current) || current.Exists && current.Mode.Type() != 0 {
		return resumeProblem("目标文件在暂停或传输期间发生变化，原文件已保留；请重新传输并确认冲突。")
	}
	return nil
}

func commitModeCopy(ctx context.Context, f FS, stage, target string, baseline FileVersion, sourceMode fs.FileMode) error {
	if err := f.privateDir(path.Dir(stage), false); err != nil {
		return err
	}
	if err := copyDestinationUnchanged(f, target, baseline); err != nil {
		return err
	}
	mode := sourceMode.Perm() & newCopyModeMask
	if baseline.Exists {
		mode = baseline.Mode.Perm()
	}
	info, err := f.Lstat(stage)
	if err != nil || !regular(info) {
		return problem(privateStageError)
	}
	if info.Mode() != mode {
		if err = f.Chmod(stage, mode); err != nil {
			return problem(copyModeError)
		}
		if info, err = f.Lstat(stage); err != nil || info.Mode() != mode {
			return problem(copyModeError)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.Rename(stage, target, baseline.Exists)
}

func (f Remote) commitCopy(ctx context.Context, stage, target string, baseline FileVersion, sourceMode fs.FileMode) error {
	return commitModeCopy(ctx, f, stage, target, baseline, sourceMode)
}
