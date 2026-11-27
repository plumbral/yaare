package ipc

import (
	"context"
	"encoding/json"
	"testing"
	"yaare/internal/domain/preferences"
)

func TestInterfaceLanguageIsPageScoped(t *testing.T) {
	a, b := testOwner(context.Background()), testOwner(context.Background())
	service := &ApplicationService{}
	for language, save := range map[string]string{"en": "Save", "zh-Hans": "保存", "zh-Hant": "儲存", "ja": "保存"} {
		if err := service.SetLanguage(a, language); err != nil {
			t.Fatal(err)
		}
		if got := uiText(callerPage(a), "保存"); got != save {
			t.Fatalf("%s: %q", language, got)
		}
		if uiLanguage(callerPage(b)) != "zh-Hans" {
			t.Fatal("language leaked to another page")
		}
	}
	for _, invalid := range []string{"system", "ja-JP", "", "unknown"} {
		before := uiLanguage(callerPage(a))
		if service.SetLanguage(a, invalid) == nil || uiLanguage(callerPage(a)) != before {
			t.Fatal("invalid language changed state")
		}
	}
	if service.SetLanguage(context.Background(), "en") == nil {
		t.Fatal("accepted unowned call")
	}
	if uiText(nil, "raw external diagnostic") != "raw external diagnostic" {
		t.Fatal("changed external text")
	}
}

func TestPortableLanguagePreference(t *testing.T) {
	for _, language := range []string{"system", "en", "zh-Hans", "zh-Hant", "ja"} {
		raw := `{"mode":"dark","language":"` + language + `"}`
		got, err := preferences.DecodePortable(json.RawMessage(raw))
		want := language
		if want == "system" {
			want = ""
		}
		if err != nil || got.Mode != "dark" || got.Language != want {
			t.Fatal(language, got, err)
		}
	}
	for _, raw := range []string{`{"language":"ja-JP"}`, `{"language":3}`, `{"language":null}`} {
		if _, err := preferences.DecodePortable(json.RawMessage(raw)); err == nil {
			t.Fatal("accepted invalid preference", raw)
		}
	}
}
