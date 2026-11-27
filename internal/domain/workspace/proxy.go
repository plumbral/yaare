package workspace

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type ProxyConfig struct {
	Kind              string `json:"kind"`
	Address           string `json:"address"`
	Port              int    `json:"port"`
	UseAuthentication bool   `json:"useAuthentication"`
	Username          string `json:"username"`
}

func ValidateProxy(p ProxyConfig) error {
	if p.Kind == "" || p.Kind == "none" || p.Kind == "auto" {
		if p.Address != "" || p.Port != 0 || p.Username != "" || p.UseAuthentication {
			return Fail("INVALID_PROXY", "直连配置不能包含代理地址或认证内容。")
		}
		return nil
	}
	if p.Kind != "socks5" && p.Kind != "http" {
		return Fail("INVALID_PROXY", "请选择 SOCKS5 或 HTTP 代理。")
	}
	if !addressPattern.MatchString(p.Address) || p.Port < 1 || p.Port > 65535 {
		return Fail("INVALID_PROXY", "请检查代理地址和端口，不要填写协议、路径或认证信息。")
	}
	if !p.UseAuthentication {
		if p.Username != "" {
			return Fail("INVALID_PROXY", "填写代理用户名时请启用代理认证。")
		}
		return nil
	}
	if !utf8.ValidString(p.Username) || len(p.Username) < 1 || len(p.Username) > 255 || strings.IndexFunc(p.Username, unicode.IsControl) >= 0 || (p.Kind == "http" && strings.Contains(p.Username, ":")) {
		return Fail("INVALID_PROXY", "代理用户名应为 1–255 字节；HTTP 用户名不能包含冒号。")
	}
	return nil
}
