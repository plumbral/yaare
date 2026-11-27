//go:build docker_integration

package webserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const resourceTestData = "testdata/resources"

type resourceFixture struct {
	project string
	image   string
	origin  string
	hostKey string
	signer  string
}

func (f resourceFixture) compose(ctx context.Context, input io.Reader, args ...string) (string, error) {
	flags := []string{"compose", "-p", f.project, "-f", resourceTestData + "/compose.yaml"}
	cmd := exec.CommandContext(ctx, "docker", append(flags, args...)...)
	cmd.Env = append(os.Environ(), "YAARE_TEST_SSH_IMAGE="+f.image)
	return resourceCommand(cmd, input)
}

func resourceCommand(cmd *exec.Cmd, input io.Reader) (string, error) {
	cmd.Stdin = input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s%s", cmd.Args[1], err, output, stderr.String())
	}
	return strings.TrimSpace(string(output)), nil
}

func (f resourceFixture) run(t *testing.T, input io.Reader, args ...string) string {
	t.Helper()
	out, err := f.compose(t.Context(), input, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func resourceScript(t *testing.T, path string) io.Reader {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
}

func resourcePeerImage(t *testing.T) string {
	t.Helper()
	const base = "debian:12-slim"
	const installer = "../../tests/install-fixture-packages.sh"
	const prepare = "sh -s -- openssh-server openssh-client && rm -f /etc/ssh/ssh_host_* && rm -rf /var/lib/apt/lists/*"
	docker := func(input io.Reader, args ...string) string {
		t.Helper()
		output, err := resourceCommand(exec.CommandContext(t.Context(), "docker", args...), input)
		if err != nil {
			t.Fatal(err)
		}
		return output
	}
	if exec.CommandContext(t.Context(), "docker", "image", "inspect", base).Run() != nil {
		docker(nil, "pull", base)
	}
	baseID := docker(nil, "image", "inspect", "--format", "{{.Id}}", base)
	digest := sha256.New()
	fmt.Fprintln(digest, baseID, prepare)
	io.Copy(digest, resourceScript(t, installer))
	image := fmt.Sprintf("yaare-test-ssh-agent:%x", digest.Sum(nil)[:12])
	if exec.CommandContext(t.Context(), "docker", "image", "inspect", image).Run() == nil {
		return image
	}

	t.Log("Installing OpenSSH in a disposable container for the local test image")
	create := []string{"create", "--label", "yaare.test=ssh-agent-packages"}
	install := []string{"exec", "-i"}
	for _, proxy := range []string{"HTTP_PROXY", "HTTPS_PROXY", "FTP_PROXY", "NO_PROXY", "ALL_PROXY"} {
		for _, name := range []string{proxy, strings.ToLower(proxy)} {
			create = append(create, "--env", name+"=")
			install = append(install, "--env", name)
		}
	}
	id := docker(nil, append(create, baseID, "sleep", "infinity")...)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := resourceCommand(exec.CommandContext(ctx, "docker", "rm", "-f", id), nil); err != nil {
			t.Error(err)
		}
	}()
	docker(nil, "start", id)
	docker(resourceScript(t, installer), append(install, id, "sh", "-ec", prepare)...)
	docker(nil, "stop", "--timeout", "2", id)
	docker(nil, "commit", id, image)
	return image
}

func startResourceFixture(t *testing.T) resourceFixture {
	t.Helper()
	f := resourceFixture{project: "yaare-resources-" + strings.ToLower(rand.Text()), image: resourcePeerImage(t)}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if _, err := f.compose(ctx, nil, "down", "--volumes", "--timeout", "2"); err != nil {
			t.Error(err)
		}
	})
	f.run(t, nil, "up", "--detach")
	addr := f.run(t, nil, "port", "web", "4180")
	if host, _, err := net.SplitHostPort(addr); err != nil || host != "127.0.0.1" {
		t.Fatalf("expected a published loopback address, got %q", addr)
	}
	f.origin = "http://" + addr
	t.Logf("Docker project %s, web endpoint %s", f.project, f.origin)
	f.run(t, resourceScript(t, resourceTestData+"/peer.sh"), "exec", "-T", "peer", "sh", "-s")
	fingerprint := func(path string) string {
		t.Helper()
		fields := strings.Fields(f.run(t, nil, "exec", "-T", "peer", "ssh-keygen", "-E", "sha256", "-lf", path))
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "SHA256:") {
			t.Fatalf("missing fingerprint for %s", path)
		}
		return fields[1]
	}
	f.hostKey = fingerprint("/run/yaare-ssh/host.pub")
	f.signer = fingerprint("/home/yaaretest/client.pub")
	f.startWeb(t)
	return f
}

func (f resourceFixture) startWeb(t *testing.T) {
	t.Helper()
	arch := f.run(t, nil, "exec", "-T", "web", "dpkg", "--print-architecture")
	if arch == "i386" {
		arch = "386"
	}
	binary := filepath.Join(t.TempDir(), "yaare-web")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/yaare-web")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build Linux web server: %v\n%s", err, output)
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	f.run(t, file, "exec", "-T", "web", "sh", "-ec", "cat >/tmp/yaare-web && chmod 700 /tmp/yaare-web")
	const start = `set -eu
umask 077
exec >/tmp/yaare-web.log 2>&1
mkdir /tmp/public
head -c 32 /dev/urandom >/tmp/instance.key
printf '%s' 'Yaare-Web-Test-Only!' >/tmp/password
exec /tmp/yaare-web --listen=0.0.0.0:4180 --origin="$1" --data=/tmp/data --assets=/tmp/public --user=test-user --key-file=/tmp/instance.key --password-file=/tmp/password`
	f.run(t, nil, "exec", "-T", "-d", "web", "sh", "-c", start, "sh", f.origin)
	client := http.Client{Timeout: time.Second}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		res, err := client.Get(f.origin + "/readyz")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("web server did not become ready: %s", f.run(t, nil, "exec", "-T", "web", "cat", "/tmp/yaare-web.log"))
}
