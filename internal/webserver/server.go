package webserver

import (
	"context"

	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"yaare/internal/adapters/configsync"

	"strings"
	"sync"
	"sync/atomic"
	"time"
	"yaare/internal/desktop/ipc"
)

type Config struct{ Root, Assets, Origin, User, PasswordFile, KeyFile, ResetPasswordFile string }
type Server struct {
	objects     *objectStore
	config      Config
	auth        *authentication
	services    *ipc.BrowserServices
	number      atomic.Uint64
	connections sync.WaitGroup
	closing     bool
	closeOnce   sync.Once
	downloadMu  sync.Mutex
	downloads   map[string]downloadTicket
}

func Open(ctx context.Context, c Config) (*Server, error) {
	parsed, e := url.Parse(c.Origin)
	if e != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" || parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, errors.New("请设置完整的服务地址，例如 https://ssh.example.com。")
	}
	if parsed.Scheme == "http" && parsed.Hostname() != "localhost" && !net.ParseIP(parsed.Hostname()).IsLoopback() {
		return nil, errors.New("远程服务地址必须使用 HTTPS。")
	}
	c.Origin = strings.TrimSuffix(c.Origin, "/")
	if !filepath.IsAbs(c.Root) || !filepath.IsAbs(c.Assets) {
		return nil, errors.New("服务数据和静态资源目录需要绝对路径。")
	}
	vault, auth, e := instanceCredentials(c, false, nil)
	if e != nil {
		return nil, e
	}
	services, e := ipc.NewBrowserServices(func() (string, error) { return c.Root, nil }, vault)
	if e != nil {
		return nil, e
	}
	objects, e := openObjects(c.Root)
	if e != nil {
		services.Close()
		return nil, e
	}
	services.ConfigureBrowserSync(func(cfg configsync.Config, credential []byte) (*configsync.HTTP, bool, error) {
		if strings.TrimRight(cfg.Endpoint, "/") != c.Origin {
			return nil, false, nil
		}
		client, err := configsync.NewInstanceClient(cfg, credential, instanceTransport{objects})
		return client, true, err
	})
	if e = services.Initialize(ctx); e != nil {
		objects.db.Close()
		services.Close()
		return nil, e
	}
	if c.ResetPasswordFile != "" {
		if e = auth.resetPassword(c.Root, c.ResetPasswordFile); e != nil {
			objects.db.Close()
			services.Close()
			return nil, e
		}
	}
	return &Server{objects: objects, config: c, auth: auth, services: services, downloads: map[string]downloadTicket{}}, nil
}
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.auth.mu.Lock()
		s.closing = true
		for _, session := range s.auth.sessions {
			for _, cancel := range session.cancels {
				cancel()
			}
		}
		s.auth.mu.Unlock()
		s.connections.Wait()
		s.services.Close()
		s.objects.db.Close()
	})
}
func (s *Server) sameOrigin(r *http.Request) bool { return r.Header.Get("Origin") == s.config.Origin }
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin, _ := url.Parse(s.config.Origin)
	if !strings.EqualFold(r.Host, origin.Host) {
		http.Error(w, "服务地址不匹配，请使用配置的访问地址。", 403)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' "+strings.Replace(strings.Replace(s.config.Origin, "https://", "wss://", 1), "http://", "ws://", 1)+"; worker-src 'self' blob:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")

		if r.URL.Path == "/api/devices" {
			s.devices(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/sync/v2/") {
			if !s.objects.authenticated(r) {
				w.WriteHeader(401)
				return
			}
			s.objects.serve(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/download/") {
			s.download(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/session":
			if r.Method != "GET" {
				http.Error(w, "请求方法无效。", 405)
				return
			}
			_, session := s.auth.session(r)
			if session == nil {
				writeJSON(w, map[string]bool{"authenticated": false})
				return
			}
			writeJSON(w, map[string]any{"authenticated": true, "csrf": session.csrf, "user": s.auth.account.User})
			return
		case "/api/login":
			if r.Method != "POST" || !s.sameOrigin(r) {
				http.Error(w, "请从此服务的登录页操作。", 403)
				return
			}
			s.auth.login(w, r, origin.Scheme == "https")
			return
		case "/api/logout":
			if r.Method != "POST" || !s.sameOrigin(r) {
				http.Error(w, "请求来源无效。", 403)
				return
			}
			s.auth.logout(w, r)
			return
		case "/api/ws":
			s.websocket(w, r)
			return
		default:
			http.NotFound(w, r)
			return
		}
	}
	if r.URL.Path == "/readyz" {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.objects.db.PingContext(ctx); err != nil {
			w.WriteHeader(503)
			return
		}
		writeJSON(w, map[string]string{"status": "ready"})
		return
	}
	if r.URL.Path == "/healthz" {
		writeJSON(w, map[string]string{"status": "ok"})
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "请求方法无效。", 405)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if r.URL.Path == "/manifest.webmanifest" {
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	}
	files := os.DirFS(s.config.Assets)
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	if _, e := fs.Stat(files, name); e != nil {
		http.NotFound(w, r)
		return
	}
	http.FileServer(http.FS(files)).ServeHTTP(w, r)
}

type instanceTransport struct{ objects *objectStore }

func (t instanceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.objects.serve(w, r)
	return w.Result(), nil
}
