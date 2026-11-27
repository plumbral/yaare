package webserver

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testInstance struct {
	*Server
	origin string
	client *http.Client
	cookie *http.Cookie
	csrf   string
}

func startTest(t *testing.T) *testInstance {
	t.Helper()
	dir := t.TempDir()
	assets := filepath.Join(dir, "public")
	os.Mkdir(assets, 0700)
	os.WriteFile(filepath.Join(assets, "index.html"), []byte("test shell"), 0600)
	password := filepath.Join(dir, "password")
	os.WriteFile(password, []byte("Yaare-Web-Test-Only!"), 0600)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	origin := "http://" + listener.Addr().String()
	cfg := Config{Root: filepath.Join(dir, "data"), Assets: assets, Origin: origin, User: "test-user", PasswordFile: password}
	cfg.KeyFile = filepath.Join(dir, "instance.key")
	if err := os.WriteFile(cfg.KeyFile, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	s, e := Open(context.Background(), cfg)
	if e != nil {
		listener.Close()
		t.Fatal(e)
	}
	server := &http.Server{Handler: s}
	go server.Serve(listener)
	t.Cleanup(func() { s.Close(); server.Close() })
	return &testInstance{Server: s, origin: origin, client: &http.Client{Timeout: 5 * time.Second}}
}
func (s *testInstance) request(t *testing.T, method, path, body, origin string) *http.Response {
	t.Helper()
	request, e := http.NewRequest(method, s.origin+path, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	request.Header.Set("Content-Type", "application/json")
	if s.cookie != nil {
		request.AddCookie(s.cookie)
	}
	response, e := s.client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	return response
}
func (s *testInstance) login(t *testing.T) {
	t.Helper()
	user := "test-user"
	if s.Server == nil && os.Getenv("YAARE_BROWSER_TEST_USER") != "" {
		user = os.Getenv("YAARE_BROWSER_TEST_USER")
	}
	s.loginAs(t, user)
}
func (s *testInstance) loginAs(t *testing.T, user string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"user": user, "password": "Yaare-Web-Test-Only!"})
	if err != nil {
		t.Fatal(err)
	}
	response := s.request(t, "POST", "/api/login", string(body), s.origin)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("login", response.Status)
	}
	var result struct {
		CSRF string `json:"csrf"`
	}
	if e := json.NewDecoder(response.Body).Decode(&result); e != nil {
		t.Fatal(e)
	}
	s.csrf = result.CSRF
	for _, cookie := range response.Cookies() {
		if cookie.Name == cookieName {
			s.cookie = cookie
		}
	}
	if s.cookie == nil || !s.cookie.HttpOnly || s.cookie.SameSite != http.SameSiteStrictMode || s.csrf == "" {
		t.Fatal("cookie security")
	}
}
func (s *testInstance) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	header := http.Header{"Origin": []string{s.origin}, "Cookie": []string{s.cookie.String()}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, e := websocket.Dial(ctx, strings.Replace(s.origin, "http://", "ws://", 1)+"/api/ws", &websocket.DialOptions{HTTPHeader: header, Subprotocols: []string{"yaare.v1", s.csrf}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}
func TestLoginOriginCookiesRPCAndLogout(t *testing.T) {
	s := startTest(t)
	response := s.request(t, "GET", "/api/session", "", "")
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(raw), `"authenticated":false`) || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("unauthenticated session")
	}
	response = s.request(t, "POST", "/api/login", `{"user":"test-user","password":"Yaare-Web-Test-Only!"}`, "https://foreign.invalid")
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross origin login")
	}
	s.login(t)
	conn := s.dial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := wsjson.Write(ctx, conn, map[string]any{"type": "call", "id": "1", "method": "WorkspaceService.Load", "args": []any{}}); e != nil {
		t.Fatal(e)
	}
	var result struct {
		Type, ID, Error string
		Data            struct{ Ready bool }
	}
	if e := wsjson.Read(ctx, conn, &result); e != nil || result.Error != "" || !result.Data.Ready {
		t.Fatal("RPC", e, result)
	}
	if e := wsjson.Write(ctx, conn, map[string]any{"type": "call", "id": "2", "method": "FileService.SelectLocal", "args": []any{}}); e != nil {
		t.Fatal(e)
	}
	if e := wsjson.Read(ctx, conn, &result); e != nil || result.Error == "" {
		t.Fatal("native picker exposed", e, result)
	}
	request, _ := http.NewRequest("POST", s.origin+"/api/logout", nil)
	request.Header.Set("Origin", s.origin)
	request.Header.Set("X-Yaare-CSRF", s.csrf)
	request.AddCookie(s.cookie)
	response, e := s.client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	if e := wsjson.Read(ctx, conn, &result); e == nil {
		t.Fatal("logout did not close connection")
	}
}
func TestWebsocketRequiresLoginCSRFAndMonotonicIDs(t *testing.T) {
	s := startTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.Replace(s.origin, "http://", "ws://", 1) + "/api/ws"
	header := http.Header{"Origin": []string{s.origin}}
	conn, response, e := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header, Subprotocols: []string{"yaare.v1"}})
	if e == nil {
		conn.CloseNow()
		t.Fatal("unauthenticated WS")
	}
	if response.StatusCode != 401 {
		t.Fatal(response.Status)
	}
	s.login(t)
	header.Set("Cookie", s.cookie.String())
	_, response, e = websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header, Subprotocols: []string{"yaare.v1"}})
	if e == nil || response.StatusCode != 403 {
		t.Fatal("missing CSRF", e)
	}
	conn = s.dial(t)
	request := map[string]any{"type": "call", "id": "1", "method": "ApplicationService.GetInfo", "args": []any{}}
	wsjson.Write(ctx, conn, request)
	var out outgoing
	if e = wsjson.Read(ctx, conn, &out); e != nil {
		t.Fatal(e)
	}
	wsjson.Write(ctx, conn, request)
	if e = wsjson.Read(ctx, conn, &out); e == nil {
		t.Fatal("replayed request accepted")
	}
}
func TestMissingServerKeyNeverReplacedForExistingDatabase(t *testing.T) {
	s := startTest(t)
	key := s.config.KeyFile
	s.Close()
	if e := os.Remove(key); e != nil {
		t.Fatal(e)
	}
	_, e := Open(context.Background(), s.config)
	if e == nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(key); !os.IsNotExist(e) {
		t.Fatal("replacement key generated")
	}
}
