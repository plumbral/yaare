package sessionlog

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"yaare/internal/domain/workspace"
)

const MaxBytes int64 = 20 << 20
const TotalBytes int64 = 200 << 20
const MaxRecords = 100

type Info struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SessionID string `json:"sessionId"`
	Attempt   int    `json:"attempt"`
	Started   int64  `json:"started"`
	Ended     int64  `json:"ended"`
	Bytes     int64  `json:"bytes"`
	State     string `json:"state"`
	Message   string `json:"message"`
}
type Line struct {
	Time int64  `json:"time"`
	Text string `json:"text"`
}
type Query struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	After  int64  `json:"after"`
	Cursor int    `json:"cursor"`
}
type Page struct {
	Lines []Line `json:"lines"`
	Next  int    `json:"next"`
	More  bool   `json:"more"`
}
type packet struct {
	at    int64
	bytes []byte
}
type Recording struct {
	mu     sync.Mutex
	info   Info
	queue  chan packet
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	path   string
	file   *os.File
	max    int64
	finish func(Info)
}

func (r *Recording) Info() Info { r.mu.Lock(); defer r.mu.Unlock(); return r.info }
func (r *Recording) end(reason string) {
	r.once.Do(func() { r.mu.Lock(); r.info.Message = reason; r.mu.Unlock(); close(r.stop) })
}

func (r *Recording) Capture(data []byte) {
	select {
	case <-r.stop:
		return
	default:
	}
	for len(data) > 0 {
		n := min(len(data), 32768)
		p := packet{time.Now().UnixMilli(), append([]byte(nil), data[:n]...)}
		data = data[n:]
		select {
		case <-r.stop:
			return
		case r.queue <- p:
		default:
			r.end("日志写入跟不上输出，已停止记录；SSH 不受影响。")
			return
		}
	}
}
func (r *Recording) Stop(ctx context.Context, reason string) (Info, error) {
	r.end(reason)
	select {
	case <-r.done:
		return r.Info(), nil
	case <-ctx.Done():
		return r.Info(), errors.New("日志正在结束，请稍后刷新。")
	}
}
func (r *Recording) run() {
	var plain plainText
	var writeErr error
	writer := bufio.NewWriterSize(r.file, maxLineBytes)
	at := r.Info().Started
	emit := func(text string) {
		if writeErr != nil {
			return
		}
		data, _ := json.Marshal(Line{at, text})
		data = append(data, '\n')
		r.mu.Lock()
		fits := r.info.Bytes+int64(len(data)) <= r.max
		r.mu.Unlock()
		if !fits {
			writeErr = errors.New("limit")
			r.end("日志达到 20 MiB 上限，已停止记录。")
			return
		}
		n, err := writer.Write(data)
		r.mu.Lock()
		r.info.Bytes += int64(n)
		r.mu.Unlock()
		if err != nil {
			writeErr = err
			r.end("日志写入失败，已停止记录；SSH 不受影响。")
		}
	}
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case p := <-r.queue:
			at = p.at
			plain.feed(p.bytes, emit)
		case <-timer.C:
			plain.flush(emit)
			if err := writer.Flush(); err != nil && writeErr == nil {
				writeErr = err
				r.end("日志写入失败，已停止记录；SSH 不受影响。")
			}
		case <-r.stop:
			draining := true
			for draining {
				select {
				case p := <-r.queue:
					at = p.at
					plain.feed(p.bytes, emit)
				default:
					draining = false
				}
			}
			plain.flush(emit)
			if err := writer.Flush(); err != nil && writeErr == nil {
				writeErr = err
			}
			if err := r.file.Sync(); err != nil && writeErr == nil {
				writeErr = err
			}
			if err := r.file.Close(); err != nil && writeErr == nil {
				writeErr = err
			}
			info := r.Info()
			info.Ended = time.Now().UnixMilli()
			info.State = "stopped"
			if writeErr != nil || info.Message != "" {
				info.State = "limited"
				if info.Message == "" {
					info.Message = "日志未完整写入，请检查磁盘后重新记录。"
				}
			}
			if err := writeIndex(r.path+".json", info); err != nil {
				info.State = "limited"
				info.Message = "日志索引保存失败，本次内容可能无法在重启后打开。"
			}
			r.mu.Lock()
			r.info = info
			r.mu.Unlock()
			close(r.done)
			if r.finish != nil {
				r.finish(r.Info())
			}
			return
		}
	}
}

type Store struct {
	mu            sync.Mutex
	root          func() (string, error)
	path          string
	records       map[string]*Recording
	retainedBytes int64
	ready         bool
	closed        bool
}

func New(root func() (string, error)) *Store {
	return &Store{root: root, records: map[string]*Recording{}}
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func (s *Store) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initializeLocked()
}

func (s *Store) initializeLocked() error {
	if s.closed {
		return errors.New("日志服务不可用。")
	}
	if s.ready {
		return nil
	}
	root, err := s.root()
	if err != nil {
		return errors.New("日志目录不可用。")
	}
	s.path = filepath.Join(root, "session-logs")
	if err = os.MkdirAll(s.path, 0700); err != nil {
		return errors.New("无法创建日志目录。")
	}
	entries, err := os.ReadDir(s.path)
	if err != nil {
		return errors.New("无法读取日志目录。")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if !identifier.MatchString(id) {
			continue
		}
		if len(s.records) >= MaxRecords {
			break
		}
		path := filepath.Join(s.path, id)
		info, ok := loadIndex(path+".json", id)
		if !ok {
			continue
		}
		file, size, err := openRegular(path+".jsonl", MaxBytes)
		if err != nil {
			continue
		}
		file.Close()
		info.Bytes = size
		if info.State == "recording" {
			info.State = "stopped"
			info.Message = "上次程序退出，记录已结束。"
		}
		s.records[id] = &Recording{info: info, path: path, max: MaxBytes}
	}
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".jsonl")
		if !ok || !identifier.MatchString(id) || s.records[id] != nil || !entry.Type().IsRegular() {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			s.retainedBytes += min(info.Size(), TotalBytes-s.retainedBytes)
		}
	}
	s.ready = true
	return nil
}
func (s *Store) Start(session string, attempt int, name string, finish func(Info)) (*Recording, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !identifier.MatchString(session) || attempt < 1 {
		return nil, errors.New("会话记录请求无效。")
	}
	if err := s.initializeLocked(); err != nil {
		return nil, err
	}
	used := s.retainedBytes
	for _, record := range s.records {
		info := record.Info()
		if info.State == "recording" {
			used += MaxBytes
		} else {
			used += info.Bytes
		}
	}
	if len(s.records) >= MaxRecords || used+MaxBytes > TotalBytes {
		return nil, errors.New("日志空间已满，请删除不需要的记录后再开始。")
	}
	id := workspace.NewID()
	if id == "" {
		return nil, errors.New("无法创建日志编号。")
	}
	p := filepath.Join(s.path, id)
	file, err := os.OpenFile(p+".jsonl", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, errors.New("无法创建日志文件。")
	}
	r := &Recording{info: Info{ID: id, Name: name, SessionID: session, Attempt: attempt, Started: time.Now().UnixMilli(), State: "recording"}, path: p, file: file, max: MaxBytes, queue: make(chan packet, 16), stop: make(chan struct{}), done: make(chan struct{}), finish: finish}
	if err = writeIndex(p+".json", r.info); err != nil {
		file.Close()
		os.Remove(p + ".jsonl")
		return nil, errors.New("无法保存日志索引。")
	}
	s.records[id] = r
	go r.run()
	return r, nil
}
func (s *Store) List() ([]Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.initializeLocked(); err != nil {
		return nil, err
	}
	out := []Info{}
	for _, r := range s.records {
		out = append(out, r.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	return out, nil
}
func (s *Store) get(id string) (*Recording, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.initializeLocked(); err != nil {
		return nil, err
	}
	r := s.records[id]
	if !identifier.MatchString(id) || r == nil {
		return nil, errors.New("日志不存在，请刷新列表。")
	}
	return r, nil
}
func (s *Store) Read(ctx context.Context, q Query) (Page, error) {
	out := Page{Lines: []Line{}}
	if len(q.Text) > 1024 || q.Cursor < 0 || int64(q.Cursor) > MaxBytes || q.After < 0 {
		return out, errors.New("无效的日志查询。")
	}
	r, err := s.get(q.ID)
	if err != nil {
		return out, err
	}
	file, size, err := openRegular(r.path+".jsonl", MaxBytes)
	if err != nil {
		return out, errors.New("无法读取日志。")
	}
	defer file.Close()
	start := int64(q.Cursor)
	if start >= size {
		out.Next = int(size)
		return out, nil
	}
	if start > 0 {
		if _, err = file.Seek(start, io.SeekStart); err != nil {
			return out, errors.New("无法读取日志。")
		}
	}
	reader := newLogReader(file, start, MaxBytes-start)
	needle := strings.ToLower(q.Text)
	bytes := 0
	for reader.next(ctx) {
		line := reader.line
		if line.Time < q.After || !strings.Contains(strings.ToLower(line.Text), needle) {
			continue
		}
		if len(out.Lines) >= 200 || bytes+len(line.Text) > 128<<10 {
			out.More = true
			out.Next = int(reader.lineStart)
			return out, nil
		}
		out.Lines = append(out.Lines, line)
		bytes += len(line.Text)
	}
	out.Next = int(reader.offset)
	if ctx.Err() != nil {
		return out, errors.New("日志查询已取消。")
	}
	if reader.err != nil {
		return out, errors.New("日志读取未完成，请重试。")
	}
	return out, nil
}
func (s *Store) Export(ctx context.Context, id string, w io.Writer) error {
	r, err := s.get(id)
	if err != nil {
		return err
	}
	if r.Info().State == "recording" {
		return errors.New("请先停止记录，再导出完整日志。")
	}
	file, _, err := openRegular(r.path+".jsonl", MaxBytes)
	if err != nil {
		return errors.New("无法读取日志。")
	}
	defer file.Close()
	reader := newLogReader(file, 0, MaxBytes)
	for reader.next(ctx) {
		line := reader.line
		ending := ""
		if !strings.HasSuffix(line.Text, "\n") {
			ending = "\n"
		}
		if _, err = fmt.Fprintf(w, "[%s] %s%s", time.UnixMilli(line.Time).Format(time.RFC3339Nano), line.Text, ending); err != nil {
			return errors.New("日志导出写入失败。")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader.err != nil {
		return errors.New("日志读取未完成，请重试。")
	}
	return nil
}
func (s *Store) Delete(id string) error {
	r, err := s.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Info().State == "recording" {
		return errors.New("请先停止记录。")
	}
	if err = os.Remove(r.path + ".jsonl"); err != nil && !os.IsNotExist(err) {
		return errors.New("无法删除日志内容。")
	}
	if err = os.Remove(r.path + ".json"); err != nil && !os.IsNotExist(err) {
		return errors.New("无法删除日志索引。")
	}
	delete(s.records, id)
	return nil
}
func (s *Store) Close() {
	s.mu.Lock()
	s.closed = true
	records := []*Recording{}
	for _, r := range s.records {
		if r.Info().State == "recording" {
			records = append(records, r)
		}
	}
	s.mu.Unlock()
	for _, r := range records {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		r.Stop(ctx, "")
		cancel()
	}
}
