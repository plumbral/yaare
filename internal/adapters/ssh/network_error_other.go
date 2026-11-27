//go:build !windows

package sshclient

func normalizeSocketError(err error) error { return err }
