package desktop

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

type Dial func(context.Context, string) (net.Conn, error)
type RDPTunnel struct {
	listener    net.Listener
	cancel      context.CancelFunc
	mu          sync.Mutex
	connections map[net.Conn]bool
	once        sync.Once
}

func StartRDP(ctx context.Context, dial Dial, target string) (*RDPTunnel, error) {
	if _, _, e := net.SplitHostPort(target); e != nil {
		return nil, errors.New("RDP 地址无效。")
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return nil, errors.New("无法创建本机 RDP 通道。")
	}
	life, cancel := context.WithCancel(ctx)
	r := &RDPTunnel{listener: listener, cancel: cancel, connections: map[net.Conn]bool{}}
	go func() { <-life.Done(); r.Close() }()
	go func() {
		slots := make(chan struct{}, 4)
		for {
			in, e := listener.Accept()
			if e != nil {
				return
			}
			select {
			case slots <- struct{}{}:
			default:
				in.Close()
				continue
			}
			r.mu.Lock()
			r.connections[in] = true
			r.mu.Unlock()
			go func() {
				defer func() { in.Close(); r.mu.Lock(); delete(r.connections, in); r.mu.Unlock(); <-slots }()
				bounded, stop := context.WithTimeout(life, 10*time.Second)
				out, e := dial(bounded, target)
				stop()
				if e != nil {
					return
				}
				defer out.Close()
				finished := make(chan struct{})
				go func() { _, _ = io.Copy(out, in); out.Close(); close(finished) }()
				_, _ = io.Copy(in, out)
				in.Close()
				<-finished
			}()
		}
	}()
	return r, nil
}
func (r *RDPTunnel) Address() string { return r.listener.Addr().String() }
func (r *RDPTunnel) Close() {
	r.once.Do(func() {
		r.cancel()
		r.listener.Close()
		r.mu.Lock()
		defer r.mu.Unlock()
		for connection := range r.connections {
			connection.Close()
		}
	})
}

func RDPFile(address, user string) ([]byte, error) {
	host, port, e := net.SplitHostPort(address)
	n, _ := strconv.Atoi(port)
	if e != nil || host != "127.0.0.1" || n < 1 || n > 65535 || len(user) > 256 || strings.ContainsAny(user, "\x00\r\n") {
		return nil, errors.New("RDP 连接信息无效。")
	}
	lines := []string{"full address:s:" + address, "username:s:" + user, "prompt for credentials:i:1", "authentication level:i:2", "enablecredsspsupport:i:1", "redirectclipboard:i:0", "redirectprinters:i:0", "redirectsmartcards:i:0", "drivestoredirect:s:", "audiomode:i:2", "screen mode id:i:1", "desktopwidth:i:1280", "desktopheight:i:800", "smart sizing:i:1"}
	characters := utf16.Encode([]rune(strings.Join(lines, "\r\n") + "\r\n"))
	data := make([]byte, 2+2*len(characters))
	data[0], data[1] = 0xff, 0xfe
	for i, char := range characters {
		binary.LittleEndian.PutUint16(data[2+i*2:], char)
	}
	return data, nil
}
func WriteRDPFile(address, user string) (string, func(), error) {
	data, e := RDPFile(address, user)
	if e != nil {
		return "", func() {}, e
	}
	file, e := os.CreateTemp("", "yaare-desktop-*.rdp")
	if e != nil {
		return "", func() {}, errors.New("无法创建本次 RDP 连接文件。")
	}
	name := file.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, e = file.Write(data); e != nil {
		file.Close()
		cleanup()
		return "", func() {}, errors.New("RDP 连接文件写入失败。")
	}
	if e = file.Close(); e != nil {
		cleanup()
		return "", func() {}, e
	}
	return filepath.Clean(name), cleanup, nil
}
