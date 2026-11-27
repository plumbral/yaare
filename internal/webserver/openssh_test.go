package webserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	sshclient "yaare/internal/adapters/ssh"
)

type protocolFrame struct {
	Type, ID, Error string
	Seq             uint64
	Data            json.RawMessage
}
type protocolClient struct {
	ctx       context.Context
	conn      *websocket.Conn
	mu, write sync.Mutex
	next      int
	pending   map[string]chan protocolFrame
	events    chan protocolFrame
	done      chan struct{}
}

func protocol(t *testing.T, s *testInstance, ctx context.Context) *protocolClient {
	t.Helper()
	p := &protocolClient{ctx: ctx, conn: s.dial(t), pending: map[string]chan protocolFrame{}, events: make(chan protocolFrame, 64), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		for {
			var frame protocolFrame
			if e := wsjson.Read(ctx, p.conn, &frame); e != nil {
				return
			}
			if frame.Type == "event" {
				p.send(map[string]any{"type": "ack", "id": frame.ID, "seq": frame.Seq})
				select {
				case p.events <- frame:
				case <-ctx.Done():
					return
				}
			} else {
				p.mu.Lock()
				ch := p.pending[frame.ID]
				delete(p.pending, frame.ID)
				p.mu.Unlock()
				if ch != nil {
					ch <- frame
				}
			}
		}
	}()
	return p
}
func (p *protocolClient) send(value any) error {
	p.write.Lock()
	defer p.write.Unlock()
	return wsjson.Write(p.ctx, p.conn, value)
}
func (p *protocolClient) start(method string, args ...any) (string, <-chan protocolFrame, error) {
	p.mu.Lock()
	p.next++
	id := fmt.Sprint(p.next)
	ch := make(chan protocolFrame, 1)
	p.pending[id] = ch
	p.mu.Unlock()
	e := p.send(map[string]any{"type": "call", "id": id, "method": method, "args": args})
	return id, ch, e
}
func (p *protocolClient) call(method string, args ...any) (json.RawMessage, error) {
	_, ch, e := p.start(method, args...)
	if e != nil {
		return nil, e
	}
	select {
	case result := <-ch:
		if result.Error != "" {
			return nil, fmt.Errorf("%s", result.Error)
		}
		return result.Data, nil
	case <-p.done:
		return nil, fmt.Errorf("protocol closed")
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	}
}
func protocolString(t *testing.T, p *protocolClient, method string, args ...any) string {
	t.Helper()
	raw, e := p.call(method, args...)
	if e != nil {
		t.Fatal(method, e)
	}
	var out string
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func protocolCall(t *testing.T, p *protocolClient, method string, args ...any) json.RawMessage {
	t.Helper()
	raw, e := p.call(method, args...)
	if e != nil {
		t.Fatal(method, e)
	}
	return raw
}
func waitProtocolTask(t *testing.T, p *protocolClient, id string) {
	t.Helper()
	for {
		raw := protocolCall(t, p, "FileService.Tasks")
		var result struct {
			Tasks []struct{ ID, State, Message string }
		}
		if e := json.Unmarshal(raw, &result); e != nil {
			t.Fatal(e)
		}
		for _, task := range result.Tasks {
			if task.ID == id {
				if task.State == "completed" {
					return
				}
				if task.State == "failed" || task.State == "cancelled" || task.State == "conflict" {
					t.Fatal("file task", task.State, task.Message)
				}
			}
		}
		select {
		case <-p.ctx.Done():
			t.Fatal("file task timeout")
		case <-time.After(25 * time.Millisecond):
		}
	}
}
func TestRealBrowserProtocolSSHAndSFTPBinaryUploadDownload(t *testing.T) {
	if os.Getenv("YAARE_BROWSER_TEST") != "1" {
		t.Skip("isolated Docker OpenSSH browser protocol opt-in")
	}
	var s *testInstance
	address, port := "127.0.0.1", 22222
	if endpoint := os.Getenv("YAARE_BROWSER_URL"); endpoint != "" {
		s = &testInstance{origin: endpoint, client: &http.Client{Timeout: 5 * time.Second}}
		address = "yaare-test-openssh"
		port = 22
	} else {
		s = startTest(t)
	}
	s.client.Timeout = 2 * time.Minute
	s.login(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p := protocol(t, s, ctx)
	defer p.conn.CloseNow()
	request := map[string]any{"sessionId": "browser-ssh", "attempt": 1, "address": address, "port": port, "username": "yaaretest", "authentication": "password", "password": "Yaare-OpenSSH-Test-Only!", "timeoutSeconds": 10}
	if s.Server == nil {
		raw := protocolCall(t, p, "IdentityService.List")
		var identities []struct{ ID, Name string }
		json.Unmarshal(raw, &identities)
		for _, identity := range identities {
			if identity.Name == "browser test" {
				request["identityId"] = identity.ID
				request["password"] = ""
				t.Log("saved server identity available after restart")
				break
			}
		}
	}
	connectID, _, e := p.start("SSHService.Connect", request)
	if e != nil {
		t.Fatal(e)
	}
	connected := false
	for !connected {
		select {
		case frame := <-p.events:
			if frame.ID != connectID {
				continue
			}
			var event sshclient.Event
			if e = json.Unmarshal(frame.Data, &event); e != nil {
				t.Fatal(e)
			}
			switch event.Type {
			case "hostkey":
				if event.Challenge == nil || !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+event.Challenge.Fingerprint+",") {
					t.Fatal("fixture fingerprint mismatch")
				}
				protocolCall(t, p, "SSHService.ConfirmHost", "browser-ssh", 1, event.Challenge.ID, "once")
			case "connected":
				connected = true
			case "failed":
				t.Fatal(event.Message)
			}
		case <-ctx.Done():
			t.Fatal("SSH connect timeout")
		}
	}
	command := base64.StdEncoding.EncodeToString([]byte("printf 'browser-%s\\n' terminal-ok\r"))
	protocolCall(t, p, "SSHService.Write", "browser-ssh", 1, command)
	output := ""
	for !strings.Contains(output, "browser-terminal-ok") {
		select {
		case frame := <-p.events:
			var event sshclient.Event
			json.Unmarshal(frame.Data, &event)
			if event.Type == "output" {
				output += string(event.Data)
			}
		case <-ctx.Done():
			t.Fatal("PTY output timeout")
		}
	}
	remote := map[string]any{"side": "remote", "sessionId": "browser-ssh", "attempt": 1, "path": "/home/yaaretest/files"}
	listing := protocolCall(t, p, "FileService.List", remote)
	if !bytes.Contains(listing, []byte(`"entries"`)) {
		t.Fatal("SFTP list")
	}
	other := protocol(t, s, ctx)
	defer other.conn.CloseNow()
	if _, e = other.call("FileService.List", remote); e == nil {
		t.Fatal("another browser page accessed SSH")
	}
	raw := protocolCall(t, p, "BrowserFiles.Begin")
	var folder struct{ Token string }
	json.Unmarshal(raw, &folder)
	local := map[string]any{"side": "local", "token": folder.Token, "path": "/"}
	name := fmt.Sprintf("browser-protocol-%d.bin", time.Now().UnixNano())
	size := 1 << 20
	if os.Getenv("YAARE_BROWSER_LARGE_FILE") == "1" {
		size = 192 << 20
	}
	expected := sha256.New()
	chunk := make([]byte, 256<<10)
	upload := protocolString(t, p, "BrowserFiles.UploadBegin", map[string]any{"token": folder.Token, "path": name, "size": size})
	for offset := 0; offset < size; offset += len(chunk) {
		for i := range chunk {
			chunk[i] = byte(i*13 + offset/len(chunk))
		}
		expected.Write(chunk)
		protocolCall(t, p, "BrowserFiles.UploadChunk", map[string]any{"id": upload, "offset": offset, "data": base64.StdEncoding.EncodeToString(chunk), "finish": offset+len(chunk) == size})
	}
	task := protocolString(t, p, "FileService.Transfer", map[string]any{"source": local, "destination": remote, "names": []string{name}, "policy": "ask"})
	waitProtocolTask(t, p, task)
	protocolCall(t, p, "FileService.Operate", map[string]any{"location": local, "kind": "mkdir", "name": "download"})
	destination := map[string]any{"side": "local", "token": folder.Token, "path": "/download"}
	task = protocolString(t, p, "FileService.Transfer", map[string]any{"source": remote, "destination": destination, "names": []string{name}, "policy": "ask"})
	waitProtocolTask(t, p, task)
	href := protocolString(t, p, "BrowserFiles.Download", task)
	response := s.request(t, "GET", href, "", "")
	actual := sha256.New()
	n, e := io.Copy(actual, response.Body)
	response.Body.Close()
	if e != nil || response.StatusCode != 200 || n != int64(size) || !bytes.Equal(expected.Sum(nil), actual.Sum(nil)) || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("HTTP download", response.Status, n, e)
	}
	response = s.request(t, "GET", href, "", "")
	response.Body.Close()
	if response.StatusCode != 410 {
		t.Fatal("ticket replay")
	}
	protocolCall(t, p, "FileService.Operate", map[string]any{"location": remote, "kind": "delete", "names": []string{name}, "confirmed": true})
	protocolCall(t, p, "IdentityService.Save", map[string]any{"id": fmt.Sprintf("browser-managed-%d", time.Now().UnixNano()), "name": "browser test", "username": "yaaretest", "authentication": "password", "replaceSecret": true, "password": "Yaare-OpenSSH-Test-Only!"})
	metadata := protocolCall(t, p, "IdentityService.List")
	if bytes.Contains(metadata, []byte("Yaare-OpenSSH-Test-Only!")) || bytes.Contains(metadata, []byte(`"cipher"`)) {
		t.Fatal("credential returned")
	}
	if s.Server != nil {
		entries, _ := os.ReadDir(s.config.Root)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "workspace.sqlite3") {
				data, _ := os.ReadFile(filepath.Join(s.config.Root, entry.Name()))
				if bytes.Contains(data, []byte("Yaare-OpenSSH-Test-Only!")) {
					t.Fatal("plaintext credential in database")
				}
			}
		}
	}
	protocolCall(t, p, "SSHService.Disconnect", "browser-ssh", 1)
	t.Logf("Verified OpenSSH PTY and SFTP, %d MiB round-trip SHA-256, authenticated one-use download and page isolation.", size>>20)
}
