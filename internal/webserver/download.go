package webserver

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"yaare/internal/desktop/ipc"
)

type downloadTicket struct {
	key     [32]byte
	owner   context.Context
	file    *ipc.BrowserDownload
	expires time.Time
}

func (s *Server) prepareDownload(owner context.Context, key [32]byte, task string) (string, error) {
	file, e := s.services.PrepareBrowserDownload(owner, task)
	if e != nil {
		return "", e
	}
	return s.issueDownload(owner, key, file)
}
func (s *Server) issueDownload(owner context.Context, key [32]byte, file *ipc.BrowserDownload) (string, error) {
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()
	for id, ticket := range s.downloads {
		if time.Now().After(ticket.expires) || ticket.owner.Err() != nil {
			delete(s.downloads, id)
		}
	}
	if len(s.downloads) >= 64 {
		return "", errors.New("正在保存的文件较多，请稍后重试。")
	}
	id := randomToken()
	s.downloads[id] = downloadTicket{key: key, owner: owner, file: file, expires: time.Now().Add(2 * time.Minute)}
	return "/api/download/" + id, nil
}
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "请求无效。", 403)
		return
	}
	key, session := s.auth.session(r)
	if session == nil {
		http.Error(w, "请先登录。", 401)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/download/")
	s.downloadMu.Lock()
	ticket, ok := s.downloads[id]
	if ok && ticket.key == key {
		delete(s.downloads, id)
	}
	s.downloadMu.Unlock()
	if !ok || ticket.key != key || time.Now().After(ticket.expires) || ticket.owner.Err() != nil {
		http.Error(w, "下载已过期，请在任务列表中重新保存到设备。", 410)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	stop := context.AfterFunc(ticket.owner, cancel)
	defer stop()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": ticket.file.Name}))
	if ticket.file.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(ticket.file.Size, 10))
	}
	if e := ticket.file.Write(ctx, w); e != nil {
		panic(http.ErrAbortHandler)
	}
}
