package files

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
)

var ErrConflict = errors.New("transfer awaits conflict decision")

type ResumeProblem struct{ Message string }

func (p *ResumeProblem) Error() string   { return p.Message }
func resumeProblem(message string) error { return &ResumeProblem{message} }

type ResumeHandle interface {
	io.ReadWriteSeeker
	io.Closer
	Stat() (fs.FileInfo, error)
	Truncate(int64) error
}
type ResumeFS interface {
	FS
	OpenResume(string) (ResumeHandle, error)
}

func (f Local) OpenResume(p string) (ResumeHandle, error) {
	return f.openPrivateResume(p)
}
func (f Remote) OpenResume(p string) (ResumeHandle, error) {
	handle, err := f.Client.OpenFile(p, os.O_RDWR)
	if err != nil {
		return nil, f.spaceError(p, err)
	}
	return remoteWriter{File: handle, remote: f, path: p}, nil
}

type CopyCheckpoint struct {
	Index           int
	Progress        Progress
	Renamed         map[string]string
	Skipped         []string
	SkippedBytes    int64
	File            *PartialFile
	ConflictVersion FileVersion `json:"-"`
}

func (cp *CopyCheckpoint) DestinationRelative(relative string) string {
	longest := ""
	for prefix := range cp.Renamed {
		if (relative == prefix || strings.HasPrefix(relative, prefix+"/")) && len(prefix) > len(longest) {
			longest = prefix
		}
	}
	if longest != "" {
		return cp.Renamed[longest] + strings.TrimPrefix(relative, longest)
	}
	return relative
}

type PartialFile struct {
	Temporary, Destination string
	Overwrite              bool
	Existing               FileVersion
	Offset                 int64
	Digest                 hash.Hash `json:"-"`
	ExpectedDigest         []byte    `json:"digest,omitempty"`
	Created                bool
	Resumed                bool
}
type FileVersion struct {
	Exists   bool
	Size     int64
	Modified time.Time
	Mode     fs.FileMode
}

func version(info fs.FileInfo, err error) (FileVersion, error) {
	if errors.Is(err, fs.ErrNotExist) {
		return FileVersion{}, nil
	}
	if err != nil {
		return FileVersion{}, err
	}
	return FileVersion{true, info.Size(), info.ModTime(), info.Mode()}, nil
}
func (v FileVersion) Matches(other FileVersion) bool {
	return v.Exists == other.Exists && (!v.Exists || v.Size == other.Size && v.Modified.Equal(other.Modified) && v.Mode == other.Mode)
}
func regular(info fs.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode()&fs.ModeSymlink == 0
}

func CopyResumable(ctx context.Context, source, dest FS, items []Item, target, nonce string, total int64, cp *CopyCheckpoint, conflict Conflict, progress func(Progress), phase func(string)) error {
	if cp.Renamed == nil {
		cp.Renamed = map[string]string{}
	}
	cp.Progress.Total = total - cp.SkippedBytes
	emit := func() { progress(cp.Progress) }
	for cp.Index < len(items) {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := items[cp.Index]
		cp.Progress.Current = item.Relative
		ignore := false
		for _, prefix := range cp.Skipped {
			if item.Relative == prefix || strings.HasPrefix(item.Relative, prefix+"/") {
				ignore = true
				break
			}
		}
		if ignore {
			cp.Progress.Skipped++
			if !item.Directory {
				cp.SkippedBytes += item.Size
			}
			cp.Progress.Total = total - cp.SkippedBytes
			cp.Index++
			emit()
			continue
		}
		relative := cp.DestinationRelative(item.Relative)
		destination := path.Join(target, relative)
		if cp.File == nil {
			before, err := source.Lstat(item.Source)
			if err != nil {
				return err
			}
			if before.Mode()&fs.ModeSymlink != 0 || before.IsDir() != item.Directory || (!item.Directory && (!regular(before) || before.Size() != item.Size || !before.ModTime().Equal(item.Modified))) {
				return resumeProblem("源文件已变化，请重新传输。")
			}
			existing, err := dest.Lstat(destination)
			baseline, err := version(existing, err)
			if err != nil {
				return err
			}
			overwrite := false
			if baseline.Exists {
				cp.ConflictVersion = baseline
				choice, err := conflict(item.Relative, item.Directory)
				if err != nil {
					return err
				}
				switch choice {
				case "skip":
					if item.Directory {
						cp.Skipped = append(cp.Skipped, item.Relative)
					} else {
						cp.SkippedBytes += item.Size
					}
					cp.Progress.Skipped++
					cp.Progress.Total = total - cp.SkippedBytes
					cp.Index++
					emit()
					continue
				case "overwrite":
					if (item.Directory && !existing.IsDir()) || (!item.Directory && !regular(existing)) || existing.Mode()&fs.ModeSymlink != 0 {
						return resumeProblem("目标类型已变化或为符号链接，请重新传输并保留两者。")
					}
					overwrite = true
				case "keepBoth":
					base, ext := path.Base(destination), path.Ext(destination)
					if item.Directory {
						ext = ""
					}
					stem := strings.TrimSuffix(base, ext)
					found := false
					for n := 2; n < 10000; n++ {
						candidate := path.Join(path.Dir(destination), fmt.Sprintf("%s (%d)%s", stem, n, ext))
						if _, err = dest.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
							destination = candidate
							found = true
							break
						} else if err != nil {
							return err
						}
					}
					if !found {
						return problem("无法找到可用副本名称。")
					}
					baseline = FileVersion{}
					if item.Directory {
						cp.Renamed[item.Relative] = strings.TrimPrefix(strings.TrimPrefix(destination, target), "/")
					}
				default:
					return problem("请选择覆盖、跳过或保留两者。")
				}
			}
			if item.Directory {
				if !overwrite {
					if err := dest.Mkdir(destination); err != nil {
						return err
					}
				}
				cp.Index++
				emit()
				continue
			}
			cp.File = &PartialFile{Temporary: copyStagePath(destination, nonce, cp.Index), Destination: destination, Overwrite: overwrite, Existing: baseline, Digest: sha256.New()}
		}
		if err := continueFile(ctx, source, dest, item, cp, emit, phase); err != nil {
			return err
		}
		_ = removeCopyStage(dest, cp.File.Temporary)
		cp.File = nil
		cp.Progress.Files++
		cp.Index++
		emit()
	}
	return nil
}

func checkedSource(source FS, item Item) (fs.FileInfo, error) {
	info, err := source.Lstat(item.Source)
	if err != nil {
		return nil, err
	}
	if !regular(info) || info.Size() != item.Size || !info.ModTime().Equal(item.Modified) {
		return nil, resumeProblem("源文件已变化，不能继续；请重新传输。")
	}
	return info, nil
}
func checkDestination(dest FS, p *PartialFile) error {
	current, err := version(dest.Lstat(p.Destination))
	if err != nil {
		return err
	}
	if !p.Existing.Matches(current) {
		return resumeProblem("目标文件在暂停或传输期间发生变化，原文件已保留；请重新传输并确认冲突。")
	}
	return nil
}
func checkParent(dest FS, p string) error {
	info, err := dest.Lstat(path.Dir(p))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return resumeProblem("目标目录已变化或为符号链接，不能继续。")
	}
	return nil
}
func hashPrefix(ctx context.Context, input io.Reader, n int64, pulse func()) (hash.Hash, error) {
	h := sha256.New()
	buffer := make([]byte, 32768)
	for n > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count, err := io.ReadFull(input, buffer[:min(n, int64(len(buffer)))])
		if count > 0 {
			_, _ = h.Write(buffer[:count])
			n -= int64(count)
			pulse()
		}
		if err != nil {
			return nil, err
		}
	}
	return h, nil
}
func equalDigest(a, b hash.Hash) bool { return string(a.Sum(nil)) == string(b.Sum(nil)) }

func continueFile(ctx context.Context, source, dest FS, item Item, cp *CopyCheckpoint, pulse func(), phase func(string)) (result error) {
	p := cp.File
	if _, err := checkedSource(source, item); err != nil {
		return err
	}
	if err := checkParent(dest, p.Destination); err != nil {
		return err
	}
	if err := checkDestination(dest, p); err != nil {
		return err
	}
	input, err := source.Open(item.Source)
	if err != nil {
		return err
	}
	defer input.Close()
	if opened, ok := input.(interface{ Stat() (fs.FileInfo, error) }); ok {
		info, e := opened.Stat()
		if e != nil {
			return e
		}
		if !regular(info) || info.Size() != item.Size || !info.ModTime().Equal(item.Modified) {
			return resumeProblem("源文件在打开期间变化，不能继续。")
		}
	}
	var output io.WriteCloser
	if !p.Created {
		output, err = createCopyStage(dest, p.Temporary, item.Size)
		if err != nil {
			return err
		}
		p.Created = true
	} else {
		phase("checking")
		if !copyStageLayout(p.Temporary) || dest.privateDir(path.Dir(p.Temporary), false) != nil {
			return resumeProblem(privateStageError)
		}
		info, err := dest.Lstat(p.Temporary)
		if err != nil {
			return resumeProblem("续传临时文件不可用，请重新传输。")
		}
		if !regular(info) || info.Size() < p.Offset || info.Size() > item.Size {
			return resumeProblem("临时文件类型或长度已变化，不能继续。")
		}
		resumable, ok := dest.(ResumeFS)
		if !ok {
			return resumeProblem("目标不支持安全续传，请重新传输。")
		}
		handle, err := resumable.OpenResume(p.Temporary)
		if err != nil {
			return err
		}
		defer handle.Close()
		opened, err := handle.Stat()
		if err != nil {
			return err
		}
		if !regular(opened) || opened.Size() != info.Size() || !opened.ModTime().Equal(info.ModTime()) {
			return resumeProblem("临时文件在打开期间变化，不能继续。")
		}
		sourceHash, err := hashPrefix(ctx, input, p.Offset, pulse)
		if err != nil {
			return err
		}
		stageHash, err := hashPrefix(ctx, handle, p.Offset, pulse)
		if err != nil {
			return err
		}
		expected := p.ExpectedDigest
		if p.Digest != nil {
			expected = p.Digest.Sum(nil)
		}
		if len(expected) != sha256.Size || string(sourceHash.Sum(nil)) != string(expected) || string(stageHash.Sum(nil)) != string(expected) {
			return resumeProblem("源文件或临时文件的已传内容不一致，不能续传；请重新传输。")
		}
		if _, err = checkedSource(source, item); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = handle.Truncate(p.Offset); err != nil {
			return err
		}
		if _, err = handle.Seek(p.Offset, io.SeekStart); err != nil {
			return err
		}
		p.Digest = sourceHash
		p.ExpectedDigest = nil
		p.Resumed = true
		output = handle
	}
	defer output.Close()
	phase("transferring")
	if err = streamWrite(ctx, input, output, dest, p.Offset, item.Size, false, func(data []byte) {
		_, _ = p.Digest.Write(data)
		p.Offset += int64(len(data))
		cp.Progress.Done += int64(len(data))
		pulse()
	}); err != nil {
		return err
	}
	if err = input.Close(); err != nil {
		return err
	}
	if local, ok := output.(*os.File); ok {
		if err = local.Sync(); err != nil {
			return err
		}
	}
	if err = output.Close(); err != nil {
		return err
	}
	phase("checking")
	if p.Resumed {
		info, err := dest.Lstat(p.Temporary)
		if err != nil {
			return err
		}
		if !regular(info) || info.Size() != item.Size {
			return resumeProblem("临时文件在校验前发生变化，请重新传输。")
		}
		stage, err := dest.Open(p.Temporary)
		if err != nil {
			return err
		}
		digest, err := hashPrefix(ctx, stage, item.Size, pulse)
		closeErr := stage.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if !equalDigest(digest, p.Digest) {
			return resumeProblem("续传文件完整性校验失败，原目标已保留；请重新传输。")
		}
	}
	after, err := checkedSource(source, item)
	if err != nil {
		return err
	}
	if err = checkParent(dest, p.Destination); err != nil {
		return err
	}
	if err = checkDestination(dest, p); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	phase("committing")
	return dest.commitCopy(ctx, p.Temporary, p.Destination, p.Existing, after.Mode())
}

func (cp *CopyCheckpoint) Abort(dest FS) error {
	if cp.File == nil || !cp.File.Created {
		return nil
	}
	err := removeCopyStage(dest, cp.File.Temporary)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
