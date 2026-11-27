package sshclient

import (
	"context"
	"os"
	"time"
)

func lockHostTrust(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	failed := fault("KNOWN_HOSTS_WRITE", "无法锁定主机信任记录。")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, failed
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	retry := time.NewTicker(40 * time.Millisecond)
	defer retry.Stop()
	for {
		locked, err := tryHostFileLock(file)
		if err != nil {
			file.Close()
			return nil, failed
		}
		if locked {
			return file, nil
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			file.Close()
			return nil, fault("KNOWN_HOSTS_BUSY", "主机信任记录正被使用，请稍后重试。")
		case <-retry.C:
		}
	}
}
