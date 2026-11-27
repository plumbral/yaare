//go:build !windows

package files

import (
	"context"
	"io"
	"io/fs"
	"os"
)

func (f Local) createPrivate(p string) (io.WriteCloser, error) {
	return f.Root.OpenFile(localPath(p), os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateCopyMode)
}

func (f Local) openPrivateResume(p string) (ResumeHandle, error) {
	return f.Root.OpenFile(localPath(p), os.O_RDWR, 0)
}

func (f Local) privateDir(p string, create bool) error {
	if create {
		if err := f.Root.Mkdir(localPath(p), 0700); err != nil {
			return err
		}
	}
	err := verifyPrivateDir(f, p)
	if err != nil && create {
		_ = f.Remove(p, true)
	}
	return err
}

func (f Local) commitCopy(ctx context.Context, stage, target string, baseline FileVersion, sourceMode fs.FileMode) error {
	return commitModeCopy(ctx, f, stage, target, baseline, sourceMode)
}
