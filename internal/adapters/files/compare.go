package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/pkg/sftp"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const MaxCompareFileBytes = 16 << 20

type CompareStamp struct {
	HasOwner                         bool
	UID, GID                         uint32
	Exists, Directory, Link, Scanned bool
	Size                             int64
	Modified                         int64
	Mode                             fs.FileMode
	Hash                             string
	Problem                          string
}
type CompareEntry struct {
	Relative    string       `json:"relative"`
	Status      string       `json:"status"`
	Message     string       `json:"message"`
	Directory   bool         `json:"directory"`
	LeftSize    int64        `json:"leftSize"`
	RightSize   int64        `json:"rightSize"`
	LeftExists  bool         `json:"leftExists"`
	RightExists bool         `json:"rightExists"`
	Left        CompareStamp `json:"-"`
	Right       CompareStamp `json:"-"`
}
type Comparison struct {
	Entries   []CompareEntry `json:"entries"`
	Directory bool           `json:"directory"`
	Recursive bool           `json:"recursive"`
	Complete  bool           `json:"complete"`
	Scanned   int            `json:"scanned"`
	Bytes     int64          `json:"bytes"`
	Message   string         `json:"message"`
}

func CompareFingerprint(ctx context.Context, f FS, p string) (CompareStamp, error) {
	info, err := f.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return CompareStamp{}, nil
	}
	if err != nil {
		return CompareStamp{}, err
	}
	return compareFingerprintFromInfo(ctx, f, p, info)
}

func stampFromInfo(info fs.FileInfo) CompareStamp {
	stamp := CompareStamp{Exists: true, Directory: info.IsDir(), Link: info.Mode()&fs.ModeSymlink != 0, Size: info.Size(), Modified: info.ModTime().UnixNano(), Mode: info.Mode()}
	compareOwner(&stamp, info)
	return stamp
}

func (s CompareStamp) matchesMetadata(other CompareStamp) bool {
	if s.Exists != other.Exists {
		return false
	}
	if !s.Exists {
		return true
	}
	if !s.sameOwner(other) {
		return false
	}
	if s.Directory || other.Directory {
		return s.Directory == other.Directory && !s.Link && !other.Link && s.Mode == other.Mode
	}
	return !s.Link && !other.Link && s.Size == other.Size && s.Modified == other.Modified && s.Mode == other.Mode
}

func compareFingerprintFromInfo(ctx context.Context, f FS, p string, info fs.FileInfo) (CompareStamp, error) {
	stamp := stampFromInfo(info)
	if stamp.Directory || stamp.Link {
		return stamp, nil
	}
	if !regular(info) || stamp.Size < 0 || stamp.Size > MaxCompareFileBytes {
		stamp.Problem = "文件类型或大小超出比较范围。"
		return stamp, nil
	}
	input, err := f.Open(p)
	if err != nil {
		return stamp, err
	}
	h := sha256.New()
	buffer := make([]byte, 32768)
	var n int64
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		count, e := input.Read(buffer)
		if count > 0 {
			n += int64(count)
			if n > MaxCompareFileBytes || n > stamp.Size {
				err = problem("文件在比较期间发生变化。")
				break
			}
			h.Write(buffer[:count])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			err = e
			break
		}
		if count == 0 {
			err = io.ErrNoProgress
			break
		}
	}
	closeErr := input.Close()
	if err != nil {
		return stamp, err
	}
	if closeErr != nil {
		return stamp, closeErr
	}
	after, err := f.Lstat(p)
	if err != nil {
		return stamp, err
	}
	afterOwner := CompareStamp{}
	compareOwner(&afterOwner, after)
	if !regular(after) || after.Size() != stamp.Size || after.ModTime().UnixNano() != stamp.Modified || after.Mode() != stamp.Mode || n != stamp.Size || !stamp.sameOwner(afterOwner) {
		return stamp, problem("文件在比较期间发生变化。")
	}
	stamp.Hash = hex.EncodeToString(h.Sum(nil))
	return stamp, nil
}
func compareOwner(stamp *CompareStamp, info fs.FileInfo) {
	if stat, ok := info.Sys().(*sftp.FileStat); ok {
		stamp.HasOwner = true
		stamp.UID, stamp.GID = stat.UID, stat.GID
	}
}
func (s CompareStamp) sameOwner(other CompareStamp) bool {
	return s.HasOwner == other.HasOwner && (!s.HasOwner || s.UID == other.UID && s.GID == other.GID)
}
func (s CompareStamp) Matches(other CompareStamp) bool {
	if s.Exists != other.Exists {
		return false
	}
	if !s.Exists {
		return true
	}
	if !s.sameOwner(other) {
		return false
	}
	if s.Directory || other.Directory {
		return s.Directory == other.Directory && !other.Link && s.Mode == other.Mode
	}
	return !s.Link && !other.Link && s.Problem == "" && other.Problem == "" && s.Hash != "" && s.Hash == other.Hash && s.Size == other.Size && s.Modified == other.Modified && s.Mode == other.Mode
}
func CompareTrees(ctx context.Context, left, right FS, leftPath, rightPath string, recursive, windowsNames bool, pulse func(int, int64)) (Comparison, error) {
	return compareTrees(ctx, left, right, leftPath, rightPath, recursive, windowsNames, pulse, nil)
}

type SyncScanCache [2]map[string]CompareStamp

func CompareTreesCached(ctx context.Context, left, right FS, leftPath, rightPath string, windowsNames bool, cache *SyncScanCache) (Comparison, error) {
	return compareTrees(ctx, left, right, leftPath, rightPath, true, windowsNames, func(int, int64) {}, cache)
}
func compareTrees(ctx context.Context, left, right FS, leftPath, rightPath string, recursive, windowsNames bool, pulse func(int, int64), cache *SyncScanCache) (Comparison, error) {
	result := Comparison{Entries: []CompareEntry{}, Recursive: recursive, Complete: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	li, e := left.Lstat(leftPath)
	if e != nil {
		return result, e
	}
	ri, e := right.Lstat(rightPath)
	if e != nil {
		return result, e
	}
	if li.Mode()&fs.ModeSymlink != 0 || ri.Mode()&fs.ModeSymlink != 0 || li.IsDir() != ri.IsDir() {
		return result, problem("请选择两个普通文件，或两个普通目录；根路径不跟随链接。")
	}
	result.Directory = li.IsDir()
	var budgetMu sync.Mutex
	totalPathBytes := 0
	last := time.Now()
	limited := false
	markLimited := func() {
		budgetMu.Lock()
		limited = true
		budgetMu.Unlock()
	}
	progress := func() {
		budgetMu.Lock()
		if time.Since(last) <= 100*time.Millisecond {
			budgetMu.Unlock()
			return
		}
		scanned, readBytes := result.Scanned, result.Bytes
		last = time.Now()
		budgetMu.Unlock()
		pulse(scanned, readBytes)
	}
	scanCtx, cancelScan := context.WithCancel(ctx)
	defer cancelScan()
	scan := func(f FS, base string, side int, rootInfo fs.FileInfo) (map[string]CompareStamp, error) {
		found := map[string]CompareStamp{}
		type node struct {
			relative string
			depth    int
			info     fs.FileInfo
		}
		stack := []node{{".", 0, rootInfo}}
		for len(stack) > 0 {
			if err := scanCtx.Err(); err != nil {
				return found, err
			}
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			p := base
			if n.relative != "." {
				p = path.Join(base, n.relative)
			}
			budgetMu.Lock()
			if result.Scanned >= 4000 || totalPathBytes > 2<<20 {
				limited = true
				budgetMu.Unlock()
				break
			}
			result.Scanned++
			totalPathBytes += len(p)
			bytesSoFar := result.Bytes
			budgetMu.Unlock()

			info := n.info
			var statErr error
			if info == nil {
				info, statErr = f.Lstat(p)
			}
			var stamp CompareStamp
			var err error
			if statErr != nil {
				err = statErr
			} else if regular(info) && info.Size() > 128<<20-bytesSoFar {
				stamp = stampFromInfo(info)
				stamp.Problem = "达到内容读取预算，请缩小比较范围。"
				markLimited()
			} else {
				meta := stampFromInfo(info)
				if cache != nil && regular(info) {
					if cached, ok := cache[side][n.relative]; ok && cached.Hash != "" && meta.matchesMetadata(cached) {
						stamp = cached
					}
				}
				if !stamp.Exists {
					stamp, err = compareFingerprintFromInfo(scanCtx, f, p, info)
				}
			}
			if err != nil {
				if scanCtx.Err() != nil {
					return found, scanCtx.Err()
				}
				stamp.Problem = "此项读取失败或在扫描期间变化。"
				stamp.Exists = true
			}
			overBudget := false
			if stamp.Hash != "" {
				budgetMu.Lock()
				result.Bytes += stamp.Size
				if result.Bytes > 128<<20 {
					limited = true
					overBudget = true
				}
				budgetMu.Unlock()
			}
			if overBudget {
				stamp.Hash = ""
				stamp.Problem = "达到内容读取预算，请缩小比较范围。"
			}
			if n.relative != "." || !result.Directory {
				found[n.relative] = stamp
			}
			if stamp.Directory && !stamp.Link && (n.relative == "." || recursive) {
				if n.depth >= 32 {
					markLimited()
					continue
				}
				rows, e := f.ReadDir(p)
				if e != nil {
					if n.relative == "." {
						return found, e
					}
					stamp.Problem = "子目录不可读。"
					found[n.relative] = stamp
					continue
				}
				stamp.Scanned = true
				if n.relative != "." {
					found[n.relative] = stamp
				}
				for i := len(rows) - 1; i >= 0; i-- {
					name := rows[i].Name()
					if !Leaf(name) || !utf8.ValidString(name) {
						markLimited()
						continue
					}
					if len(stack) >= 2048 {
						markLimited()
						break
					}
					stack = append(stack, node{path.Join(n.relative, name), n.depth + 1, rows[i]})
				}
			}
			progress()
			budgetMu.Lock()
			stopEarly := limited && result.Bytes > 128<<20
			budgetMu.Unlock()
			if stopEarly {
				break
			}
		}
		return found, nil
	}
	var a, b map[string]CompareStamp
	var errA, errB error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		a, errA = scan(left, leftPath, 0, li)
		if errA != nil {
			cancelScan()
		}
	}()
	go func() {
		defer wg.Done()
		b, errB = scan(right, rightPath, 1, ri)
		if errB != nil {
			cancelScan()
		}
	}()
	wg.Wait()
	if errA != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errA
	}
	if errB != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errB
	}
	names := map[string]bool{}
	for p := range a {
		names[p] = true
	}
	for p := range b {
		names[p] = true
	}
	macConflicts := macVolumeNameConflicts(left, leftPath, names)
	keys := []string{}
	for p := range names {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	windowsConflicts := map[string]bool{}
	if windowsNames {
		seen := make(map[string]string, len(keys))
		for _, p := range keys {
			folded := strings.ToLower(p)
			if previous, ok := seen[folded]; ok {
				windowsConflicts[previous], windowsConflicts[p] = true, true
			}
			seen[folded] = p
		}
		for _, p := range keys {
			if windowsConflicts[path.Dir(p)] || !WindowsRelativeName(p) {
				windowsConflicts[p] = true
			}
		}
	}
	for _, p := range keys {
		if len(result.Entries) >= 2000 {
			limited = true
			break
		}
		l, r := a[p], b[p]
		entry := CompareEntry{Relative: p, Left: l, Right: r, Directory: l.Directory || r.Directory, LeftSize: l.Size, RightSize: r.Size, LeftExists: l.Exists, RightExists: r.Exists}
		switch {
		case l.Problem != "" || r.Problem != "":
			entry.Status = "unverified"
			entry.Message = l.Problem
			if entry.Message == "" {
				entry.Message = r.Problem
			}
		case l.Link || r.Link:
			entry.Status = "unsupported"
			entry.Message = "链接不参与同步。"
		case windowsConflicts[p]:
			entry.Status = "name-conflict"
			entry.Message = "大小写或名称与 Windows 文件规则冲突。"
		case macConflicts[p]:
			entry.Status = "name-conflict"
			entry.Message = "这些名称在本地 macOS 卷上无法区分。"
		case l.Exists && r.Exists && l.Directory != r.Directory:
			entry.Status = "type-conflict"
			entry.Message = "两端类型不同，请手动处理。"
		case !l.Exists:
			entry.Status = "right-only"
		case !r.Exists:
			entry.Status = "left-only"
		case l.Directory:
			entry.Status = "directory"
			if !recursive {
				entry.Message = "未比较子目录。"
			}
		case l.Hash != "" && r.Hash != "" && l.Hash == r.Hash:
			entry.Status = "same"
		case l.Hash == "" || r.Hash == "":
			entry.Status = "unverified"
			entry.Message = "未能完整校验内容。"
		default:
			entry.Status = "different"
		}
		result.Entries = append(result.Entries, entry)
	}
	if limited {
		result.Complete = false
		result.Message = "比较达到扫描、层级或读取预算；结果不完整，请缩小范围后重新比较。"
	}
	if cache != nil && result.Complete {
		*cache = SyncScanCache{a, b}
	}
	pulse(result.Scanned, result.Bytes)
	return result, nil
}
func WindowsRelativeName(p string) bool {
	if p == "." {
		return true
	}
	for _, name := range strings.Split(p, "/") {
		if name == "" || strings.ContainsAny(name, "<>:\"|?*\\") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
			return false
		}
		for _, r := range name {
			if r < 32 {
				return false
			}
		}
		base, _, _ := strings.Cut(name, ".")
		stem := strings.ToUpper(strings.TrimRight(base, " "))
		switch stem {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT") {
			switch stem[3:] {
			case "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
				return false
			}
		}
	}
	return true
}
