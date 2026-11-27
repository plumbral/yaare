package preferences

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
	"yaare/internal/domain/workspace"
)

const Version = 1

type Portable struct {
	Language         string  `json:"language,omitempty"`
	Mode             string  `json:"mode,omitempty"`
	InterfacePalette string  `json:"interfacePalette,omitempty"`
	TerminalPalette  string  `json:"terminalPalette,omitempty"`
	IconStyle        string  `json:"iconStyle,omitempty"`
	FontSize         int     `json:"fontSize,omitempty"`
	Scrollback       int     `json:"scrollback,omitempty"`
	CursorBlink      bool    `json:"cursorBlink,omitempty"`
	LineHeight       float64 `json:"lineHeight,omitempty"`
}

type Device struct {
	ScreenReaderMode    bool               `json:"screenReaderMode,omitempty"`
	UIFont              string             `json:"uiFont"`
	FontFamily          string             `json:"fontFamily"`
	FallbackFont        string             `json:"fallbackFont"`
	AutoReconnect       bool               `json:"autoReconnect"`
	DetectServerSystem  bool               `json:"detectServerSystem"`
	DirectorySync       bool               `json:"directorySync"`
	TransferConcurrency int                `json:"transferConcurrency"`
	Shortcuts           map[string]*string `json:"shortcuts"`
}

type RevisionChange struct {
	Before int64 `json:"before"`
	After  int64 `json:"after"`
}

type Snapshot struct {
	WorkspaceRevision *RevisionChange `json:"workspaceRevision,omitempty"`
	Version           int             `json:"version"`
	Revision          int64           `json:"revision"`
	Portable          Portable        `json:"portable"`
	Device            Device          `json:"device"`
}

type SaveRequest struct {
	Version  int       `json:"version"`
	Revision int64     `json:"revision"`
	Portable *Portable `json:"portable,omitempty"`
	Device   *Device   `json:"device,omitempty"`
}

func Empty() Snapshot {
	return Snapshot{Version: Version, Device: Device{AutoReconnect: true, TransferConcurrency: 2, Shortcuts: map[string]*string{}}}
}

func Invalid() error {
	return workspace.Fail("PREFERENCES_INVALID", "设置包含无效值，未保存。")
}

func NormalizePortable(value Portable) (Portable, error) {
	if !slices.Contains([]string{"", "system", "en", "zh-Hans", "zh-Hant", "ja"}, value.Language) ||
		!slices.Contains([]string{"", "system", "light", "dark"}, value.Mode) ||
		!slices.Contains([]string{"", "neutral", "paper", "purple", "blue", "green"}, value.InterfacePalette) ||
		!slices.Contains([]string{"", "auto", "solarized-auto", "solarized-light", "solarized-dark", "tokyo-auto", "tokyo-day", "tokyo-night", "catppuccin-auto", "catppuccin-latte", "catppuccin-mocha", "gruvbox-auto", "gruvbox-light", "gruvbox-dark", "ayu-auto", "ayu-light", "ayu-dark"}, value.TerminalPalette) ||
		!slices.Contains([]string{"", "monochrome", "color"}, value.IconStyle) ||
		(value.FontSize != 0 && (value.FontSize < 8 || value.FontSize > 32)) ||
		!slices.Contains([]int{0, 1000, 3000, 5000}, value.Scrollback) ||
		!slices.Contains([]float64{0, 1, 1.2, 1.5, 1.8, 2}, value.LineHeight) {
		return Portable{}, Invalid()
	}
	if value.Language == "system" {
		value.Language = ""
	}
	if value.Mode == "system" {
		value.Mode = ""
	}
	if value.InterfacePalette == "neutral" {
		value.InterfacePalette = ""
	}
	if value.TerminalPalette == "auto" {
		value.TerminalPalette = ""
	}
	if value.IconStyle == "monochrome" {
		value.IconStyle = ""
	}
	if value.FontSize == 14 {
		value.FontSize = 0
	}
	if value.Scrollback == 1000 {
		value.Scrollback = 0
	}
	if value.LineHeight == 1.5 {
		value.LineHeight = 0
	}
	return value, nil
}

func DecodePortable(raw json.RawMessage) (Portable, error) {
	var fields map[string]json.RawMessage
	if !decodeObject(raw, 4096, &fields) {
		return Portable{}, Invalid()
	}
	for _, field := range fields {
		if bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			return Portable{}, Invalid()
		}
	}
	var value Portable
	if !decodeObject(raw, 4096, &value) {
		return value, Invalid()
	}
	return NormalizePortable(value)
}

func DecodeDevice(raw []byte) (Device, error) {
	value := Empty().Device
	if !decodeObject(raw, 8192, &value) || !ValidDevice(value) {
		return Device{}, Invalid()
	}
	if value.Shortcuts == nil {
		value.Shortcuts = map[string]*string{}
	}
	return value, nil
}

func decodeObject(raw []byte, limit int, value any) bool {
	if len(raw) > limit || !utf8.Valid(raw) {
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || !json.Valid(raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value) == nil && decoder.Decode(new(any)) == io.EOF
}

func validFont(value, system string) bool {
	if value == "" || value == system {
		return true
	}
	name, ok := strings.CutPrefix(value, "local:")
	return ok && name != "" && name == strings.TrimSpace(name) && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 128 &&
		!strings.ContainsFunc(name, func(char rune) bool { return char < 32 || char >= 127 && char <= 159 })
}

var shortcutIDs = []string{
	"hosts", "identities", "commands", "settings", "host-search", "new-connection", "new-tab", "close-tab", "terminal-search",
	"terminal-copy", "terminal-paste", "session-focus", "next-pane", "previous-pane", "split-right", "split-down", "maximize-pane",
	"save", "submit", "file-select-all", "file-rename", "file-back", "file-forward", "move-up", "move-down",
}
var shortcutPattern = regexp.MustCompile("^(ctrl[+])?(alt[+])?(shift[+])?(meta[+])?([a-z0-9,]|f([1-9]|1[0-9]|2[0-4])|enter|escape|left|right|up|down|home|end|pageup|pagedown|space)$")

func ValidDevice(value Device) bool {
	if value.TransferConcurrency < 1 || value.TransferConcurrency > 8 ||
		!validFont(value.UIFont, "system") || !validFont(value.FontFamily, "system-mono") || !validFont(value.FallbackFont, "system") ||
		len(value.Shortcuts) > len(shortcutIDs) {
		return false
	}
	used := map[string]bool{}
	for id, keys := range value.Shortcuts {
		if !slices.Contains(shortcutIDs, id) {
			return false
		}
		if keys == nil {
			continue
		}
		key := shortcutPattern.FindStringSubmatch(*keys)
		if len(key) == 0 || (key[1] == "" && key[2] == "" && key[4] == "" && key[6] == "" && *keys != "shift+escape") || used[*keys] ||
			slices.Contains([]string{"alt+f4", "meta+q", "meta+h", "meta+m", "meta+space", "f1", "f5", "f11", "f12", "shift+f10"}, *keys) {
			return false
		}
		used[*keys] = true
	}
	return true
}
