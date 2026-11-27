package ipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"yaare/internal/domain/preferences"
)

func TestDeviceFontPreferences(t *testing.T) {
	for _, raw := range []string{
		`{"uiFont":"","fontFamily":"","fallbackFont":""}`,
		`{"uiFont":"system","fontFamily":"system-mono","fallbackFont":"system"}`,
		`{"fontFamily":"system-mono"}`,
		`{"uiFont":"local:Microsoft YaHei UI","fontFamily":"local:Consolas","fallbackFont":"local:等距更紗黑體 TC"}`,
	} {
		if _, err := preferences.DecodeDevice([]byte(raw)); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`{"uiFont":"url(secret)"}`, `{"fallbackFont":"arbitrary"}`, `{"fontFamily":3}`, `{"fontFamily":"Consolas"}`, `{"fontFamily":"monospace"}`, `{"uiFont":"MiSans"}`, `{"fontFamily":"JetBrains Mono"}`, `{"fallbackFont":"Sarasa Term TC"}`, `{"fontFamily":"local:"}`, `{"uiFont":"local: Arial"}`, `{"fallbackFont":"local:A\nB"}`, `{"fontFamily":"local:` + strings.Repeat("字", 129) + `"}`} {
		if _, err := preferences.DecodeDevice([]byte(raw)); err == nil {
			t.Fatal("invalid font preference accepted:", raw)
		}
	}
	for _, raw := range []string{`{"uiFont":"Inter"}`, `{"fontFamily":"Google Sans Code"}`, `{"fallbackFont":"Maple Mono CN"}`} {
		if _, err := preferences.DecodePortable(json.RawMessage(raw)); err == nil {
			t.Fatal("portable preference accepted device-only font field:", raw)
		}
	}
}

func TestLocalFontMetadataRequiresNativeWindow(t *testing.T) {
	service := NewApplicationService(nil)
	for _, ctx := range []context.Context{context.Background(), testOwner(context.Background())} {
		if rows, err := service.ListSystemFonts(ctx); err == nil || len(rows) != 0 {
			t.Fatal("a non-native caller must not enumerate server fonts")
		}
	}
}

func TestDeviceLocalFontNamesPreserved(t *testing.T) {
	raw := `{"uiFont":"local:Custom \"UI\"","fontFamily":"local:Consolas","fallbackFont":"local:等距更紗黑體 TC"}`
	got, err := preferences.DecodeDevice([]byte(raw))
	if err != nil || got.UIFont != `local:Custom "UI"` || got.FontFamily != "local:Consolas" || got.FallbackFont != "local:等距更紗黑體 TC" {
		t.Fatal("device font names were rejected or rewritten", got, err)
	}
	if !validLocalFont("local:"+strings.Repeat("字", 128)) || validLocalFont("local:A\u0085B") {
		t.Fatal("font-name length/control validation mismatch")
	}
}
