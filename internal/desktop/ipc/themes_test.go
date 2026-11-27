package ipc

import (
	"encoding/json"
	"testing"
	"yaare/internal/domain/preferences"
)

func TestPortableThemePreferences(t *testing.T) {
	for _, ui := range []string{"neutral", "paper", "purple", "blue", "green"} {
		for _, terminal := range []string{"auto", "solarized-light", "solarized-dark", "solarized-auto", "tokyo-day", "tokyo-night", "tokyo-auto",
			"catppuccin-latte", "catppuccin-mocha", "catppuccin-auto", "gruvbox-light", "gruvbox-dark", "gruvbox-auto", "ayu-light", "ayu-dark", "ayu-auto"} {
			input, _ := json.Marshal(map[string]string{"interfacePalette": ui, "terminalPalette": terminal, "mode": "system"})
			result, err := preferences.DecodePortable(input)
			wantUI, wantTerminal := ui, terminal
			if wantUI == "neutral" {
				wantUI = ""
			}
			if wantTerminal == "auto" {
				wantTerminal = ""
			}
			if err != nil || result.InterfacePalette != wantUI || result.TerminalPalette != wantTerminal || result.Mode != "" {
				t.Fatalf("theme selection did not survive portable decode: %+v %v", result, err)
			}
		}
	}
	for _, input := range []string{`{"interfacePalette":true}`, `{"terminalPalette":null}`, `{"interfacePalette":"future"}`, `{"terminalPalette":"url(secret)"}`, `{"terminalPalette":"dracula"}`, `{"terminalPalette":"nord"}`} {
		if _, err := preferences.DecodePortable(json.RawMessage(input)); err == nil {
			t.Fatal("invalid theme accepted:", input)
		}
	}
}
