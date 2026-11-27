package frontendassets

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"time"
)

//go:embed all:packed
var packed embed.FS

func Load() (fs.FS, error) {
	file, err := packed.Open("packed/frontend.zip")
	if err != nil {
		return nil, fmt.Errorf("embedded frontend archive: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	reader, ok := file.(io.ReaderAt)
	if !ok {
		return nil, fmt.Errorf("embedded frontend archive is not readable")
	}
	return New(reader, info.Size())
}

type Files struct {
	reader  io.ReaderAt
	archive *zip.Reader
	files   map[string]*zip.File
}

func New(reader io.ReaderAt, size int64) (*Files, error) {
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return nil, err
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if !fs.ValidPath(file.Name) {
			return nil, fmt.Errorf("invalid frontend asset name")
		}
		if _, exists := files[file.Name]; exists {
			return nil, fmt.Errorf("duplicate frontend asset name")
		}
		files[file.Name] = file
	}
	if files["index.html"] == nil {
		return nil, fmt.Errorf("frontend index.html is missing")
	}
	return &Files{reader: reader, archive: archive, files: files}, nil
}

type fileInfo struct{ fs.FileInfo }

func (fileInfo) ModTime() time.Time { return time.Time{} }

type directoryFile struct{ fs.ReadDirFile }

func (f *directoryFile) Stat() (fs.FileInfo, error) {
	info, err := f.ReadDirFile.Stat()
	if err != nil {
		return nil, err
	}
	return fileInfo{info}, nil
}

type directoryEntry struct{ fs.DirEntry }

func (e directoryEntry) Info() (fs.FileInfo, error) {
	info, err := e.DirEntry.Info()
	if err != nil {
		return nil, err
	}
	return fileInfo{info}, nil
}
func (f *directoryFile) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.ReadDirFile.ReadDir(n)
	for i, entry := range entries {
		entries[i] = directoryEntry{entry}
	}
	return entries, err
}

type seekFile struct {
	io.ReadSeeker
	info fs.FileInfo
}

func (f *seekFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (*seekFile) Close() error                 { return nil }

type compressedAsset struct {
	file    *zip.File
	content *bytes.Reader
	err     error
}

func (a *compressedAsset) ReadAt(p []byte, offset int64) (int, error) {
	if a.content == nil && a.err == nil {
		reader, err := a.file.Open()
		if err != nil {
			a.err = err
		} else {
			data, readErr := io.ReadAll(reader)
			if closeErr := reader.Close(); readErr == nil {
				readErr = closeErr
			}
			a.err = readErr
			if readErr == nil {
				a.content = bytes.NewReader(data)
			}
		}
	}
	if a.err != nil {
		return 0, a.err
	}
	return a.content.ReadAt(p, offset)
}

func (f *Files) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	file := f.files[name]
	if file == nil {
		entry, err := f.archive.Open(name)
		if err != nil {
			return nil, err
		}
		if directory, ok := entry.(fs.ReadDirFile); ok {
			return &directoryFile{directory}, nil
		}
		return entry, nil
	}
	info := fileInfo{file.FileInfo()}
	if file.Method == zip.Store {
		offset, err := file.DataOffset()
		if err != nil {
			return nil, err
		}
		return &seekFile{ReadSeeker: io.NewSectionReader(f.reader, offset, int64(file.UncompressedSize64)), info: info}, nil
	}
	body := &compressedAsset{file: file}
	return &seekFile{ReadSeeker: io.NewSectionReader(body, 0, int64(file.UncompressedSize64)), info: info}, nil
}

func (f *Files) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if file := f.files[name]; file != nil {
		return fileInfo{file.FileInfo()}, nil
	}
	entry, err := f.archive.Open(name)
	if err != nil {
		return nil, err
	}
	defer entry.Close()
	info, err := entry.Stat()
	if err != nil {
		return nil, err
	}
	return fileInfo{info}, nil
}
