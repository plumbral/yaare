package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

const fixtureReadDelay = 12 * time.Millisecond
const fixtureDownloadSize = 4<<20 + 19

type delayedDownloads struct {
	sftp.FileReader
	active, peak atomic.Int32
}

func (d *delayedDownloads) Fileread(req *sftp.Request) (io.ReaderAt, error) {
	reader, err := d.FileReader.Fileread(req)
	if err != nil {
		return nil, err
	}
	return delayedDownloadReader{reader, d}, nil
}

type delayedDownloadReader struct {
	io.ReaderAt
	delay *delayedDownloads
}

func (r delayedDownloadReader) ReadAt(data []byte, offset int64) (int, error) {
	current := r.delay.active.Add(1)
	defer r.delay.active.Add(-1)
	for previous := r.delay.peak.Load(); current > previous; previous = r.delay.peak.Load() {
		if r.delay.peak.CompareAndSwap(previous, current) {
			break
		}
	}
	delay := fixtureReadDelay
	if (offset/transferChunk)%4 == 0 {
		delay *= 2
	}
	time.Sleep(delay)
	return r.ReaderAt.ReadAt(data, offset)
}

type serialDownloads struct{ FS }
type serialDownloadReader struct{ io.ReadCloser }

func (f serialDownloads) Open(p string) (io.ReadCloser, error) {
	reader, err := f.FS.Open(p)
	if err != nil {
		return nil, err
	}
	return serialDownloadReader{reader}, nil
}

func delayedSFTPFixture(t *testing.T, size int) (Remote, *delayedDownloads, []byte) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	handlers := sftp.InMemHandler()
	delay := &delayedDownloads{FileReader: handlers.FileGet}
	handlers.FileGet = delay
	server := sftp.NewRequestServer(serverConn, handlers)
	served := make(chan error, 1)
	go func() { served <- server.Serve() }()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = server.Close()
		<-served
	})
	client, err := sftp.NewClientPipe(clientConn, clientConn, sftp.UseConcurrentReads(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	data := make([]byte, size)
	random := rand.NewPCG(1337, 1984)
	for offset := 0; offset < len(data); offset += 8 {
		value := random.Uint64()
		for index := offset; index < min(offset+8, len(data)); index++ {
			data[index] = byte(value)
			value >>= 8
		}
	}
	file, err := client.Create("/file")
	if err != nil {
		t.Fatal(err)
	}
	if count, err := file.Write(data); err != nil || count != len(data) {
		t.Fatal("seed bytes", count, err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return Remote{Client: client}, delay, data
}

func TestSFTPDownloadDelay(t *testing.T) {
	source, delay, data := delayedSFTPFixture(t, fixtureDownloadSize)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	items, total, err := Plan(ctx, source, "/", []string{"file"})
	if err != nil {
		t.Fatal(err)
	}
	var measurements [2]time.Duration
	for index, input := range []FS{serialDownloads{source}, source} {
		dest, dst := sandbox(t)
		delay.peak.Store(0)
		start := time.Now()
		err = Copy(ctx, input, dest, items, "/", "delay", total, overwriteCopy, func(Progress) {})
		measurements[index] = time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "file"))
		if err != nil || sha256.Sum256(got) != sha256.Sum256(data) {
			t.Fatal("download checksum mismatch", err)
		}
		peak := delay.peak.Load()
		if index == 0 && peak != 1 || index == 1 && (peak <= 1 || peak > downloadWindow) || delay.active.Load() != 0 {
			t.Fatalf("case %d: incorrect in-flight reads: peak=%d active=%d", index, peak, delay.active.Load())
		}
		t.Logf("case=%d bytes=%d duration=%s peak_reads=%d", index, total, measurements[index].Round(time.Millisecond), peak)
	}
	t.Logf("serial/read-ahead=%.2fx (base latency=%s, every fourth chunk takes twice as long)", float64(measurements[0])/float64(measurements[1]), fixtureReadDelay)
}

func TestSFTPDownloadResumeAndSize(t *testing.T) {
	source, _, data := delayedSFTPFixture(t, 13*transferChunk+97)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items, total, err := Plan(ctx, source, "/", []string{"file"})
	if err != nil {
		t.Fatal(err)
	}
	dest, dst := sandbox(t)
	target := filepath.Join(dst, "file")
	if err = os.WriteFile(target, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	pause, stop := context.WithCancel(ctx)
	defer stop()
	cp := CopyCheckpoint{}
	err = CopyResumable(pause, source, dest, items, "/", "resume-download", total, &cp, overwriteCopy, func(p Progress) {
		if p.Done >= 2*transferChunk {
			stop()
		}
	}, func(string) {})
	if !errors.Is(err, context.Canceled) || cp.File == nil || cp.File.Offset != 2*transferChunk {
		t.Fatal("prefetched data counted as committed", cp.File, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "previous" {
		t.Fatal("cancel changed target", err)
	}
	if staged, err := os.ReadFile(filepath.Join(dst, localPath(cp.File.Temporary))); err != nil || !bytes.Equal(staged, data[:cp.File.Offset]) {
		t.Fatal("pending reads wrote non-prefix bytes", err)
	}
	encoded, err := EncodeCheckpoint(cp)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeCheckpoint(encoded)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data[:cp.File.Offset])
	if !bytes.Equal(restored.File.ExpectedDigest, digest[:]) {
		t.Fatal("resume digest contains unacknowledged or unordered bytes")
	}
	if err = CopyResumable(ctx, source, dest, items, "/", "resume-download", total, &restored, overwriteCopy, func(Progress) {}, func(string) {}); err != nil || restored.Progress.Done != total || restored.Progress.Files != 1 {
		t.Fatal("resume failed", restored.Progress, err)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, data) {
		t.Fatal("resumed bytes differ", err)
	}
	for _, invalidSize := range []int64{total - 1, total + 1} {
		items[0].Size = invalidSize
		err = Copy(ctx, source, dest, items, "/", "size-check", invalidSize, overwriteCopy, func(Progress) {})
		if err == nil || err.Error() != sourceChanged {
			t.Fatal("incorrect size accepted", invalidSize, err)
		}
		if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, data) {
			t.Fatal("size failure touched existing bytes", err)
		}
	}
}

type delayedUploads struct {
	sftp.FileWriter
	opener       sftp.OpenFileWriter
	active, peak atomic.Int32
}

func (d *delayedUploads) Filewrite(req *sftp.Request) (io.WriterAt, error) {
	writer, err := d.FileWriter.Filewrite(req)
	if err != nil {
		return nil, err
	}
	return delayedUploadWriter{writer, d}, nil
}

func (d *delayedUploads) OpenFile(req *sftp.Request) (sftp.WriterAtReaderAt, error) {
	rw, err := d.opener.OpenFile(req)
	if err != nil {
		return nil, err
	}
	return delayedUploadReadWriter{rw, d}, nil
}

type delayedUploadWriter struct {
	io.WriterAt
	delay *delayedUploads
}

func (w delayedUploadWriter) WriteAt(data []byte, offset int64) (int, error) {
	current := w.delay.active.Add(1)
	defer w.delay.active.Add(-1)
	for previous := w.delay.peak.Load(); current > previous; previous = w.delay.peak.Load() {
		if w.delay.peak.CompareAndSwap(previous, current) {
			break
		}
	}
	delay := fixtureReadDelay
	if (offset/transferChunk)%4 == 0 {
		delay *= 2
	}
	time.Sleep(delay)
	return w.WriterAt.WriteAt(data, offset)
}

func (w delayedUploadWriter) Close() error {
	if closer, ok := w.WriterAt.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type delayedUploadReadWriter struct {
	sftp.WriterAtReaderAt
	delay *delayedUploads
}

func (w delayedUploadReadWriter) WriteAt(data []byte, offset int64) (int, error) {
	return delayedUploadWriter{WriterAt: w.WriterAtReaderAt, delay: w.delay}.WriteAt(data, offset)
}

func (w delayedUploadReadWriter) Close() error {
	if closer, ok := w.WriterAtReaderAt.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type serialUploads struct{ FS }
type serialUploadWriter struct{ io.WriteCloser }

func (f serialUploads) Create(p string) (io.WriteCloser, error) {
	writer, err := f.FS.Create(p)
	if err != nil {
		return nil, err
	}
	return serialUploadWriter{writer}, nil
}

type chmodInMemHandler struct {
	cmd   sftp.FileCmder
	posix sftp.PosixRenameFileCmder
	list  sftp.FileLister
	mu    sync.Mutex
	perm  map[string]fs.FileMode
}

type permOverrideInfo struct {
	fs.FileInfo
	perm fs.FileMode
}

func (p permOverrideInfo) Mode() fs.FileMode {
	return (p.FileInfo.Mode() &^ fs.ModePerm) | p.perm
}

type permOverrideLister struct {
	inner sftp.ListerAt
	perm  fs.FileMode
	has   bool
}

func (l permOverrideLister) ListAt(dst []fs.FileInfo, offset int64) (int, error) {
	n, err := l.inner.ListAt(dst, offset)
	if l.has {
		for i := 0; i < n; i++ {
			dst[i] = permOverrideInfo{FileInfo: dst[i], perm: l.perm}
		}
	}
	return n, err
}

func (h *chmodInMemHandler) Filecmd(req *sftp.Request) error {
	if req.Method == "Setstat" && req.AttrFlags().Permissions {
		h.mu.Lock()
		h.perm[req.Filepath] = req.Attributes().FileMode().Perm()
		h.mu.Unlock()
	}
	if req.Method == "Remove" || req.Method == "Rmdir" {
		h.mu.Lock()
		delete(h.perm, req.Filepath)
		h.mu.Unlock()
	}
	if req.Method == "Rename" {
		h.mu.Lock()
		if p, ok := h.perm[req.Filepath]; ok {
			h.perm[req.Target] = p
			delete(h.perm, req.Filepath)
		}
		h.mu.Unlock()
	}
	return h.cmd.Filecmd(req)
}

func (h *chmodInMemHandler) PosixRename(req *sftp.Request) error {
	h.mu.Lock()
	if p, ok := h.perm[req.Filepath]; ok {
		h.perm[req.Target] = p
		delete(h.perm, req.Filepath)
	}
	h.mu.Unlock()
	return h.posix.PosixRename(req)
}

func (h *chmodInMemHandler) Filelist(req *sftp.Request) (sftp.ListerAt, error) {
	lister, err := h.list.Filelist(req)
	if err != nil {
		return nil, err
	}
	if req.Method == "Stat" {
		h.mu.Lock()
		p, ok := h.perm[req.Filepath]
		h.mu.Unlock()
		return permOverrideLister{inner: lister, perm: p, has: ok}, nil
	}
	return lister, nil
}

func delayedSFTPUploadFixture(t *testing.T, size int) (Remote, *delayedUploads, []byte) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	handlers := sftp.InMemHandler()
	delay := &delayedUploads{FileWriter: handlers.FilePut, opener: handlers.FilePut.(sftp.OpenFileWriter)}
	handlers.FilePut = delay
	chmod := &chmodInMemHandler{cmd: handlers.FileCmd, posix: handlers.FileCmd.(sftp.PosixRenameFileCmder), list: handlers.FileList, perm: map[string]fs.FileMode{}}
	handlers.FileCmd = chmod
	handlers.FileList = chmod
	server := sftp.NewRequestServer(serverConn, handlers)
	served := make(chan error, 1)
	go func() { served <- server.Serve() }()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = server.Close()
		<-served
	})
	client, err := sftp.NewClientPipe(clientConn, clientConn, sftp.UseConcurrentWrites(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	data := make([]byte, size)
	random := rand.NewPCG(2025, 4242)
	for offset := 0; offset < len(data); offset += 8 {
		value := random.Uint64()
		for index := offset; index < min(offset+8, len(data)); index++ {
			data[index] = byte(value)
			value >>= 8
		}
	}
	return Remote{Client: client}, delay, data
}

func readRemoteAll(t *testing.T, remote Remote, path string) []byte {
	t.Helper()
	f, err := remote.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSFTPUploadDelay(t *testing.T) {
	dest, delay, data := delayedSFTPUploadFixture(t, 48*transferChunk+19)
	source, src := sandbox(t)
	if err := os.WriteFile(filepath.Join(src, "upload.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	items, total, err := Plan(ctx, source, "/", []string{"upload.bin"})
	if err != nil {
		t.Fatal(err)
	}
	var measurements [2]time.Duration
	for index, output := range []FS{serialUploads{dest}, dest} {
		delay.peak.Store(0)
		start := time.Now()
		err = Copy(ctx, source, output, items, "/", "upload-delay", total, overwriteCopy, func(Progress) {})
		measurements[index] = time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		got := readRemoteAll(t, dest, "/upload.bin")
		if sha256.Sum256(got) != sha256.Sum256(data) {
			t.Fatal("upload checksum mismatch")
		}
		peak := delay.peak.Load()
		if index == 0 && peak != 1 || index == 1 && (peak <= 1 || peak > uploadWindow) || delay.active.Load() != 0 {
			t.Fatalf("case %d: incorrect in-flight writes: peak=%d active=%d", index, peak, delay.active.Load())
		}
		t.Logf("case=%d bytes=%d duration=%s peak_writes=%d", index, total, measurements[index].Round(time.Millisecond), peak)
	}
	t.Logf("serial/pipelined-upload=%.2fx", float64(measurements[0])/float64(measurements[1]))
}

func TestSFTPUploadResumeAndSize(t *testing.T) {
	dest, _, data := delayedSFTPUploadFixture(t, 13*transferChunk+97)
	source, src := sandbox(t)
	if err := os.WriteFile(filepath.Join(src, "upload.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	existing, err := dest.Create("/upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := existing.Write([]byte("previous")); err != nil {
		t.Fatal(err)
	}
	if err := existing.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items, total, err := Plan(ctx, source, "/", []string{"upload.bin"})
	if err != nil {
		t.Fatal(err)
	}
	pause, stop := context.WithCancel(ctx)
	defer stop()
	cp := CopyCheckpoint{}
	err = CopyResumable(pause, source, dest, items, "/", "resume-upload", total, &cp, overwriteCopy, func(p Progress) {
		if p.Done >= 2*transferChunk {
			stop()
		}
	}, func(string) {})
	if !errors.Is(err, context.Canceled) || cp.File == nil || cp.File.Offset < 2*transferChunk || cp.File.Offset >= total {
		t.Fatalf("unexpected upload pause checkpoint: cp=%+v err=%v", cp.File, err)
	}
	if got := readRemoteAll(t, dest, "/upload.bin"); string(got) != "previous" {
		t.Fatal("cancel changed remote target")
	}
	encoded, err := EncodeCheckpoint(cp)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeCheckpoint(encoded)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data[:cp.File.Offset])
	if !bytes.Equal(restored.File.ExpectedDigest, digest[:]) {
		t.Fatal("upload resume digest does not match contiguous acknowledged prefix")
	}
	if err = CopyResumable(ctx, source, dest, items, "/", "resume-upload", total, &restored, overwriteCopy, func(Progress) {}, func(string) {}); err != nil || restored.Progress.Done != total || restored.Progress.Files != 1 {
		t.Fatal("upload resume failed", restored.Progress, err)
	}
	if got := readRemoteAll(t, dest, "/upload.bin"); !bytes.Equal(got, data) {
		t.Fatal("resumed upload bytes differ")
	}
	for _, invalidSize := range []int64{total - 1, total + 1} {
		items[0].Size = invalidSize
		err = Copy(ctx, source, dest, items, "/", "upload-size-check", invalidSize, overwriteCopy, func(Progress) {})
		if err == nil || err.Error() != sourceChanged {
			t.Fatal("incorrect upload size accepted", invalidSize, err)
		}
		if got := readRemoteAll(t, dest, "/upload.bin"); !bytes.Equal(got, data) {
			t.Fatal("upload size failure touched existing remote bytes")
		}
	}
}
