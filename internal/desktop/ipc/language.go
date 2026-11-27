package ipc

import (
	"context"
	"errors"
	"yaare/locales"
)

func (s *ApplicationService) SetLanguage(ctx context.Context, language string) error {
	page := callerPage(ctx)
	if page == nil {
		return errors.New("页面已关闭，请重新打开。")
	}
	switch language {
	case "en", "zh-Hans", "zh-Hant", "ja":
	default:
		return errors.New("语言偏好无效。")
	}
	page.language.Store(&language)
	return nil
}

func uiText(page *callerScope, source string) string {
	return locales.Text(uiLanguage(page), source)
}

func uiLanguage(page *callerScope) string {
	language := "zh-Hans"
	if page != nil {
		if value := page.language.Load(); value != nil {
			language = *value
		}
	}
	return language
}
