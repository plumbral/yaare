package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/pkg/sftp"
	"hash"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

type SyncPreview struct {
	Path      string `json:"path"`
	Action    string `json:"action"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
}
type CopyReview struct {
	Source, Destination           string
	SourceStamp, DestinationStamp CompareStamp
}
type ReviewedSync struct {
	Items           []Item
	Reviews         map[string]CopyReview
	Preview         []SyncPreview
	Total           int64
	DestinationRoot string
	Retained        int
	created         map[string]bool
	mu              sync.Mutex
	verified        map[string]string
}

func (p *ReviewedSync) markVerified(sourcePath, digest string) {
	p.mu.Lock()
	if p.verified == nil {
		p.verified = map[string]string{}
	}
	p.verified[sourcePath] = digest
	p.mu.Unlock()
}

func (p *ReviewedSync) isVerified(sourcePath, digest string) bool {
	if digest == "" {
		return false
	}
	p.mu.Lock()
	got := p.verified[sourcePath]
	p.mu.Unlock()
	return got == digest
}

func (p *ReviewedSync) clearVerified(sourcePath string) {
	p.mu.Lock()
	delete(p.verified, sourcePath)
	p.mu.Unlock()
}

func RestoreReviewedSync(data []byte) (*ReviewedSync, error) {
	var plan ReviewedSync
	if len(data) > 8<<20 || json.Unmarshal(data, &plan) != nil || len(plan.Items) == 0 || len(plan.Items) > 2000 || len(plan.Reviews) > 2000 || plan.Total < 0 || plan.Total > 128<<20 {
		return nil, problem("比较复制记录无效，请重新比较。")
	}
	plan.created = map[string]bool{}
	return &plan, nil
}

func BuildReviewedSync(comparison Comparison, leftRoot, rightRoot, direction string, selected []string) (*ReviewedSync, error) {
	if !comparison.Complete {
		return nil, problem("比较结果不完整，请缩小范围后重新比较。")
	}
	if direction != "upload" && direction != "download" || len(selected) == 0 || len(selected) > 256 {
		return nil, problem("请选择同步方向和 1–256 项。")
	}
	byPath := map[string]CompareEntry{}
	for _, e := range comparison.Entries {
		byPath[e.Relative] = e
	}
	chosen := map[string]bool{}
	for _, p := range selected {
		entry, ok := byPath[p]
		if !ok {
			return nil, problem("所选比较结果已失效。")
		}
		chosen[p] = true
		if entry.Directory {
			source := entry.Left
			if direction == "download" {
				source = entry.Right
			}
			if !comparison.Recursive || !source.Scanned {
				return nil, problem("请选择文件，或开启子目录比较后再选择目录。")
			}
			for q := range byPath {
				if strings.HasPrefix(q, p+"/") {
					chosen[q] = true
				}
			}
		}
	}
	for p := range chosen {
		for parent := path.Dir(p); parent != "." && parent != "/"; parent = path.Dir(parent) {
			if _, ok := byPath[parent]; !ok {
				return nil, problem("缺少父目录比较结果，请重新比较。")
			}
			chosen[parent] = true
		}
	}
	keys := []string{}
	for p := range chosen {
		keys = append(keys, p)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := strings.Count(keys[i], "/"), strings.Count(keys[j], "/")
		if a == b {
			return keys[i] < keys[j]
		}
		return a < b
	})
	sourceRoot, targetRoot := leftRoot, rightRoot
	if direction == "download" {
		sourceRoot, targetRoot = rightRoot, leftRoot
	}
	plan := &ReviewedSync{Reviews: map[string]CopyReview{}, Preview: []SyncPreview{}, Items: []Item{}, DestinationRoot: targetRoot, created: map[string]bool{}}
	for _, relative := range keys {
		entry := byPath[relative]
		src, dst := entry.Left, entry.Right
		if direction == "download" {
			src, dst = dst, src
		}
		if !src.Exists {
			plan.Retained++
			continue
		}
		if entry.Status == "same" {
			continue
		}
		if entry.Status == "unsupported" || entry.Status == "unverified" || entry.Status == "name-conflict" || entry.Status == "type-conflict" || src.Link || dst.Link || !src.Directory && src.Hash == "" {
			return nil, problem("所选项包含未校验、链接或名称/类型冲突，请先处理后重新比较。")
		}
		from, to, rel := path.Join(sourceRoot, relative), path.Join(targetRoot, relative), relative
		if !comparison.Directory {
			from, to, rel = sourceRoot, targetRoot, path.Base(targetRoot)
			plan.DestinationRoot = path.Dir(targetRoot)
		}
		plan.Reviews[to] = CopyReview{from, to, src, dst}
		plan.Items = append(plan.Items, Item{Source: from, Relative: rel, Directory: src.Directory, Size: src.Size, Modified: timeFromStamp(src)})
		if !src.Directory {
			plan.Total += src.Size
		}
		if !src.Directory || !dst.Exists {
			action := "create"
			if dst.Exists {
				action = "overwrite"
			}
			plan.Preview = append(plan.Preview, SyncPreview{relative, action, src.Directory, src.Size})
		}
	}
	if len(plan.Preview) == 0 {
		return nil, problem("所选项在此方向没有需要复制的差异；仅在目标端的文件会保留。")
	}
	if len(plan.Items) > 2000 || plan.Total > 128<<20 {
		return nil, problem("本次复制超出比较预算，请缩小选择范围。")
	}
	return plan, nil
}
func timeFromStamp(s CompareStamp) time.Time { return time.Unix(0, s.Modified) }
func (p *ReviewedSync) CheckMetadata(ctx context.Context, source, dest FS) error {
	for _, r := range p.Reviews {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkedParents(source, r.Source); err != nil {
			return err
		}
		if err := p.destinationParents(dest, r.Destination); err != nil {
			return err
		}
		for _, x := range []struct {
			f     FS
			path  string
			stamp CompareStamp
		}{{source, r.Source, r.SourceStamp}, {dest, r.Destination, r.DestinationStamp}} {
			info, e := x.f.Lstat(x.path)
			var actual CompareStamp
			if errors.Is(e, fs.ErrNotExist) {
				actual = CompareStamp{}
			} else if e != nil {
				return e
			} else {
				actual = stampFromInfo(info)
			}
			if !x.stamp.matchesMetadata(actual) {
				return resumeProblem("文件在预览后发生变化，请重新比较并确认。")
			}
		}
	}
	return nil
}

func (p *ReviewedSync) Check(ctx context.Context, source, dest FS) error {
	for _, r := range p.Reviews {
		if err := checkedParents(source, r.Source); err != nil {
			return err
		}
		if err := p.destinationParents(dest, r.Destination); err != nil {
			return err
		}
		for _, x := range []struct {
			f     FS
			path  string
			stamp CompareStamp
		}{{source, r.Source, r.SourceStamp}, {dest, r.Destination, r.DestinationStamp}} {
			actual, e := CompareFingerprint(ctx, x.f, x.path)
			if e != nil {
				return e
			}
			if !x.stamp.Matches(actual) {
				return resumeProblem("文件在预览后发生变化，请重新比较并确认。")
			}
		}
	}
	return nil
}
func (p *ReviewedSync) destinationParents(f FS, target string) error {
	for i := 0; ; i++ {
		if i > 64 {
			return problem("路径层级过深。")
		}
		target = path.Dir(target)
		info, e := f.Lstat(target)
		if errors.Is(e, fs.ErrNotExist) {
			r, ok := p.Reviews[target]
			if !ok || !r.SourceStamp.Directory || r.DestinationStamp.Exists {
				return e
			}
		} else if e != nil {
			return e
		} else if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return resumeProblem("目标父目录为链接或类型已变化。")
		}
		if target == "/" || target == "." {
			return nil
		}
	}
}
func checkedParents(f FS, p string) error {
	for i := 0; ; i++ {
		if i > 64 {
			return problem("路径层级过深。")
		}
		p = path.Dir(p)
		info, e := f.Lstat(p)
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return resumeProblem("父目录已变化或为链接，请重新比较。")
		}
		if p == "/" || p == "." {
			return nil
		}
	}
}
func (p *ReviewedSync) Wrap(ctx context.Context, source, dest FS) (FS, FS) {
	return reviewedSource{source, ctx, p}, reviewedDestination{dest, source, ctx, p}
}

type reviewedSource struct {
	FS
	ctx  context.Context
	plan *ReviewedSync
}

type reviewedReader struct {
	io.ReadCloser
	fs       FS
	path     string
	expected CompareStamp
	plan     *ReviewedSync
	hash     hash.Hash
	n        int64
	closed   bool
}

func (r *reviewedReader) Stat() (fs.FileInfo, error) {
	if s, ok := r.ReadCloser.(interface{ Stat() (fs.FileInfo, error) }); ok {
		return s.Stat()
	}
	return r.fs.Lstat(r.path)
}

func (r *reviewedReader) recordChunk(data []byte) {
	if len(data) == 0 {
		return
	}
	_, _ = r.hash.Write(data)
	r.n += int64(len(data))
}

func (r *reviewedReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.recordChunk(p[:n])
	}
	return n, err
}

func (r *reviewedReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	closeErr := r.ReadCloser.Close()
	if closeErr != nil {
		return closeErr
	}
	if r.n == r.expected.Size {
		if hex.EncodeToString(r.hash.Sum(nil)) != r.expected.Hash {
			return resumeProblem("来源内容已变化，请重新比较。")
		}
		after, err := r.fs.Lstat(r.path)
		if err != nil {
			return err
		}
		if !r.expected.matchesMetadata(stampFromInfo(after)) {
			return resumeProblem("来源内容已变化，请重新比较。")
		}
		r.plan.markVerified(r.path, r.expected.Hash)
	}
	return nil
}

func (f reviewedSource) Open(p string) (io.ReadCloser, error) {
	var expected *CompareStamp
	for _, r := range f.plan.Reviews {
		if r.Source == p {
			copy := r.SourceStamp
			expected = &copy
			break
		}
	}
	if expected == nil {
		return nil, fs.ErrPermission
	}
	f.plan.clearVerified(p)
	if e := checkedParents(f.FS, p); e != nil {
		return nil, e
	}
	info, e := f.FS.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !expected.matchesMetadata(stampFromInfo(info)) {
		return nil, resumeProblem("来源内容已变化，请重新比较。")
	}
	inner, e := f.FS.Open(p)
	if e != nil {
		return nil, e
	}
	return &reviewedReader{
		ReadCloser: inner,
		fs:         f.FS,
		path:       p,
		expected:   *expected,
		plan:       f.plan,
		hash:       sha256.New(),
	}, nil
}

type reviewedDestination struct {
	FS
	source FS
	ctx    context.Context
	plan   *ReviewedSync
}

func (f reviewedDestination) Mkdir(p string) error {
	r, ok := f.plan.Reviews[p]
	if !ok || !r.SourceStamp.Directory || r.DestinationStamp.Exists {
		return fs.ErrPermission
	}
	if e := checkedParents(f.FS, p); e != nil {
		return e
	}
	now, e := CompareFingerprint(f.ctx, f.FS, p)
	if e != nil {
		return e
	}
	if now.Exists {
		return resumeProblem("目标目录在预览后出现，请重新比较。")
	}
	if e = f.FS.Mkdir(p); e == nil {
		f.plan.created[p] = true
	}
	return e
}
func (f reviewedDestination) commitCopy(ctx context.Context, temporary, target string, baseline FileVersion, sourceMode fs.FileMode) error {
	r, ok := f.plan.Reviews[target]
	if !ok || r.SourceStamp.Directory {
		return fs.ErrPermission
	}
	if r.DestinationStamp.Exists {
		if r.DestinationStamp.Mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
			return problem("目标带特殊权限，请手动处理；原文件已保留。")
		}
	}
	if e := checkedParents(f.FS, target); e != nil {
		return e
	}
	if e := checkedParents(f.source, r.Source); e != nil {
		return e
	}
	streamVerified := f.plan.isVerified(r.Source, r.SourceStamp.Hash)
	if streamVerified {
		info, e := f.source.Lstat(r.Source)
		if e != nil {
			return e
		}
		if !r.SourceStamp.matchesMetadata(stampFromInfo(info)) {
			return resumeProblem("来源、目标或临时内容与已确认预览不同，原目标已保留；请重新比较。")
		}
	} else {
		src, e := CompareFingerprint(f.ctx, f.source, r.Source)
		if e != nil {
			return e
		}
		if !r.SourceStamp.Matches(src) {
			return resumeProblem("来源、目标或临时内容与已确认预览不同，原目标已保留；请重新比较。")
		}
	}
	if !r.DestinationStamp.Exists {
		if _, e := f.FS.Lstat(target); e == nil {
			return resumeProblem("来源、目标或临时内容与已确认预览不同，原目标已保留；请重新比较。")
		} else if !errors.Is(e, fs.ErrNotExist) {
			return e
		}
	} else {
		dst, e := CompareFingerprint(f.ctx, f.FS, target)
		if e != nil {
			return e
		}
		if !r.DestinationStamp.Matches(dst) {
			return resumeProblem("来源、目标或临时内容与已确认预览不同，原目标已保留；请重新比较。")
		}
	}
	if !streamVerified || localCopyTarget(f.FS) {
		stage, e := CompareFingerprint(f.ctx, f.FS, temporary)
		if e != nil {
			return e
		}
		if stage.Hash == "" || stage.Hash != r.SourceStamp.Hash {
			return resumeProblem("来源、目标或临时内容与已确认预览不同，原目标已保留；请重新比较。")
		}
	} else {
		info, e := f.FS.Lstat(temporary)
		if e != nil {
			return e
		}
		if !regular(info) || info.Size() != r.SourceStamp.Size {
			return resumeProblem("来源、目标或临时内容与已确认预览不同，原目标已保留；请重新比较。")
		}
	}
	if remote, ok := f.FS.(Remote); ok && r.DestinationStamp.Exists && r.DestinationStamp.HasOwner {
		info, e := remote.Lstat(temporary)
		if e != nil {
			return e
		}
		if stat, ok := info.Sys().(*sftp.FileStat); !ok {
			return problem("无法确认临时文件所有者，原文件已保留。")
		} else if stat.UID != r.DestinationStamp.UID || stat.GID != r.DestinationStamp.GID {
			if e = remote.Client.Chown(temporary, int(r.DestinationStamp.UID), int(r.DestinationStamp.GID)); e != nil {
				return problem("无法保留目标所有者或用户组，原文件已保留。")
			}
		}
	}
	return f.FS.commitCopy(ctx, temporary, target, baseline, sourceMode)
}

func (f reviewedDestination) OpenResume(p string) (ResumeHandle, error) {
	base, ok := f.FS.(ResumeFS)
	if !ok {
		return nil, errors.New("resume unavailable")
	}
	return base.OpenResume(p)
}

var _ ResumeFS = reviewedDestination{}
