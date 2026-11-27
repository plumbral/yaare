package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMacVolumeNames(t *testing.T) {
	names := map[string]bool{"A": true, "a": true, "A/only": true, "a/other": true, "café": true, "cafe\u0301": true, "CON": true}
	for _, sensitive := range []bool{true, false} {
		bad := macCollidingPaths(names, func(string) bool { return sensitive })
		for _, p := range []string{"A", "a", "A/only", "a/other"} {
			if bad[p] == sensitive {
				t.Fatal("case sensitivity", sensitive, p)
			}
		}
		if !bad["café"] || !bad["cafe\u0301"] || bad["CON"] {
			t.Fatal("canonical or reserved names")
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(dir, "probe"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	root, openErr := os.OpenRoot(dir)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer root.Close()
	actual := macVolumeNameConflicts(Local{Root: root}, "/", names)
	if actual["A"] != (err == nil) {
		t.Fatal("detection disagrees with native volume behavior")
	}
}
