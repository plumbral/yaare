package frontendassets_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/egoist/mygo"
	"yaare/internal/adapters/frontendassets"
	"yaare/internal/buildassets"
)

func fixture(t *testing.T) (map[string][]byte, []byte, *frontendassets.Files) {
	t.Helper()
	files := map[string][]byte{
		"index.html":        []byte("<!doctype html><title>Yaare 中文</title>"),
		"assets/lazy.js":    bytes.Repeat([]byte("export const text='中文・日本語';\n"), 300),
		"assets/theme.css":  []byte("body { color: black; }"),
		"assets/font.woff2": bytes.Repeat([]byte{0, 1, 2, 3, 4, 5, 0xff}, 150000),
		"assets/许可.txt":     []byte("Original license text\nCopyright\n"),
	}
	root := t.TempDir()
	for name, data := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "frontend.zip")
	result, err := buildassets.Pack(root, archive)
	if err != nil || result.Files != len(files) {
		t.Fatal(result, err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	for name := range files {
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(name)), time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := buildassets.Pack(root, archive); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("archive is not deterministic", err)
	}
	packed, err := frontendassets.New(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return files, data, packed
}

func TestPackedFrontendPreservesBytesAndStandardFilesystem(t *testing.T) {
	files, _, packed := fixture(t)
	names := make([]string, 0, len(files))
	for name, original := range files {
		names = append(names, name)
		data, err := fs.ReadFile(packed, name)
		if err != nil || sha256.Sum256(data) != sha256.Sum256(original) {
			t.Fatal("asset changed", name, err)
		}
	}
	if err := fstest.TestFS(packed, names...); err != nil {
		t.Fatal(err)
	}
}

func TestPackedFrontendWithActualMyGoHTTPHandler(t *testing.T) {
	files, _, packed := fixture(t)
	handler := mygo.FileServer(packed)
	for name, data := range files {
		url := "/" + name
		if name == "index.html" {
			url = "/"
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url+"?version=1", nil))
		if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), data) || response.Header().Get("Content-Type") == "" {
			t.Fatal(name, response.Code, response.Body.String())
		}
		if response.Header().Get("Last-Modified") != "" || response.Header().Get("Content-Encoding") != "" {
			t.Fatal("must preserve the previous embed.FS cache and decoded-response contract")
		}
		cachedRequest := httptest.NewRequest(http.MethodGet, url, nil)
		cachedRequest.Header.Set("If-Modified-Since", "Wed, 01 Jan 2031 00:00:00 GMT")
		cachedResponse := httptest.NewRecorder()
		handler.ServeHTTP(cachedResponse, cachedRequest)
		if cachedResponse.Code != 200 {
			t.Fatal("fixed ZIP timestamp made an updated asset appear unchanged", name)
		}
		head := httptest.NewRecorder()
		handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, url, nil))
		if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(len(data)) {
			t.Fatal("HEAD", name, head.Code)
		}
		request := httptest.NewRequest(http.MethodGet, url, nil)
		request.Header.Set("Range", "bytes=2-12")
		rangeResponse := httptest.NewRecorder()
		handler.ServeHTTP(rangeResponse, request)
		if rangeResponse.Code != 206 || !bytes.Equal(rangeResponse.Body.Bytes(), data[2:13]) {
			t.Fatal("range", name, rangeResponse.Code)
		}
	}
	for _, name := range []string{"/", "/settings/about"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, name, nil))
		if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), files["index.html"]) {
			t.Fatal("index/SPA fallback", name, response.Code)
		}
	}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/missing.js", nil))
	if missing.Code != 404 {
		t.Fatal("missing chunk must not receive HTML", missing.Code)
	}
}

func TestBuiltFrontendPreservesEveryProductionAsset(t *testing.T) {
	directory := os.Getenv("YAARE_VERIFY_FRONTEND_DIST")
	if directory == "" {
		t.Skip("enable after a web production build")
	}
	packed, err := frontendassets.Load()
	if err != nil {
		t.Fatal(err)
	}
	handler := mygo.FileServer(packed)
	count := 0
	err = fs.WalkDir(os.DirFS(directory), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		original, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		data, err := fs.ReadFile(packed, name)
		if err != nil || sha256.Sum256(data) != sha256.Sum256(original) {
			t.Fatalf("packed bytes changed: %s: %v", name, err)
		}
		url := "/" + name
		if name == "index.html" {
			url = "/"
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
		if response.Code != 200 || sha256.Sum256(response.Body.Bytes()) != sha256.Sum256(original) {
			t.Fatalf("MyGo response changed: %s (%d)", name, response.Code)
		}
		count++
		return nil
	})
	if err != nil || count == 0 {
		t.Fatal(count, err)
	}
	t.Logf("verified %d production files through the actual desktop handler", count)
}

type countingReader struct {
	*bytes.Reader
	readBytes int
}

func (r *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.Reader.ReadAt(p, off)
	r.readBytes += n
	return n, err
}

func TestStoredFontsAreSeekableWithoutExpandingTheWholeFile(t *testing.T) {
	files, data, _ := fixture(t)
	reader := &countingReader{Reader: bytes.NewReader(data)}
	packed, err := frontendassets.New(reader, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	reader.readBytes = 0
	file, err := packed.Open("assets/font.woff2")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seeker, ok := file.(io.Seeker)
	if !ok {
		t.Fatal("font must support range/seek")
	}
	if _, err = seeker.Seek(900000, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 32)
	if _, err = io.ReadFull(file, chunk); err != nil || !bytes.Equal(chunk, files["assets/font.woff2"][900000:900032]) {
		t.Fatal("font byte range", err)
	}
	if reader.readBytes > 4096 {
		t.Fatal("a small font range expanded the whole font", reader.readBytes)
	}
}

func TestInvalidArchivesAndCorruptCompressedChunksFail(t *testing.T) {
	for _, names := range [][]string{{"assets/a.js"}, {"index.html", "../outside"}, {"index.html", "index.html"}} {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		for _, name := range names {
			file, _ := writer.Create(name)
			_, _ = file.Write([]byte("test"))
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := frontendassets.New(bytes.NewReader(buffer.Bytes()), int64(buffer.Len())); err == nil {
			t.Fatal("invalid archive accepted", names)
		}
	}
	_, data, _ := fixture(t)
	reader, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, file := range reader.File {
		if file.Name == "assets/lazy.js" {
			offset, _ := file.DataOffset()
			data[offset+int64(file.CompressedSize64)/2] ^= 0xff
		}
	}
	packed, err := frontendassets.New(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile(packed, "assets/lazy.js"); err == nil {
		t.Fatal("corrupt compressed script accepted")
	}
}

func TestHeadDoesNotReadCompressedBody(t *testing.T) {
	files, data, _ := fixture(t)
	reader := &countingReader{Reader: bytes.NewReader(data)}
	packed, err := frontendassets.New(reader, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, rangeHeader := range []string{"", "bytes=2-12"} {
		reader.readBytes = 0
		request := httptest.NewRequest(http.MethodHead, "/assets/lazy.js", nil)
		request.Header.Set("Range", rangeHeader)
		response := httptest.NewRecorder()
		mygo.FileServer(packed).ServeHTTP(response, request)
		status, length := http.StatusOK, len(files["assets/lazy.js"])
		if rangeHeader != "" {
			status, length = http.StatusPartialContent, 11
		}
		if response.Code != status || response.Header().Get("Content-Length") != strconv.Itoa(length) || response.Body.Len() != 0 || reader.readBytes != 0 {
			t.Fatalf("HEAD %q read compressed data or returned bad metadata: status=%d, length=%s, reads=%d", rangeHeader, response.Code, response.Header().Get("Content-Length"), reader.readBytes)
		}
	}
}
