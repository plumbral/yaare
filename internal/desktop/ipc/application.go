package ipc

import (
	"context"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"yaare/internal/app"
)

type ApplicationService struct {
	application       *app.Application
	updates           applicationUpdateChecker
	updateMu          sync.Mutex
	lastUpdateAttempt time.Time
	lastUpdateStatus  ApplicationUpdateStatus
	releaseSource     string
	releaseMu         sync.Mutex
	releaseCheckedAt  time.Time
	releases          []githubRelease
}

func NewApplicationService(application *app.Application) *ApplicationService {
	return &ApplicationService{application: application, updates: mygo.Updater, releaseSource: yaareReleaseNotesURL}
}

func (s *ApplicationService) GetInfo() app.Info {
	info := s.application.Info()
	if version := mygo.App.Version(); version != "" {
		info.Version = version
	}
	return info
}

func (s *ApplicationService) WriteClipboardText(ctx context.Context, text string) error {
	if callerPage(ctx) == nil || mygo.CallerWindow(ctx) == nil {
		return errors.New("请在桌面窗口中复制。")
	}
	return writeClipboardText(text)
}

func writeClipboardText(text string) error {
	if len(text) > 1024*1024 || !utf8.ValidString(text) {
		return errors.New("复制内容超过 1 MB，请分段选择。")
	}
	if err := mygo.Clipboard.Write(transfer.TextData(text)); err != nil {
		return errors.New("无法写入系统剪贴板，请重试。")
	}
	return nil
}
func (s *ApplicationService) ReadClipboardText(ctx context.Context) (string, error) {
	if callerPage(ctx) == nil || mygo.CallerWindow(ctx) == nil {
		return "", errors.New("请在桌面窗口中粘贴。")
	}
	bytes, err := mygo.Clipboard.ReadFormat(transfer.Text)
	defer clear(bytes)
	if err != nil {
		return "", errors.New("剪贴板没有可用文本，或暂时被其他程序占用。")
	}
	if len(bytes) > 262144 || !utf8.Valid(bytes) {
		return "", errors.New("粘贴内容超过 256 KB 或编码无效，请分段粘贴。")
	}
	return string(bytes), nil
}

func (s *ApplicationService) SetWindowAppearance(ctx context.Context, mode string) error {
	var theme mygo.ThemeSource
	switch mode {
	case "light":
		theme = mygo.ThemeLight
	case "dark":
		theme = mygo.ThemeDark
	default:
		return errors.New("unsupported window appearance")
	}
	window := mygo.CallerWindow(ctx)
	if window == nil {
		return errors.New("calling window unavailable")
	}
	window.SetWindowButtonsTheme(theme)
	return nil
}

func (s *ApplicationService) SetWindowFullscreen(ctx context.Context, enabled bool) (bool, error) {
	window := mygo.CallerWindow(ctx)
	if window == nil || mygo.CallerPage(ctx) == nil {
		return false, errors.New("请在桌面窗口中切换全屏。")
	}
	previous := window.IsFullScreen()
	window.SetFullScreen(enabled)
	return previous, nil
}
