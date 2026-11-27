package buildassets

import (
	"archive/zip"
	"compress/flate"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Files        int   `json:"files"`
	SourceBytes  int64 `json:"sourceBytes"`
	ArchiveBytes int64 `json:"archiveBytes"`
}

func Pack(source, target string) (Result, error) {
	var result Result
	if _, err := os.Stat(filepath.Join(source, "index.html")); err != nil {
		return result, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return result, err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".frontend-*.zip")
	if err != nil {
		return result, err
	}
	defer os.Remove(temp.Name())
	writer := zip.NewWriter(temp)
	writer.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, flate.BestCompression) })
	err = fs.WalkDir(os.DirFS(source), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("frontend asset is not a regular file: %s", name)
		}
		file, err := os.Open(filepath.Join(source, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		defer file.Close()
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0444)
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		switch strings.ToLower(filepath.Ext(name)) {
		case ".js", ".css", ".html", ".json", ".webmanifest", ".svg", ".txt":
			header.Method = zip.Deflate
		}
		out, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		count, err := io.Copy(out, file)
		if err != nil {
			return err
		}
		result.SourceBytes += count
		result.Files++
		return nil
	})
	zipErr := writer.Close()
	closeErr := temp.Close()
	if err != nil {
		return result, err
	}
	if zipErr != nil {
		return result, zipErr
	}
	if closeErr != nil {
		return result, closeErr
	}
	info, err := os.Stat(temp.Name())
	if err != nil {
		return result, err
	}
	result.ArchiveBytes = info.Size()
	if err := os.Rename(temp.Name(), target); err != nil {
		return result, err
	}
	return result, nil
}
