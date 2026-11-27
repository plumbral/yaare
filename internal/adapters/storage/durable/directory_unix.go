//go:build !windows

package durable

import "os"

func SyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func Rename(temp, dest string) error {
	return os.Rename(temp, dest)
}
