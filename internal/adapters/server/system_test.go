package server

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	filefs "yaare/internal/adapters/files"
)

func TestParseDetectedServerSystems(t *testing.T) {
	tests := []struct{ input, family, id, version, label string }{
		{"Linux\nNAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\n", "linux", "ubuntu", "24.04", "Ubuntu 24.04"},
		{"Linux\nNAME=\"Debian GNU/Linux\"\nID=debian\nVERSION_ID=12\nID_LIKE=ubuntu\n", "linux", "debian", "12", "Debian GNU/Linux 12"},
		{"Linux\nNAME='Rolling'\nID=arch\n", "linux", "arch", "", "Rolling"},
		{"Linux\n", "linux", "", "", "Linux"},
		{"FreeBSD\n14.1-RELEASE\n", "freebsd", "freebsd", "14.1-RELEASE", "FreeBSD 14.1-RELEASE"},
		{"Linux\nNAME=\"safe\\\" name\x1b\"\nID=custom\n", "linux", "custom", "", "safe\" name"},
	}
	for _, tc := range tests {
		got, err := parseSystem("YAARE_OS_V1\n" + tc.input)
		if err != nil || !got.Valid() || got.Family != tc.family || got.ID != tc.id || got.Version != tc.version || got.Label != tc.label {
			t.Errorf("%q: %+v %v", tc.input, got, err)
		}
	}
	if _, err := parseSystem("custom command output"); err == nil {
		t.Fatal("unrecognized response accepted")
	}
}

func TestServerSystemProbeRejectsIncompleteOrOversizedOutput(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		wait error
	}{
		{"YAARE_OS_V1\nLinux\nID=ubuntu\n", context.DeadlineExceeded},
		{"YAARE_OS_V1\nLinux\n" + strings.Repeat("x", 8192), nil},
	} {
		closed := false
		_, err := DetectSystem(context.Background(), func(_ context.Context, _ string) (filefs.RemoteCommand, error) {
			return filefs.RemoteCommand{
				Output: io.NopCloser(strings.NewReader(tc.raw)),
				Wait:   func() error { return tc.wait },
				Close:  func() { closed = true },
			}, nil
		})
		if err == nil || !closed {
			t.Fatal("incomplete system probe accepted or channel left open", err)
		}
	}
	_, err := DetectSystem(context.Background(), func(context.Context, string) (filefs.RemoteCommand, error) {
		return filefs.RemoteCommand{}, context.DeadlineExceeded
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("open timeout lost", err)
	}
}
