package ipc

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"time"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/domain/workspace"
)

type nativeDrop struct {
	page  *callerScope
	paths []string
	at    time.Time
}

func InstallNativeDrop(window *mygo.Window, s *FileService) {
	nativePage := window.Page()
	page := scopeForPage(nativePage)
	window.OnFileDrop(func(e *mygo.FileDropEvent) { s.captureDrop(page, e) })
	window.OnClosed(func() {
		s.releasePage(page)
		releasePageScope(nativePage)
	})
}
func (s *FileService) captureDrop(page *callerScope, e *mygo.FileDropEvent) {
	paths := append([]string(nil), e.Paths...)
	clear(e.Paths)
	if len(paths) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.drops {
		if time.Since(d.at) > 2*time.Minute {
			delete(s.drops, id)
		}
	}
	if page == nil || len(paths) > 256 || len(s.drops) >= 16 {
		return
	}
	id := "yaare-drop:" + workspace.NewID()
	s.drops[id] = nativeDrop{page, paths, time.Now()}
	e.Paths[0] = id
}
func (s *FileService) UploadDropped(ctx context.Context, token string, destination FileLocation) (string, error) {
	if err := workspaceCaller(ctx); err != nil {
		return "", err
	}
	if destination.Side != "remote" || s.ssh == nil || s.ssh.owned(ctx, destination.SessionID, destination.Attempt) == nil {
		return "", errors.New("请拖入已连接的远程文件区。")
	}
	s.mu.Lock()
	drop, ok := s.drops[token]
	if !ok || drop.page != callerPage(ctx) || time.Since(drop.at) > 2*time.Minute {
		s.mu.Unlock()
		return "", errors.New("拖放授权已过期，请重新拖入。")
	}
	delete(s.drops, token)
	s.mu.Unlock()
	source, err := filefs.OpenDropped(drop.paths)
	if err != nil {
		return "", fileProblem(err)
	}
	names := source.Names()
	source.Close()
	request := TransferRequest{Source: FileLocation{Side: "drop", Path: "/"}, Destination: destination, Names: names, Policy: "ask"}
	return s.enqueueTransfer(ctx, request, drop.paths)
}
