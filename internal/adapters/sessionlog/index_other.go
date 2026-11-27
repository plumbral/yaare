//go:build !windows

package sessionlog

import (
	"os"
	"path/filepath"
)

func replaceIndex(temp, path string) error {
	if err := os.Rename(temp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
