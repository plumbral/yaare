package webserver

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type account struct {
	User string `json:"user"`
	Hash []byte `json:"hash"`
}
type loginSession struct {
	csrf    string
	expires time.Time
	cancels map[uint64]func()
}
type loginFailures struct {
	count int
	until time.Time
}
type authentication struct {
	account  account
	mu       sync.Mutex
	sessions map[[32]byte]*loginSession
	failures map[string]loginFailures
	slots    chan struct{}
}

const cookieName = "yaare_session"

func randomToken() string {
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
func loadAuthentication(root, user string, password func() ([]byte, error)) (*authentication, error) {
	raw, err := readOrCreatePrivate(filepath.Join(root, "auth.json"), func() ([]byte, error) {
		if err := checkBootstrapDirectory(root, keyMarker); err != nil {
			return nil, err
		}
		if password == nil {
			return nil, errors.New("请先运行 docker compose run --rm yaare init 设置管理员密码。")
		}
		if len(user) < 1 || len(user) > 64 || strings.ContainsAny(user, "\x00\r\n") {
			return nil, errors.New("用户名须为 1–64 字节。")
		}
		pass, err := password()
		if err != nil {
			return nil, err
		}
		defer clear(pass)
		hash, err := hashOwnerPassword(pass)
		if err != nil {
			return nil, err
		}
		return json.Marshal(account{user, hash})
	})
	if err != nil {
		return nil, err
	}
	a := &authentication{sessions: map[[32]byte]*loginSession{}, failures: map[string]loginFailures{}, slots: make(chan struct{}, 2)}
	if json.Unmarshal(raw, &a.account) != nil || len(a.account.User) < 1 || len(a.account.User) > 64 || len(a.account.Hash) > 128 {
		return nil, errors.New("登录配置损坏，原文件已保留。")
	}
	if _, err = bcrypt.Cost(a.account.Hash); err != nil {
		return nil, errors.New("登录配置无效，请检查原实例数据。")
	}
	return a, nil
}
func (a *authentication) session(r *http.Request) ([32]byte, *loginSession) {
	var zero [32]byte
	cookie, e := r.Cookie(cookieName)
	if e != nil || len(cookie.Value) != 43 {
		return zero, nil
	}
	key := sha256.Sum256([]byte(cookie.Value))
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[key]
	if s != nil && time.Now().After(s.expires) {
		for _, cancel := range s.cancels {
			cancel()
		}
		delete(a.sessions, key)
		s = nil
	}
	return key, s
}
func (a *authentication) login(w http.ResponseWriter, r *http.Request, secure bool) {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "请以 JSON 提交登录。", 415)
		return
	}
	var draft struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&draft) != nil {
		http.Error(w, "登录内容无效。", 400)
		return
	}
	address, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.mu.Lock()
	now := time.Now()
	for ip, item := range a.failures {
		if now.After(item.until) {
			delete(a.failures, ip)
		}
	}
	entry := a.failures[address]
	blocked := entry.count >= 5 || len(a.failures) >= 128 && entry.count == 0
	a.mu.Unlock()
	if blocked {
		http.Error(w, "登录尝试较多，请一分钟后重试。", 429)
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		http.Error(w, "登录正在处理，请稍后重试。", 429)
		return
	}
	e := bcrypt.CompareHashAndPassword(a.account.Hash, []byte(draft.Password))
	draft.Password = ""
	if e != nil || subtle.ConstantTimeCompare([]byte(draft.User), []byte(a.account.User)) != 1 {
		a.mu.Lock()
		item := a.failures[address]
		item.count++
		item.until = now.Add(time.Minute)
		a.failures[address] = item
		a.mu.Unlock()
		http.Error(w, "用户名或密码不正确。", 401)
		return
	}
	token, csrf := randomToken(), randomToken()
	key := sha256.Sum256([]byte(token))
	a.mu.Lock()
	for id, s := range a.sessions {
		if now.After(s.expires) {
			for _, cancel := range s.cancels {
				cancel()
			}
			delete(a.sessions, id)
		}
	}
	if len(a.sessions) >= 16 {
		a.mu.Unlock()
		http.Error(w, "已登录的设备较多，请先退出其他设备。", 429)
		return
	}
	a.sessions[key] = &loginSession{csrf: csrf, expires: now.Add(12 * time.Hour), cancels: map[uint64]func(){}}
	delete(a.failures, address)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	writeJSON(w, map[string]any{"authenticated": true, "user": a.account.User, "csrf": csrf})
}
func (a *authentication) logout(w http.ResponseWriter, r *http.Request) {
	key, s := a.session(r)
	if s == nil || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Yaare-CSRF")), []byte(s.csrf)) != 1 {
		http.Error(w, "登录已失效，请重新登录。", 403)
		return
	}
	a.mu.Lock()
	for _, cancel := range s.cancels {
		cancel()
	}
	delete(a.sessions, key)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, map[string]bool{"authenticated": false})
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

func readPasswordFile(filename string) ([]byte, error) {
	if filename == "-" {
		return io.ReadAll(io.LimitReader(os.Stdin, 75))
	}
	f, e := os.Open(filename)
	if e != nil {
		return nil, errors.New("无法读取管理员密码文件。")
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 75))
}

func hashOwnerPassword(raw []byte) ([]byte, error) {
	pass := bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte("\n")), []byte("\r"))
	if len(pass) < 12 || len(pass) > 72 {
		return nil, errors.New("密码须为 12–72 字节。")
	}
	return bcrypt.GenerateFromPassword(pass, 12)
}

func (a *authentication) resetPassword(root, filename string) error {
	raw, e := readPasswordFile(filename)
	if e != nil {
		return e
	}
	defer clear(raw)
	hash, e := hashOwnerPassword(raw)
	if e != nil {
		return e
	}
	next := account{a.account.User, hash}
	file, e := os.CreateTemp(root, ".owner-reset-")
	if e != nil {
		return e
	}
	name := file.Name()
	defer os.Remove(name)
	e = json.NewEncoder(file).Encode(next)
	if e == nil {
		e = file.Sync()
	}
	ce := file.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(name, filepath.Join(root, "auth.json"))
	}
	if e == nil {
		a.account = next
	}
	return e
}
