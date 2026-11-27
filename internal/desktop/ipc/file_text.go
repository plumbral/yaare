package ipc

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/domain/workspace"
)

type TextSaveRequest struct {
	TargetEncoding string       `json:"targetEncoding,omitempty"`
	TargetBOM      *bool        `json:"targetBOM,omitempty"`
	Encoding       string       `json:"encoding,omitempty"`
	Location       FileLocation `json:"location"`
	Version        string       `json:"version"`
	Content        string       `json:"content"`
}

type textProtection struct {
	window    *mygo.Window
	revision  int
	protected bool
}

func (s *FileService) SetTextProtection(ctx context.Context, revision int, protected bool) error {
	page, window := callerPage(ctx), mygo.CallerWindow(ctx)
	if page == nil || window == nil || revision < 1 {
		return errors.New("无法设置远程编辑关闭保护。")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.textProtection[page]
	if revision > previous.revision {
		s.textProtection[page] = textProtection{window, revision, protected}
	}
	return nil
}

func GuardTextWindow(window *mygo.Window, s *FileService) {
	var dialogOpen atomic.Bool
	var ending atomic.Bool
	offQuit := mygo.App.OnBeforeQuit(func(event *mygo.QuitEvent) {
		if event.SessionEnding {
			ending.Store(true)
		}
	})
	window.OnClose(func(event *mygo.CloseEvent) {
		if ending.Load() {
			return
		}
		s.mu.Lock()
		protected := false
		var owner *callerScope
		for page, state := range s.textProtection {
			if state.window == window && state.protected {
				protected = true
				owner = page
				break
			}
		}
		s.mu.Unlock()
		if !protected {
			return
		}
		event.PreventDefault()
		if !dialogOpen.CompareAndSwap(false, true) {
			return
		}
		go func() {
			result, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: window, Type: mygo.MessageWarning, Title: uiText(owner, "工作尚未完成"), Message: uiText(owner, "有未保存的内容，或后台任务仍在运行。"), Detail: uiText(owner, "继续编辑可返回工作台。退出将停止后台任务，并丢弃尚未保存的内容。"), Buttons: []string{uiText(owner, "继续编辑"), uiText(owner, "放弃并退出")}, DefaultButton: 0, CancelButton: 0, DestructiveButtons: []int{1}})
			dialogOpen.Store(false)
			if err == nil && result.Button == 1 {
				s.mu.Lock()
				for page, state := range s.textProtection {
					if state.window == window {
						state.protected = false
						s.textProtection[page] = state
					}
				}
				s.mu.Unlock()
				window.Close()
			}
		}()
	})
	window.OnClosed(func() {
		offQuit()
		s.mu.Lock()
		defer s.mu.Unlock()
		for page, state := range s.textProtection {
			if state.window == window {
				delete(s.textProtection, page)
			}
		}
	})
}

func (s *FileService) ReadText(ctx context.Context, location FileLocation) (filefs.TextFile, error) {
	return s.ReadTextWithEncoding(ctx, location, "auto")
}
func (s *FileService) ReadTextWithEncoding(ctx context.Context, location FileLocation, encoding string) (filefs.TextFile, error) {
	if err := workspaceCaller(ctx); err != nil {
		return filefs.TextFile{}, err
	}
	if location.Side != "remote" {
		return filefs.TextFile{}, errors.New("在线编辑仅用于当前 SSH 会话的远程文件。")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	filesystem, closeFS, p, err := s.filesystem(ctx, callerPage(ctx), location)
	if err != nil {
		return filefs.TextFile{}, err
	}
	defer closeFS()
	file, err := filefs.ReadTextEncoding(ctx, filesystem, p, encoding)
	if err != nil {
		return filefs.TextFile{}, fileProblem(err)
	}
	return file, nil
}

func (s *FileService) SaveText(ctx context.Context, request TextSaveRequest) (filefs.TextSaveResult, error) {
	if err := workspaceCaller(ctx); err != nil {
		return filefs.TextSaveResult{}, err
	}
	if request.Location.Side != "remote" {
		return filefs.TextSaveResult{}, errors.New("在线编辑仅用于当前 SSH 会话的远程文件。")
	}
	if len(request.Content) > filefs.MaxTextBytes {
		return filefs.TextSaveResult{}, fileProblem(&filefs.TextProblem{Code: "TEXT_TOO_LARGE", Message: "在线编辑内容不能超过 2 MiB。"})
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case s.textWrites <- struct{}{}:
		defer func() { <-s.textWrites }()
	case <-ctx.Done():
		return filefs.TextSaveResult{}, fileProblem(ctx.Err())
	case <-s.stop:
		return filefs.TextSaveResult{}, errors.New("文件服务已关闭。")
	}
	filesystem, closeFS, p, err := s.filesystem(ctx, callerPage(ctx), request.Location)
	if err != nil {
		return filefs.TextSaveResult{}, err
	}
	defer closeFS()
	result, err := filefs.SaveTextConverted(ctx, filesystem, p, request.Version, request.Content, workspace.NewID(), request.Encoding, request.TargetEncoding, request.TargetBOM)
	if err != nil {
		return filefs.TextSaveResult{}, fileProblem(err)
	}
	return result, nil
}
