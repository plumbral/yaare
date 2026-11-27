package desktop

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestRDPConnectionFileDoesNotCacheCredentialsOrRedirectDisks(t *testing.T) {
	data, e := RDPFile("127.0.0.1:12345", `DOMAIN\中文用户`)
	if e != nil {
		t.Fatal(e)
	}
	chars := make([]uint16, (len(data)-2)/2)
	for i := range chars {
		chars[i] = binary.LittleEndian.Uint16(data[2+i*2:])
	}
	text := string(utf16.Decode(chars))
	for _, want := range []string{`username:s:DOMAIN\中文用户`, "redirectclipboard:i:0", "drivestoredirect:s:", "authentication level:i:2", "prompt for credentials:i:1"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(text, "password") {
		t.Fatal("secret in RDP file")
	}
	for _, address := range []string{"0.0.0.0:1", "localhost:22", "127.0.0.1:0"} {
		if _, e = RDPFile(address, ""); e == nil {
			t.Fatal("invalid address", address)
		}
	}
	if _, e = RDPFile("127.0.0.1:12345", "user\r\nnew-setting:i:1"); e == nil {
		t.Fatal("injected setting")
	}
}
func TestRDPTunnelIsLoopbackAndClosesIndependentChannels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	remoteClosed := make(chan struct{}, 1)
	tunnel, e := StartRDP(ctx, func(_ context.Context, target string) (net.Conn, error) {
		if target != "example.test:3389" {
			t.Fatal(target)
		}
		local, remote := net.Pipe()
		go func() { defer func() { remote.Close(); remoteClosed <- struct{}{} }(); _, _ = io.Copy(remote, remote) }()
		return local, nil
	}, "example.test:3389")
	if e != nil {
		t.Fatal(e)
	}
	defer tunnel.Close()
	conn, e := net.DialTimeout("tcp", tunnel.Address(), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, e = conn.Write([]byte("rdp-binary-\x00\xff")); e != nil {
		t.Fatal(e)
	}
	value := make([]byte, 13)
	if _, e = io.ReadFull(conn, value); e != nil || string(value) != "rdp-binary-\x00\xff" {
		t.Fatal(value, e)
	}
	tunnel.Close()
	if _, e = conn.Read(value); e == nil {
		t.Fatal("open after close")
	}
	select {
	case <-remoteClosed:
	case <-time.After(time.Second):
		t.Fatal("remote channel retained")
	}
	if !strings.HasPrefix(tunnel.Address(), "127.0.0.1:") {
		t.Fatal("not loopback")
	}
}
