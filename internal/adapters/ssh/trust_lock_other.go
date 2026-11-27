//go:build !windows && !darwin && !linux && !dragonfly && !freebsd && !netbsd && !openbsd

package sshclient

import (
	"errors"
	"os"
)

func tryHostFileLock(*os.File) (bool, error) { return false, errors.ErrUnsupported }
