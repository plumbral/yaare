package webserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"yaare/internal/adapters/cloudsync"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/domain/workspace"
)

func TestReleaseFirstUseAndRecovery(t *testing.T) {
	stage := os.Getenv("YAARE_RECOVERY_RELEASE")
	if stage != "fresh" && stage != "restored" {
		t.Skip("requires isolated release containers")
	}
	origin := "http://127.0.0.1:14180"
	if stage == "restored" {
		origin = "http://127.0.0.1:14181"
	}
	s := &testInstance{origin: origin, client: &http.Client{Timeout: 10 * time.Second}}
	s.login(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	p := protocol(t, s, ctx)
	defer p.conn.CloseNow()
	host := workspace.Host{ID: "qa-recovery-server", Name: "Release recovery fixture", Address: "yaare-test-openssh", Port: 22, Username: "yaaretest", Authentication: "password", Icon: "linux", System: "Linux", Group: workspace.Ungrouped}
	var report workspace.Report
	if json.Unmarshal(protocolCall(t, p, "WorkspaceService.Load"), &report) != nil || !report.Ready {
		t.Fatal("workspace not ready")
	}
	if stage == "fresh" {
		if len(report.Snapshot.Hosts) != 0 {
			t.Fatal("requires a new test workspace")
		}
		protocolCall(t, p, "WorkspaceService.SaveConnection", map[string]any{"revision": report.Snapshot.Revision, "host": host, "rememberPassword": true, "password": "Yaare-OpenSSH-Test-Only!"})
	} else if !slices.ContainsFunc(report.Snapshot.Hosts, func(h workspace.Host) bool { return h.ID == host.ID && h.HasCredential }) {
		t.Fatal("restored host or credential absent")
	}
	const session = "release-recovery"
	id, _, err := p.start("SSHService.Connect", map[string]any{"sessionId": session, "attempt": 1, "hostId": host.ID, "address": host.Address, "port": 22, "username": host.Username, "authentication": "password", "useSavedCredential": true})
	if err != nil {
		t.Fatal(err)
	}
	for connected := false; !connected; {
		select {
		case frame := <-p.events:
			if frame.ID != id {
				continue
			}
			var event sshclient.Event
			if json.Unmarshal(frame.Data, &event) != nil {
				t.Fatal("invalid SSH event")
			}
			switch event.Type {
			case "hostkey":
				if stage != "fresh" || event.Challenge == nil || !slices.Contains(strings.Split(os.Getenv("YAARE_RECOVERY_FINGERPRINT"), ","), event.Challenge.Fingerprint) {
					t.Fatal("unexpected SSH trust challenge")
				}
				protocolCall(t, p, "SSHService.ConfirmHost", session, 1, event.Challenge.ID, "remember")
			case "failed":
				t.Fatal(event.Message)
			case "connected":
				connected = true
			}
		case <-ctx.Done():
			t.Fatal("SSH session timed out")
		}
	}
	loc := map[string]any{"side": "remote", "path": "/home/yaaretest/files", "sessionId": session, "attempt": 1}
	if !bytes.Contains(protocolCall(t, p, "FileService.List", loc), []byte(`"entries"`)) {
		t.Fatal("remote directory is unavailable")
	}
	href := protocolString(t, p, "BrowserBackup.Export", map[string]any{"password": "disposable archive password", "includeCredentials": true})
	response := s.request(t, "GET", href, "", "")
	defer response.Body.Close()
	blob, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatal("could not download archive")
	}
	raw, err := cloudsync.Open(blob, "disposable archive password")
	defer clear(raw)
	if err != nil || !bytes.Contains(raw, []byte("Release recovery fixture")) || bytes.Contains(raw, []byte("RootPath")) {
		t.Fatal("invalid portable host archive")
	}
}
