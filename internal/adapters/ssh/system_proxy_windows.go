package sshclient

import (
	"context"
	"net"
	"path"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"yaare/internal/domain/workspace"
)

var winHTTP = windows.NewLazySystemDLL("winhttp.dll")
var sysProxyConfig = winHTTP.NewProc("WinHttpGetIEProxyConfigForCurrentUser")
var proxyOpen = winHTTP.NewProc("WinHttpOpen")
var proxyClose = winHTTP.NewProc("WinHttpCloseHandle")
var proxyTimeouts = winHTTP.NewProc("WinHttpSetTimeouts")
var proxyForURL = winHTTP.NewProc("WinHttpGetProxyForUrl")
var freeProxyString = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")

type winProxySettings struct {
	autoDetect         uint32
	pac, proxy, bypass *uint16
}
type winAutoProxyOptions struct {
	flags, detect            uint32
	pac                      *uint16
	reserved                 uintptr
	reservedFlags, autoLogon uint32
}
type winProxyInfo struct {
	access        uint32
	proxy, bypass *uint16
}

func releaseProxyStrings(values ...*uint16) {
	for _, value := range values {
		if value != nil {
			freeProxyString.Call(uintptr(unsafe.Pointer(value)))
		}
	}
}

var proxyLookups = make(chan struct{}, 4)

func systemProxy(ctx context.Context, target string) (workspace.ProxyConfig, string, error) {
	type result struct {
		config   workspace.ProxyConfig
		password string
		err      error
	}
	select {
	case proxyLookups <- struct{}{}:
	case <-ctx.Done():
		return workspace.ProxyConfig{}, "", ctx.Err()
	}
	resolved := make(chan result, 1)
	go func() {
		defer func() { <-proxyLookups }()
		p, password, err := windowsSystemProxy(ctx, target)
		resolved <- result{p, password, err}
	}()
	select {
	case next := <-resolved:
		return next.config, next.password, next.err
	case <-ctx.Done():
		return workspace.ProxyConfig{}, "", ctx.Err()
	}
}

func windowsSystemProxy(ctx context.Context, target string) (workspace.ProxyConfig, string, error) {
	var config winProxySettings
	ok, _, err := sysProxyConfig.Call(uintptr(unsafe.Pointer(&config)))
	defer releaseProxyStrings(config.pac, config.proxy, config.bypass)
	if ok == 0 {
		if err == windows.ERROR_FILE_NOT_FOUND {
			return envSystemProxy(target)
		}
		return invalidSystemProxy()
	}
	pac := windows.UTF16PtrToString(config.pac)
	static := windows.UTF16PtrToString(config.proxy)
	bypass := windows.UTF16PtrToString(config.bypass)
	if pac != "" || config.autoDetect != 0 {
		info, autoErr := resolveWindowsProxy(ctx, target, pac, config.autoDetect != 0)
		defer releaseProxyStrings(info.proxy, info.bypass)
		if autoErr == nil {
			switch info.access {
			case 1:
				return workspace.ProxyConfig{}, "", nil
			case 3:
				return selectWindowsProxy(target, windows.UTF16PtrToString(info.proxy), windows.UTF16PtrToString(info.bypass))
			default:
				return invalidSystemProxy()
			}
		}
		if pac != "" || (autoErr != windows.Errno(12180) && static == "") {
			return invalidSystemProxy()
		}
	}
	if static != "" {
		return selectWindowsProxy(target, static, bypass)
	}
	return envSystemProxy(target)
}

func resolveWindowsProxy(ctx context.Context, target, pac string, detect bool) (winProxyInfo, error) {
	var info winProxyInfo
	var options winAutoProxyOptions
	var err error
	if pac != "" {
		options.flags = 2
		options.pac, err = windows.UTF16PtrFromString(pac)
		if err != nil {
			return info, err
		}
	}
	if detect {
		options.flags |= 1
		options.detect = 3
	}
	agent, _ := windows.UTF16PtrFromString("Yaare")
	handle, _, err := proxyOpen.Call(uintptr(unsafe.Pointer(agent)), 1, 0, 0, 0)
	if handle == 0 {
		return info, err
	}
	defer proxyClose.Call(handle)
	timeout := 5000
	if deadline, ok := ctx.Deadline(); ok {
		timeout = max(1, min(timeout, int(time.Until(deadline).Milliseconds())))
	}
	proxyTimeouts.Call(handle, uintptr(timeout), uintptr(timeout), uintptr(timeout), uintptr(timeout))
	address, err := windows.UTF16PtrFromString("https://" + target + "/")
	if err != nil {
		return info, err
	}
	ok, _, err := proxyForURL.Call(handle, uintptr(unsafe.Pointer(address)), uintptr(unsafe.Pointer(&options)), uintptr(unsafe.Pointer(&info)))
	if ok != 0 {
		return info, nil
	}
	return info, err
}

func selectWindowsProxy(target, raw, bypass string) (workspace.ProxyConfig, string, error) {
	host := strings.ToLower(targetHost(target))
	for _, rule := range strings.FieldsFunc(strings.ToLower(bypass), func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
		if rule == "<local>" && !strings.ContainsAny(host, ".:") && net.ParseIP(host) == nil {
			return workspace.ProxyConfig{}, "", nil
		}
		matchesHost, _ := path.Match(rule, host)
		matchesTarget, _ := path.Match(rule, strings.ToLower(target))
		if matchesHost || matchesTarget {
			return workspace.ProxyConfig{}, "", nil
		}
	}
	var named, plain string
	choices := map[string]string{}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if key, value, ok := strings.Cut(part, "="); ok {
			choices[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		} else if plain == "" {
			plain = part
		}
	}
	for _, kind := range []string{"https", "http", "socks5", "socks"} {
		if named = choices[kind]; named != "" {
			protocol := "http"
			if strings.HasPrefix(kind, "socks") {
				protocol = "socks5"
			}
			return parseSystemProxy(named, protocol)
		}
	}
	if strings.EqualFold(plain, "DIRECT") {
		return workspace.ProxyConfig{}, "", nil
	}
	if kind, value, hasDirective := strings.Cut(plain, " "); hasDirective {
		switch strings.ToUpper(kind) {
		case "PROXY", "HTTP":
			return parseSystemProxy(strings.TrimSpace(value), "http")
		case "SOCKS", "SOCKS5":
			return parseSystemProxy(strings.TrimSpace(value), "socks5")
		default:
			return invalidSystemProxy()
		}
	}
	return parseSystemProxy(plain, "http")
}
