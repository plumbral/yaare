package ipc

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/app"
	"yaare/internal/domain/workspace"
)

type systemTestSink struct {
	ctx    context.Context
	events chan sshclient.Event
}

func (s systemTestSink) Send(event sshclient.Event) error {
	select {
	case s.events <- event:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func TestDockerServerSystemDetection(t *testing.T) {
	ubuntu, debian := os.Getenv("YAARE_TEST_OS_UBUNTU_KEY"), os.Getenv("YAARE_TEST_OS_DEBIAN_KEY")
	if ubuntu == "" || debian == "" {
		t.Skip("requires the isolated loopback Docker fixtures and independently checked fingerprints")
	}
	_, store, ctx := cloudFixture(t, 91)
	workspaceApp := app.NewWorkspace(store)
	root := t.TempDir()
	svc, shutdown := NewSSHService(nil, func() (string, error) { return root, nil })
	defer shutdown()
	ConfigureSSHWorkspace(svc, workspaceApp)

	for i, tc := range []struct {
		id, key, version string
		port             int
	}{
		{"ubuntu", ubuntu, "24.04", 24221}, {"debian", debian, "12", 24222},
	} {
		t.Run(tc.id, func(t *testing.T) {
			host := workspace.NormalizeHost(workspace.Host{ID: "system-fixture", Name: "System fixture", Address: "127.0.0.1", Port: tc.port, Username: "yaaretest", Authentication: "password", Group: workspace.Ungrouped})
			before := store.Load(ctx).Snapshot
			if _, err := workspaceApp.Apply(ctx, workspace.Change{Kind: "saveHost", Revision: before.Revision, Host: host}); err != nil {
				t.Fatal(err)
			}
			parent, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			sink := systemTestSink{ctx: parent, events: make(chan sshclient.Event, 64)}
			attempt := i + 1
			const sessionID = "system-fixture-session"
			done := make(chan error, 1)
			go func() {
				done <- svc.connectStream(parent, SSHConnectRequest{
					HostID: host.ID, SessionID: sessionID, Attempt: attempt, Address: host.Address, Port: host.Port,
					Username: host.Username, Name: host.Name, Authentication: "password", Password: "yaare-os-fixture-only", TimeoutSeconds: 10,
				}, sink)
			}()
			defer func() {
				svc.Disconnect(parent, sessionID, attempt)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					cancel()
				}
			}()
			connected, detected := false, false
			var output strings.Builder
			for {
				select {
				case <-parent.Done():
					t.Fatal("system detection or PTY output timed out")
				case event := <-sink.events:
					switch event.Type {
					case "hostkey":
						if event.Challenge.Fingerprint != tc.key {
							t.Fatal("fixture fingerprint changed")
						}
						svc.ConfirmHost(parent, sessionID, attempt, event.Challenge.ID, "once")
					case "connected":
						connected = true
						if svc.ProbeServerSystem(testOwner(parent), sessionID, attempt) != nil || svc.ProbeServerSystem(parent, sessionID, attempt+1) != nil {
							t.Fatal("another page or attempt probed the server")
						}
						info := svc.ProbeServerSystem(parent, sessionID, attempt)
						if info == nil || info.ID != tc.id || info.Version != tc.version {
							t.Fatalf("unexpected system: %+v", info)
						}
						observed, err := workspaceApp.ServerSystems(parent)
						if err != nil || observed[host.ID] != *info || observed[host.ID].Label == "" {
							t.Fatal("host list record not updated", err)
						}
						detected = true
						input := base64.StdEncoding.EncodeToString([]byte("printf 'SYSTEM_%s\\n' READY\n"))
						if !svc.Write(parent, sessionID, attempt, input) {
							t.Fatal("PTY input did not work after detection")
						}
					case "output":
						output.Write(event.Data)
						if strings.Contains(output.String(), "YAARE_OS_V1") {
							t.Fatal("system probe leaked into terminal")
						}
						if connected && detected && strings.Contains(output.String(), "SYSTEM_READY") {
							return
						}
					case "failed", "disconnected":
						t.Fatal(event.Code, event.Message)
					}
				}
			}
		})
	}
}
