package lifecycle

import (
	"encoding/json"

	"github.com/egoist/mygo"

	"yaare/internal/domain/preferences"
)

type startupAppearance struct {
	Mode    string `json:"mode"`
	Palette string `json:"interfacePalette"`
}

func savedStartupAppearance(snapshot preferences.Snapshot) startupAppearance {
	appearance := startupAppearance{Mode: snapshot.Portable.Mode, Palette: snapshot.Portable.InterfacePalette}
	if appearance.Mode != "dark" && appearance.Mode != "light" {
		appearance.Mode = "system"
	}
	return appearance
}

func (appearance startupAppearance) preload() string {
	encoded, _ := json.Marshal(appearance)
	return "window.__yaareStartupAppearance = " + string(encoded) + ";\n"
}

var startupBackgrounds = map[string][2]string{
	"neutral": {"#eeefee", "#202122"},
	"paper":   {"#faf9f5", "#141413"},
	"purple":  {"#eeecef", "#242328"},
	"blue":    {"#ebeff1", "#22262a"},
	"green":   {"#eceee9", "#232722"},
}

func (appearance startupAppearance) background() string {
	colors, valid := startupBackgrounds[appearance.Palette]
	if !valid {
		colors = startupBackgrounds["neutral"]
	}
	switch appearance.Mode {
	case "light":
		return colors[0]
	case "dark":
		return colors[1]
	default:
		return "light-dark(" + colors[0] + ", " + colors[1] + ")"
	}
}

func (appearance startupAppearance) captionTheme() mygo.ThemeSource {
	switch appearance.Mode {
	case "dark":
		return mygo.ThemeDark
	case "light":
		return mygo.ThemeLight
	default:
		return mygo.ThemeSystem
	}
}
