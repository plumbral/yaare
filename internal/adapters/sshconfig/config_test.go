package sshconfig

import (
	"context"
	utf16 "golang.org/x/text/encoding/unicode"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, body string) (string, Options) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path, Options{Home: home, Username: "local"}
}
func TestConfigMatchingAndFirstValue(t *testing.T) {
	path, opts := fixture(t, `Host alpha beta
 HostName = server.example
 User "部署用户"
 Port=2222
 IdentityFile "~/.ssh/first key"
 IdentityFile ~/.ssh/%n-%r-%p
Host * !beta
 ConnectTimeout 30
 User ignored
Host *
 User fallback
 Port 22
`)
	doc, err := Read(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Hosts) != 2 {
		t.Fatal(doc)
	}
	for _, h := range doc.Hosts {
		if h.Address != "server.example" || h.Port != 2222 || h.Username != "部署用户" || h.Error != "" || len(h.IdentityFiles) != 2 {
			t.Fatalf("incorrect metadata: %+v", h)
		}
		if !strings.HasSuffix(h.IdentityFiles[1], h.Alias+"-部署用户-2222") {
			t.Fatal(h.IdentityFiles)
		}
	}
	if doc.Hosts[0].Timeout != 30 || doc.Hosts[1].Timeout != 15 {
		t.Fatal("negation ignored", doc.Hosts)
	}
	path, opts = fixture(t, "Host *\n User first\nHost alpha\n User second\n")
	doc, err = Read(path, opts)
	if err != nil || doc.Hosts[0].Username != "first" {
		t.Fatal("OpenSSH first value rule", doc, err)
	}
}
func TestConfigIncludesEncodingsAndPaths(t *testing.T) {
	path, opts := fixture(t, "Include conf.d/*.conf\nHost *\n User fallback\n")
	dir := filepath.Join(opts.Home, ".ssh", "conf.d")
	os.MkdirAll(dir, 0700)
	data, err := utf16.UTF16(utf16.LittleEndian, utf16.UseBOM).NewEncoder().Bytes([]byte("Host 中文\n HostName localhost\n IdentityFile \"~/.ssh/key with space\"\n IdentityFile ~/.ssh/100%%key\n"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "a.conf"), data, 0600)
	os.WriteFile(filepath.Join(dir, "b.conf"), append([]byte{0xef, 0xbb, 0xbf}, []byte("Host second\n HostName 127.0.0.1\n")...), 0600)
	doc, err := Read(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Hosts) != 2 || doc.Hosts[0].Alias != "中文" || doc.Hosts[0].Username != "fallback" || !strings.HasSuffix(doc.Hosts[0].IdentityFiles[1], "100%key") {
		t.Fatal(doc)
	}
	fields, err := tokens(`IdentityFile "C:\Users\name\key with space" # comment`)
	if err != nil || !slices.Equal(fields, []string{"IdentityFile", `C:\Users\name\key with space`}) {
		t.Fatal(fields, err)
	}
}
func TestConfigUnsupportedRoutingNeverExecutes(t *testing.T) {
	for _, line := range []string{"ProxyCommand dangerous-command", "LocalForward 8080 localhost:80", "CertificateFile ~/.ssh/user-cert.pub", "Match exec dangerous-command\n Port 2200"} {
		t.Run(strings.Fields(line)[0], func(t *testing.T) {
			path, opts := fixture(t, "Host sample\n HostName localhost\n "+line+"\n")
			doc, err := Read(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Hosts) != 1 || doc.Hosts[0].Error == "" {
				t.Fatal("unsupported directive silently accepted", doc)
			}
		})
	}
	path, opts := fixture(t, "Host sample\n HostName localhost\n ProxyJump gate\n IdentityFile none\n GSSAPIAuthentication yes\n")
	doc, err := Read(path, opts)
	if err != nil || len(doc.Hosts[0].IdentityFiles) != 0 || !slices.Equal(doc.Hosts[0].Jumps, []string{"gate"}) || len(doc.Hosts[0].Warnings) != 1 {
		t.Fatal(doc, err)
	}
}
func TestConfigInvalidFilesAndDefaultOnly(t *testing.T) {
	for _, body := range []string{"Host \"unterminated", "User\n", "Host x\n Port 99999", "Host x\n Port word"} {
		path, opts := fixture(t, body)
		doc, err := Read(path, opts)
		if err == nil && (len(doc.Hosts) == 0 || doc.Hosts[0].Error == "") {
			t.Fatal("invalid configuration accepted", body)
		}
	}
	path, opts := fixture(t, "Include config\n")
	if _, err := Read(path, opts); err == nil {
		t.Fatal("include cycle accepted")
	}
	path, opts = fixture(t, "Host *\n User user\n")
	doc, err := Read(path, opts)
	if err != nil || len(doc.Hosts) != 0 || len(doc.Warnings) != 1 {
		t.Fatal(doc, err)
	}
	os.WriteFile(path, []byte{0xff, 0, 0xfe, 1}, 0600)
	if _, err = Read(path, opts); err == nil {
		t.Fatal("invalid encoding accepted")
	}
}

func TestConfigCaseAliasesAndEmptyValues(t *testing.T) {
	path, options := fixture(t, "Host Sample sample\n HostName localhost\n User tester\n UserKnownHostsFile ~/.ssh/other_hosts\n")
	doc, err := Read(path, options)
	if err != nil || len(doc.Hosts) != 1 || len(doc.Hosts[0].Warnings) != 1 {
		t.Fatal(doc, err)
	}
	path, options = fixture(t, "Host sample\n User \"\"\n")
	doc, err = Read(path, options)
	if err != nil || doc.Hosts[0].Error == "" {
		t.Fatal("explicit empty User silently replaced", doc, err)
	}
}

func TestConfigMetadataMatchesInstalledOpenSSH(t *testing.T) {
	binary, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH CLI is not installed")
	}
	path, options := fixture(t, "Host alpha beta\n HostName=192.0.2.10\n User=deploy\nMatch host 192.0.2.* originalhost alpha user deploy localuser *\n Port=2222\n ConnectTimeout 17\nHost * !beta\n User ignored\nMatch originalhost beta\n Port 2211\nHost *\n Port 22\n ConnectTimeout 15\n")
	doc, err := Read(path, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range doc.Hosts {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(ctx, binary, "-G", "-F", path, host.Alias)
		output, e := command.Output()
		cancel()
		if e != nil {
			t.Fatal("OpenSSH metadata comparison", e)
		}
		metadata := map[string]string{}
		for _, line := range strings.Split(string(output), "\n") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				metadata[parts[0]] = parts[1]
			}
		}
		if host.Error != "" || metadata["hostname"] != host.Address || metadata["user"] != host.Username || metadata["port"] != strconv.Itoa(host.Port) || metadata["connecttimeout"] != strconv.Itoa(host.Timeout) {
			t.Fatal("parser disagrees with OpenSSH metadata")
		}
	}
}
