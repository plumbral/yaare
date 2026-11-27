package sshclient

import "testing"

func TestAutomaticProxyEnvironmentAndBypass(t *testing.T) {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(key, "")
	}
	t.Setenv("ALL_PROXY", "socks5h://fixture:sample@127.0.0.1:1080")
	t.Setenv("NO_PROXY", ".internal.test")
	proxy, password, err := envSystemProxy("target.example.test:22")
	if err != nil || proxy.Kind != "socks5" || proxy.Port != 1080 || !proxy.UseAuthentication || password != "sample" {
		t.Fatal("automatic proxy did not resolve the SOCKS route", err)
	}
	proxy, _, err = envSystemProxy("host.internal.test:2222")
	if err != nil || proxy.Kind != "" {
		t.Fatal("automatic proxy ignored the bypass", err)
	}
	t.Setenv("HTTPS_PROXY", "https://unsupported.example.test:443")
	if _, _, err = envSystemProxy("target.example.test:22"); err == nil {
		t.Fatal("unsupported configured proxy silently used a direct route")
	}
}
