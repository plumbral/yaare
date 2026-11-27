package lifecycle

import (
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"yaare/internal/domain/preferences"
)

func TestStartupUsesSavedThemeBeforeLoadingPage(t *testing.T) {
	stored := preferences.Snapshot{Revision: 7, Portable: preferences.Portable{Mode: "dark", InterfacePalette: "paper", Language: "ja"}}
	appearance := savedStartupAppearance(stored)
	if appearance.background() != "#141413" || appearance.captionTheme() != mygo.ThemeDark {
		t.Fatalf("window start does not match saved appearance: %+v", appearance)
	}
	script := appearance.preload()
	if !strings.Contains(script, `"mode":"dark","interfacePalette":"paper"`) || strings.Contains(script, "language") {
		t.Fatalf("unexpected startup data: %s", script)
	}
}

func TestStartupSystemAndFirstRun(t *testing.T) {
	first := savedStartupAppearance(preferences.Snapshot{})
	if !strings.Contains(first.preload(), `"mode":"system"`) || first.captionTheme() != mygo.ThemeSystem || first.background() != "light-dark(#eeefee, #202122)" {
		t.Fatalf("first start must use system appearance: %+v", first)
	}
	saved := savedStartupAppearance(preferences.Snapshot{Revision: 2})
	if saved.background() != first.background() || !strings.Contains(saved.preload(), `"mode":"system"`) {
		t.Fatalf("saved default must use system appearance: %+v", saved)
	}
}
