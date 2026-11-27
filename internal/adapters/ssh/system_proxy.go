package sshclient

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"golang.org/x/net/http/httpproxy"
	"yaare/internal/domain/workspace"
)

func envSystemProxy(target string) (workspace.ProxyConfig, string, error) {
	cfg := httpproxy.FromEnvironment()
	if cfg.HTTPSProxy == "" {
		cfg.HTTPSProxy = cfg.HTTPProxy
	}
	if cfg.HTTPSProxy == "" {
		cfg.HTTPSProxy = os.Getenv("ALL_PROXY")
		if cfg.HTTPSProxy == "" {
			cfg.HTTPSProxy = os.Getenv("all_proxy")
		}
	}
	if cfg.HTTPSProxy != "" {
		if _, _, err := parseSystemProxy(cfg.HTTPSProxy, "http"); err != nil {
			return invalidSystemProxy()
		}
	}
	u, err := cfg.ProxyFunc()(&url.URL{Scheme: "https", Host: target})
	if err != nil {
		return invalidSystemProxy()
	}
	if u == nil {
		return workspace.ProxyConfig{}, "", nil
	}
	return parseSystemProxy(u.String(), "http")
}

func invalidSystemProxy() (workspace.ProxyConfig, string, error) {
	return workspace.ProxyConfig{}, "", fault("SYSTEM_PROXY_INVALID", "系统代理不可用，请检查系统代理设置。")
}

func parseSystemProxy(raw, kind string) (workspace.ProxyConfig, string, error) {
	if !strings.Contains(raw, "://") {
		raw = kind + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return invalidSystemProxy()
	}
	port := u.Port()
	switch u.Scheme {
	case "http":
		kind = "http"
		if port == "" {
			port = "80"
		}
	case "socks", "socks5", "socks5h":
		kind = "socks5"
		if port == "" {
			port = "1080"
		}
	default:
		return invalidSystemProxy()
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return invalidSystemProxy()
	}
	p := workspace.ProxyConfig{Kind: kind, Address: u.Hostname(), Port: n}
	var password string
	if u.User != nil {
		p.Username = u.User.Username()
		password, _ = u.User.Password()
		p.UseAuthentication = true
	}
	if workspace.ValidateProxy(p) != nil || (password != "" && !workspace.ValidProxyPassword(p, password)) {
		return invalidSystemProxy()
	}
	return p, password, nil
}

func targetHost(target string) string {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return target
	}
	return host
}
