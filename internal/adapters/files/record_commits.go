package files

import (
	"context"
	"io/fs"
)

func RecordCommits(target FS, record func(string, bool)) FS { return recordingFS{target, record} }

type recordingFS struct {
	FS
	record func(string, bool)
}

func (f recordingFS) Mkdir(p string) error {
	e := f.FS.Mkdir(p)
	if e == nil {
		f.record(p, true)
	}
	return e
}
func (f recordingFS) commitCopy(ctx context.Context, stage, target string, baseline FileVersion, mode fs.FileMode) error {
	e := f.FS.commitCopy(ctx, stage, target, baseline, mode)
	if e == nil {
		f.record(target, false)
	}
	return e
}
func (f recordingFS) OpenResume(p string) (ResumeHandle, error) {
	base, ok := f.FS.(ResumeFS)
	if !ok {
		return nil, problem("目标不支持继续传输。")
	}
	return base.OpenResume(p)
}
