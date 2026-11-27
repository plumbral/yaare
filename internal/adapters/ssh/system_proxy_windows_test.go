package sshclient

import "testing"

func TestWindowsProxySelectionUsesSystemRouteAndExceptions(t *testing.T) {
	const settings = "http=proxy.example.test:8080;https=secure.example.test:7890;socks=127.0.0.1:1080"
	proxy, _, err := selectWindowsProxy("target.example.test:22", settings, "<local>;*.internal.test")
	if err != nil || proxy.Kind != "http" || proxy.Address != "secure.example.test" || proxy.Port != 7890 {
		t.Fatal("Windows HTTPS mapping did not pick the CONNECT route", err)
	}
	for _, target := range []string{"dev:22", "site.internal.test:22"} {
		proxy, _, err := selectWindowsProxy(target, settings, "<local>;*.internal.test")
		if err != nil || proxy.Kind != "" {
			t.Fatal("Windows proxy bypass not honored", err)
		}
	}
	proxy, _, err = selectWindowsProxy("target.example.test:22", "SOCKS5 127.0.0.1:1080;DIRECT", "")
	if err != nil || proxy.Kind != "socks5" || proxy.Port != 1080 {
		t.Fatal("PAC SOCKS route did not take precedence over DIRECT", err)
	}
}
