package workspace

import (
	"strings"
	"testing"
)

func TestProxyMetadataValidation(t *testing.T) {
	for _, p := range []ProxyConfig{{}, {Kind: "none"}, {Kind: "socks5", Address: "proxy.internal", Port: 1080}, {Kind: "http", Address: "[::1]", Port: 8080, UseAuthentication: true, Username: "user"}} {
		if e := ValidateProxy(p); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []ProxyConfig{{Kind: "none", Address: "proxy"}, {Kind: "https", Address: "proxy", Port: 8080}, {Kind: "http", Address: "http://proxy", Port: 8080}, {Kind: "socks5", Address: "proxy", Port: 0}, {Kind: "http", Address: "proxy", Port: 8080, Username: "hidden-user"}, {Kind: "http", Address: "proxy", Port: 8080, UseAuthentication: true, Username: "a:b"}, {Kind: "socks5", Address: "proxy", Port: 1080, UseAuthentication: true, Username: strings.Repeat("中", 86)}} {
		if ValidateProxy(p) == nil {
			t.Fatal("invalid proxy accepted", p.Kind)
		}
	}
}
