//go:build yaare_experience

package lifecycle

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

//go:embed experience_probe.js
var experienceScript string

func experienceOutput() string {
	var enabled bool
	var root string
	for _, arg := range os.Args[1:] {
		if arg == "--yaare-experience-probe" {
			enabled = true
		}
		if strings.HasPrefix(arg, "--yaare-data-dir=") {
			root = strings.TrimPrefix(arg, "--yaare-data-dir=")
		}
	}
	if !enabled || !filepath.IsAbs(root) {
		return ""
	}
	return filepath.Join(root, "experience-probe.json")
}

func experiencePageOptions(session *mygo.Session) mygo.PageOptions {
	options := mygo.PageOptions{Session: session}
	if experienceOutput() != "" {
		options.PreloadScript = experienceScript
	}
	return options
}

func installExperienceProbe(window *mygo.Window) {
	path := experienceOutput()
	if path == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	window.OnClosed(cancel)
	window.Page().OnDidFinishLoad(func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					readCtx, stop := context.WithTimeout(ctx, 800*time.Millisecond)
					value, err := window.Page().EvalContext(readCtx, "globalThis.__yaareExperienceSnapshot?.() ?? null")
					stop()
					if err != nil || value == nil {
						continue
					}
					encoded, err := json.MarshalIndent(value, "", "  ")
					if err == nil && len(encoded) <= 2*1024*1024 {
						if os.WriteFile(path+".tmp", encoded, 0600) == nil {
							_ = os.Rename(path+".tmp", path)
						}
					}
				}
			}
		}()
	})
}
