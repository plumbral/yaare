package lifecycle

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidationSessionPreservesPlatformPathIdentity(t *testing.T) {
	upper, lower := filepath.Join(t.TempDir(), "Workspace"), filepath.Join(t.TempDir(), "workspace")
	lower = filepath.Join(filepath.Dir(upper), "workspace")
	if got := validationSessionName(filepath.Join(upper, ".", "child", "..")); got != validationSessionName(upper) {
		t.Fatal("equivalent paths changed page storage")
	}
	same := validationSessionName(upper) == validationSessionName(lower)
	if same != (runtime.GOOS == "windows") {
		t.Fatal("validation page storage ignores platform case rules")
	}
}
