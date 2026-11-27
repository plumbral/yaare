package ipc

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type LocalFontFamily struct {
	Name       string `json:"name"`
	Monospaced bool   `json:"monospaced"`
}

func validLocalFont(value string) bool {
	name, ok := strings.CutPrefix(value, "local:")
	if !ok || name == "" || name != strings.TrimSpace(name) || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
		return false
	}
	return !strings.ContainsFunc(name, unicode.IsControl)
}

func (s *ApplicationService) ListSystemFonts(ctx context.Context) ([]LocalFontFamily, error) {
	if mygo.CallerPage(ctx) == nil || mygo.CallerWindow(ctx) == nil {
		return nil, errors.New("请在桌面窗口中读取本机字体。")
	}
	families := ui.FontFamilies()
	out := make([]LocalFontFamily, 0, len(families))
	for _, family := range families {
		if !family.Registered && !strings.HasPrefix(family.Name, "@") && validLocalFont("local:"+family.Name) {
			out = append(out, LocalFontFamily{Name: family.Name, Monospaced: family.Monospaced})
		}
	}
	return out, nil
}
