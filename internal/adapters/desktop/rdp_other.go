//go:build !windows

package desktop

import "errors"

func LaunchRDP(string) (func() error, error) {
	return nil, errors.New("此入口使用 Windows 远程桌面客户端，请在 Windows 桌面端打开。")
}
