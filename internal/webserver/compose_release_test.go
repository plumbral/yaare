package webserver

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/app"
	"yaare/internal/app/vault"
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
)

func TestReleaseCompose(t *testing.T) {
	image := os.Getenv("YAARE_TEST_IMAGE")
	if image == "" {
		t.Skip("set YAARE_TEST_IMAGE to test a built Docker image")
	}
	dir := t.TempDir()
	spec, err := os.ReadFile("../../deployment/docker/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), spec, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	_, port, _ := net.SplitHostPort(address)
	origin := "http://" + address
	project := "yaare-qa-" + strings.ToLower(rand.Text())
	compose := func(ctx context.Context, input string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", "compose.yaml", "-p", project}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "YAARE_IMAGE="+image, "YAARE_PORT="+port, "YAARE_ORIGIN="+origin)
		cmd.Stdin = strings.NewReader(input)
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if out, err := compose(ctx, "", "down", "--volumes", "--timeout", "3"); err != nil {
			t.Errorf("clean up %s: %v\n%s", project, err, out)
		}
	})
	run := func(input string, args ...string) {
		t.Helper()
		if out, err := compose(t.Context(), input, args...); err != nil {
			logs, _ := compose(t.Context(), "", "logs", "--tail=30")
			t.Fatalf("compose %s: %v\n%s\n%s", args[0], err, out, logs)
		}
	}
	run("Yaare-Web-Test-Only!\n", "run", "--rm", "-T", "yaare", "init", "--password-file=-")
	run("", "up", "-d", "--wait", "--wait-timeout", "120")
	s := &testInstance{origin: origin, client: &http.Client{Timeout: 15 * time.Second}}
	s.loginAs(t, "admin")
	p := protocol(t, s, t.Context())
	var report workspace.Report
	if json.Unmarshal(protocolCall(t, p, "WorkspaceService.Load"), &report) != nil || !report.Ready || len(report.Snapshot.Hosts) != 0 {
		t.Fatal("expected a fresh workspace")
	}
	host := workspace.Host{ID: "compose-qa", Name: "Compose fixture", Address: "192.0.2.1", Port: 22, Username: "test", Authentication: "password", Group: workspace.Ungrouped}
	const credential = "Compose-Saved-SSH-Secret!"
	protocolCall(t, p, "WorkspaceService.SaveConnection", map[string]any{"revision": report.Snapshot.Revision, "host": host, "rememberPassword": true, "password": credential})
	p.conn.CloseNow()
	run("", "run", "--rm", "-T", "yaare", "init")
	run("", "up", "-d", "--force-recreate", "--wait", "--wait-timeout", "120")
	s.cookie = nil
	s.loginAs(t, "admin")
	p = protocol(t, s, t.Context())
	defer p.conn.CloseNow()
	var info app.Info
	if err := json.Unmarshal(protocolCall(t, p, "ApplicationService.GetInfo"), &info); err != nil ||
		info.Platform != "linux" || info.Architecture != os.Getenv("YAARE_TEST_ARCH") || info.Version != os.Getenv("YAARE_TEST_VERSION") {
		t.Fatalf("unexpected image metadata: %+v", info)
	}
	const archivePassword = "disposable backup password"
	href := protocolString(t, p, "BrowserBackup.Export", map[string]any{"password": archivePassword, "includeCredentials": true})
	res := s.request(t, "GET", href, "", "")
	defer res.Body.Close()
	blob, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatal("backup download failed", err, res.Status)
	}
	raw, err := cloudsync.Open(blob, archivePassword)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var backup struct{ Workspace syncmodel.Payload }
	if json.Unmarshal(raw, &backup) != nil || len(backup.Workspace.HostCredentials) != 1 {
		t.Fatal("saved SSH credential missing after recreation")
	}
	saved := backup.Workspace.HostCredentials[0]
	var material vault.HostPassword
	if saved.HostID != host.ID || json.Unmarshal(saved.Material, &material) != nil || string(material.Password) != credential {
		t.Fatal("could not decrypt the original SSH credential after recreation")
	}
}
