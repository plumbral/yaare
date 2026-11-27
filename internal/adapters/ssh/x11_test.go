package sshclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
)

func x11TestPacket(order binary.ByteOrder, fake []byte) []byte {
	data := make([]byte, 12+20+16)
	data[0] = 'l'
	if order == binary.BigEndian {
		data[0] = 'B'
	}
	order.PutUint16(data[2:], 11)
	order.PutUint16(data[6:], uint16(len(x11Auth)))
	order.PutUint16(data[8:], 16)
	copy(data[12:], x11Auth)
	copy(data[32:], fake)
	return data
}
func TestX11NonceIsVerifiedAndOnlyLocalCookieIsForwarded(t *testing.T) {
	fake, real := bytes.Repeat([]byte{0xa1}, 16), bytes.Repeat([]byte{0xb2}, 16)
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		packet := x11TestPacket(order, fake)
		out, e := x11Setup(bytes.NewReader(packet), fake, real)
		if e != nil || !bytes.Equal(out[32:], real) || bytes.Contains(out, fake) {
			t.Fatal("cookie swap", e)
		}
		out, e = x11Setup(bytes.NewReader(packet), fake, nil)
		if e != nil || len(out) != 12 || order.Uint16(out[6:]) != 0 || order.Uint16(out[8:]) != 0 {
			t.Fatal("explicit local no-auth", e)
		}
		packet[len(packet)-1] ^= 1
		if _, e = x11Setup(bytes.NewReader(packet), fake, real); e == nil {
			t.Fatal("unrecognized nonce accepted")
		}
		packet = x11TestPacket(order, fake)
		order.PutUint16(packet[6:], 65535)
		if _, e = x11Setup(bytes.NewReader(packet), fake, real); e == nil {
			t.Fatal("unbounded authentication header")
		}
	}
}
func TestX11RejectsInvalidInputWithoutOpeningAChannel(t *testing.T) {
	session := NewSession(context.Background(), nil, func(Event) error { return nil })
	defer session.Close()
	for _, options := range []X11Options{{Address: "127.0.0.1:6000", Command: "xterm", Cookie: "secret-invalid"}, {Address: "127.0.0.1:0", Command: "xterm"}, {Address: "127.0.0.1:6000", Command: strings.Repeat("x", 4097)}} {
		if _, e := session.StartX11(context.Background(), options); e == nil || strings.Contains(e.Error(), "secret-invalid") {
			t.Fatal("validation or secret disclosure", e)
		}
	}
}
