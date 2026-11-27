package sessionlog

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

const maxIndexBytes = 8 << 10
const maxLineBytes = 64 << 10

func openRegular(path string, limit int64) (*os.File, int64, error) {
	prior, err := os.Lstat(path)
	if err != nil || !prior.Mode().IsRegular() || prior.Size() > limit {
		return nil, 0, errors.New("日志文件不可用。")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, errors.New("日志文件不可用。")
	}
	info, err := file.Stat()
	if err != nil || !os.SameFile(prior, info) || info.Size() > limit {
		file.Close()
		return nil, 0, errors.New("日志文件不可用。")
	}
	return file, info.Size(), nil
}

func loadIndex(path, id string) (Info, bool) {
	file, _, err := openRegular(path, maxIndexBytes)
	if err != nil {
		return Info{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxIndexBytes+1))
	var info Info
	if err != nil || len(data) > maxIndexBytes || !utf8.Valid(data) || json.Unmarshal(data, &info) != nil ||
		info.ID != id || !identifier.MatchString(info.SessionID) || info.Attempt < 1 || info.Started <= 0 {
		return Info{}, false
	}
	switch info.State {
	case "recording", "stopped", "limited":
		return info, true
	default:
		return Info{}, false
	}
}

func writeIndex(path string, info Info) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if len(data) > maxIndexBytes {
		return errors.New("日志索引超过大小限制。")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".index-*.tmp")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceIndex(temp, path)
}

type logReader struct {
	reader    *bufio.Reader
	offset    int64
	lineStart int64
	line      Line
	err       error
}

func newLogReader(file io.Reader, startOffset, limit int64) *logReader {
	if limit < 0 {
		limit = 0
	}
	return &logReader{
		reader: bufio.NewReaderSize(io.LimitReader(file, limit), maxLineBytes),
		offset: startOffset,
	}
}

func (r *logReader) next(ctx context.Context) bool {
	oversized := false
	lineStart := r.offset
	for {
		if r.err = ctx.Err(); r.err != nil {
			return false
		}
		data, err := r.reader.ReadSlice('\n')
		r.offset += int64(len(data))
		if err == bufio.ErrBufferFull {
			oversized = true
			continue
		}
		if err != nil && err != io.EOF {
			r.err = err
			return false
		}
		if len(data) == 0 && !oversized {
			return false
		}
		if oversized {
			oversized = false
			lineStart = r.offset
			continue
		}
		var line Line
		if !utf8.Valid(data) || json.Unmarshal(data, &line) != nil || line.Time <= 0 || line.Text == "" {
			lineStart = r.offset
			continue
		}
		r.lineStart = lineStart
		r.line = line
		return true
	}
}
