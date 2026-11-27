//go:build windows

package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func LaunchRDP(file string) (func() error, error) {
	program := filepath.Join(os.Getenv("SystemRoot"), "System32", "mstsc.exe")
	if !filepath.IsAbs(program) {
		return nil, errors.New("Windows 远程桌面客户端路径无效。")
	}
	command := exec.Command(program, file, "/public", "/prompt")
	if e := command.Start(); e != nil {
		return nil, errors.New("无法打开 Windows 远程桌面，请确认已安装 mstsc.exe。")
	}
	return command.Wait, nil
}
