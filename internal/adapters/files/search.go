package files

import (
	"context"
	"io/fs"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

type SearchHit struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	Link      bool   `json:"link"`
}
type SearchProgress struct {
	Hits    []SearchHit `json:"hits"`
	Scanned int         `json:"scanned"`
	Skipped int         `json:"skipped"`
	Done    bool        `json:"done"`
	Reason  string      `json:"reason"`
	Code    string      `json:"code"`
	Message string      `json:"message"`
}

func SearchNames(ctx context.Context, f FS, base, query string, hidden bool, emit func(SearchProgress) error) error {
	return SearchNamesOptions(ctx, f, base, query, hidden, true, emit)
}
func SearchNamesOptions(ctx context.Context, f FS, base, query string, hidden, recursive bool, emit func(SearchProgress) error) error {
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > 128 || strings.ContainsAny(query, "\x00/\\") {
		return problem("请输入 1–128 字的文件名关键词，不包含路径分隔符。")
	}
	query = strings.ToLower(query)
	info, err := f.Lstat(base)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return problem("搜索范围应为普通目录。")
	}
	type directory struct {
		path  string
		depth int
	}
	stack := []directory{{base, 0}}
	progress := SearchProgress{Hits: []SearchHit{}}
	matched, bytes, pendingBytes := 0, 0, 0
	last := time.Now()
	flush := func(done bool, reason string) error {
		progress.Done, progress.Reason = done, reason
		if err := emit(progress); err != nil {
			return err
		}
		progress.Hits = []SearchHit{}
		pendingBytes = 0
		last = time.Now()
		return nil
	}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		current, e := f.Lstat(d.path)
		if e != nil || !current.IsDir() || current.Mode()&fs.ModeSymlink != 0 {
			progress.Skipped++
			continue
		}
		rows, e := f.ReadDir(d.path)
		if e != nil {
			if d.path == base {
				return e
			}
			progress.Skipped++
			continue
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			progress.Scanned++
			if progress.Scanned > 50000 {
				return flush(true, "scan-limit")
			}
			name := row.Name()
			if !Leaf(name) || !utf8.ValidString(name) {
				progress.Skipped++
				continue
			}
			if !hidden && strings.HasPrefix(name, ".") {
				continue
			}
			p := path.Join(d.path, name)
			if len(p) > 32768 {
				progress.Skipped++
				continue
			}
			if strings.Contains(strings.ToLower(name), query) {
				matched++
				bytes += len(p) + len(name) + 64
				if matched > 1000 || bytes > 2<<20 {
					return flush(true, "result-limit")
				}
				progress.Hits = append(progress.Hits, SearchHit{p, name, row.IsDir(), row.Mode()&fs.ModeSymlink != 0})
				pendingBytes += len(p) + len(name) + 64
			}
			if recursive && row.IsDir() && row.Mode()&fs.ModeSymlink == 0 {
				if d.depth >= 32 || len(stack) >= 2048 {
					progress.Skipped++
					progress.Reason = "depth-limit"
				} else {
					stack = append(stack, directory{p, d.depth + 1})
				}
			}
			if pendingBytes >= 32768 || len(progress.Hits) >= 32 || time.Since(last) >= 100*time.Millisecond {
				if err := flush(false, progress.Reason); err != nil {
					return err
				}
			}
		}
	}
	return flush(true, progress.Reason)
}
