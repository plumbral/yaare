package sshclient

import (
	"strings"
	"unicode/utf8"
)

func BroadcastInput(command string, execute bool) ([]byte, error) {
	command = strings.ReplaceAll(command, "\r\n", "\n")
	if !utf8.ValidString(command) || strings.TrimSpace(command) == "" || len(command) > 65536 {
		return nil, fault("BROADCAST_INVALID", "请输入不超过 64 KiB 的有效命令。")
	}
	for _, r := range command {
		if r < 32 && r != '\t' && r != '\n' || r >= 127 && r <= 159 || r == '\u2028' || r == '\u2029' {
			return nil, fault("BROADCAST_INVALID", "命令包含不支持的控制字符。")
		}
	}
	if !execute && strings.Contains(command, "\n") {
		return nil, fault("BROADCAST_INVALID", "多行命令请使用发送回车模式，并完整预览后确认。")
	}
	input := strings.ReplaceAll(command, "\n", "\r")
	if execute {
		input += "\r"
	}
	if len(input) > 65536 {
		return nil, fault("BROADCAST_INVALID", "包含回车后的发送内容超过 64 KiB。")
	}
	return []byte(input), nil
}
