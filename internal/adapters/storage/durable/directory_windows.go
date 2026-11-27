//go:build windows

package durable

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func SyncDir(string) error { return nil }

func Rename(temp, dest string) error {
	from, err := windowsPath(temp)
	if err != nil {
		return err
	}
	to, err := windowsPath(dest)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func windowsPath(path string) (*uint16, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(absolute, `\\?\`) && !strings.HasPrefix(absolute, `\\.\`) {
		if unc, ok := strings.CutPrefix(absolute, `\\`); ok {
			absolute = `\\?\UNC\` + unc
		} else {
			absolute = `\\?\` + absolute
		}
	}
	return windows.UTF16PtrFromString(absolute)
}
