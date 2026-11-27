package sshclient

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func normalizeSocketError(err error) error {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err
	}
	switch errno {
	case windows.WSAECONNREFUSED:
		return syscall.ECONNREFUSED
	case windows.WSAECONNRESET, windows.WSAENETRESET:
		return syscall.ECONNRESET
	case windows.WSAECONNABORTED:
		return syscall.ECONNABORTED
	case windows.WSAENETUNREACH, windows.WSAENETDOWN, windows.WSAEHOSTUNREACH, windows.WSAEHOSTDOWN:
		return syscall.ENETUNREACH
	case windows.WSAETIMEDOUT:
		return syscall.ETIMEDOUT
	default:
		return err
	}
}
