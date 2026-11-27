//go:build docker_integration

package webserver

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/adapters/sshagent"
	"yaare/internal/domain/workspace"
)

func TestDockerAgentAndForwardOwnership(t *testing.T) {
	f := startResourceFixture(t)
	t.Run("plain-agent-key", func(t *testing.T) { testResourceForwards(t, f, 22, false) })
	t.Run("user-certificate", func(t *testing.T) {
		f.run(t, nil, "exec", "-T", "peer", "runuser", "-u", "yaaretest", "--", "env", "SSH_AUTH_SOCK=/run/yaare-agent/socket", "ssh-add", "/home/yaaretest/client")
		testResourceForwards(t, f, 2223, true)
	})
}

func testResourceForwards(t *testing.T, f resourceFixture, port int, certificate bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	s := &testInstance{origin: f.origin, client: &http.Client{Timeout: 4 * time.Second}}
	s.loginAs(t, "test-user")
	p := protocol(t, s, ctx)
	defer p.conn.CloseNow()
	other := protocol(t, s, ctx)
	defer other.conn.CloseNow()
	for _, method := range []string{"DesktopService.RDP", "DesktopService.X11"} {
		if _, err := p.call(method); err == nil || err.Error() != "此服务不可用。" {
			t.Fatalf("expected the browser service allowlist to reject %s, got %v", method, err)
		}
	}
	var info sshagent.AgentInfo
	if json.Unmarshal(protocolCall(t, p, "SSHService.AgentInfo"), &info) != nil || !info.Available || !slices.ContainsFunc(info.Keys, func(k sshagent.AgentKey) bool { return k.Fingerprint == f.signer }) {
		t.Fatal("dedicated agent signer not present")
	}
	hasCertificate := slices.ContainsFunc(info.Keys, func(k sshagent.AgentKey) bool { return strings.HasSuffix(k.Algorithm, "-cert-v01@openssh.com") })
	if hasCertificate != certificate {
		t.Fatalf("agent has certificate: %t, want %t", hasCertificate, certificate)
	}
	rules := []workspace.ForwardRule{
		{ID: "local", Name: "Local", Kind: "local", ListenAddress: "127.0.0.1", ListenPort: 35121, TargetAddress: "127.0.0.1", TargetPort: 22},
		{ID: "remote", Name: "Remote", Kind: "remote", ListenAddress: "127.0.0.1", ListenPort: 35122, TargetAddress: "peer", TargetPort: 22},
		{ID: "dynamic", Name: "Dynamic", Kind: "dynamic", ListenAddress: "127.0.0.1", ListenPort: 35123},
	}
	const session = "resource-test"
	id, _, e := p.start("SSHService.Connect", map[string]any{"sessionId": session, "attempt": 1, "address": "peer", "port": port, "username": "yaaretest", "authentication": "key", "useAgent": true, "forwards": rules})
	if e != nil {
		t.Fatal(e)
	}
	next := func() sshclient.Event {
		t.Helper()
		for {
			select {
			case frame := <-p.events:
				if frame.ID == id {
					var ev sshclient.Event
					if json.Unmarshal(frame.Data, &ev) != nil {
						t.Fatal("invalid event")
					}
					return ev
				}
			case <-ctx.Done():
				t.Fatal("SSH event timeout")
			case <-p.done:
				t.Fatal("workspace closed")
			}
		}
	}
	confirmedHost := false
	for {
		ev := next()
		if ev.Type == "connected" {
			if !confirmedHost {
				t.Fatal("connected without confirming the fixture host key")
			}
			break
		}
		if ev.Type == "failed" {
			t.Fatal(ev.Message)
		}
		if ev.Type == "hostkey" {
			if ev.Challenge == nil || ev.Challenge.Fingerprint != f.hostKey {
				t.Fatal("host key mismatch")
			}
			protocolCall(t, p, "SSHService.ConfirmHost", session, 1, ev.Challenge.ID, "once")
			confirmedHost = true
		}
	}
	for _, r := range rules {
		if string(protocolCall(t, other, "SSHService.SetForward", session, 1, r.ID, true)) != "false" {
			t.Fatal("another page controlled a listener")
		}
		if string(protocolCall(t, p, "SSHService.SetForward", session, 1, r.ID, true)) != "true" {
			t.Fatal("could not start listener", r.Kind)
		}
		for {
			ev := next()
			if ev.Forward == nil || ev.Forward.Rule.ID != r.ID {
				continue
			}
			if ev.Forward.State == "running" {
				break
			}
			if ev.Forward.State == "failed" {
				t.Fatal(r.Kind, ev.Forward.Message)
			}
		}
	}
	probe := func(side, state string) {
		t.Helper()
		input := resourceScript(t, resourceTestData+"/probe.sh")
		f.run(t, input, "exec", "-T", side, "timeout", "8", "bash", "-s", "--", side, state)
	}
	for _, r := range rules {
		if string(protocolCall(t, other, "SSHService.SetForward", session, 1, r.ID, false)) != "false" {
			t.Fatalf("another page could stop the active %s listener", r.Kind)
		}
	}
	probe("web", "active")
	probe("peer", "active")
	protocolCall(t, p, "SSHService.Disconnect", session, 1)
	probe("web", "closed")
	probe("peer", "closed")
}
