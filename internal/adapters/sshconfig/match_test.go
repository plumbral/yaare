package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigMatchApplicabilityAndEffects(t *testing.T) {
	for _, test := range []struct {
		name, before, block, after string
		blocked                    bool
		port                       int
	}{
		{name: "original alias after hostname", before: "HostName public.example", block: "originalhost sample\n Port 2222", port: 2222},
		{name: "pattern list veto", block: "originalhost *,!SaMpLe\n Port 2222", port: 22},
		{name: "pattern list case insensitive", block: "originalhost S?MP*,other\n Port 2222", port: 2222},
		{name: "pattern negated criterion", block: "!originalhost other\n Port 2222", port: 2222},
		{name: "wildcards are not path globs", block: "originalhost !s[ab]mple,*\n Port 2222", port: 2222},
		{name: "resolved host", before: "HostName %h.example", block: "host sample.example\n Port 2222", port: 2222},
		{name: "host is not original alias", before: "HostName public.example", block: "host sample\n Port 2222", port: 22},
		{name: "unknown hostname expansion", before: "HostName %n.example", block: "host other\n Port 2222", blocked: true},
		{name: "hostname settings after match", block: "host sample\n Port 2222", after: "Host sample\n HostName later.example", port: 2222},
		{name: "remote user", before: "User root", block: "user root\n Port 2222", port: 2222},
		{name: "quoted remote user", before: "User 'root'", block: "user root\n Port 2222", port: 2222},
		{name: "remote user case sensitive", before: "User root", block: "user ROOT\n Port 2222", port: 22},
		{name: "user defaults to local before match", block: "user local\n Port 2222", after: "Host sample\n User deploy", port: 2222},
		{name: "local user ignores remote user", before: "User root", block: "localuser root\n Port 2222", port: 22},
		{name: "local user from import environment", before: "User root", block: "localuser local\n Port 2222", port: 2222},
		{name: "known conditions combine", before: "User root", block: "localuser local user root originalhost sample\n Port 2222", port: 2222},
		{name: "malformed extra condition", block: "originalhost sample unsupported\n Port 2222", blocked: true},
		{name: "matching block preserves an earlier value", before: "Port 2200", block: "originalhost sample\n Port 2222", port: 2200},
		{name: "empty unsupported match", block: "exec untrusted-command", port: 22},
		{name: "ignored option", block: "exec untrusted-command\n LogLevel DEBUG", port: 22},
		{name: "safe shadowed overrides", before: "Port 2200\n ProxyJump gate\n IdentityFile ~/.ssh/same", block: "exec untrusted-command\n Port 2222\n ProxyCommand untrusted-command\n IdentityFile ~/.ssh/same\n IdentityFile none", port: 2200},
		{name: "ProxyJump none does not shadow ProxyCommand", before: "ProxyJump none", block: "exec untrusted-command\n ProxyCommand untrusted-command", blocked: true},
		{name: "key is additive", before: "IdentityFile ~/.ssh/base", block: "localnetwork 192.0.2.0/24\n IdentityFile ~/.ssh/conditional", blocked: true},
		{name: "unsupported option remains unsafe", block: "tagged work\n RemoteCommand untrusted-command", blocked: true},
		{name: "unsupported match after inactive host", before: "Host absent*", block: "exec untrusted-command\n Port 2222", blocked: true},
		{name: "conjunction after exec", block: "exec 'untrusted command host sample' originalhost other\n ProxyJump gate", port: 22},
		{name: "compound with quoted command", block: "originalhost other exec \"untrusted command host sample\"\n ProxyJump gate", port: 22},
		{name: "inline condition argument", block: "exec=\"untrusted command\" originalhost=other\n ProxyJump gate", port: 22},
		{name: "exec keyword in quoted argument", block: "exec 'untrusted command originalhost other'\n ProxyJump gate", blocked: true},
		{name: "incomplete condition", block: "originalhost\n ProxyJump gate", blocked: true},
		{name: "match all remains supported", block: "ALL\n Port 2222", port: 2222},
		{name: "match all keeps earlier first value", before: "Port 2200", block: "all\n Port 2222", port: 2200},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, opts := fixture(t, "Host sample\n"+test.before+"\nMatch "+test.block+"\n"+test.after)
			doc, err := Read(path, opts)
			if err != nil || len(doc.Hosts) != 1 {
				t.Fatal(doc, err)
			}
			host := doc.Hosts[0]
			if test.blocked && host.Error != unsupportedMatch || !test.blocked && (host.Error != "" || host.Port != test.port) {
				t.Fatalf("unexpected import: %+v", host)
			}
		})
	}
}

func TestConfigMatchFinalPass(t *testing.T) {
	path, opts := fixture(t, `Host stable
 Port 2200
Match host canonical.example originalhost alpha
 Port 2222
Host alpha beta stable
 HostName canonical.example
Host canonical*.example
 ConnectTimeout 40
Match final all
 LogLevel DEBUG
Match canonical originalhost beta
 IdentityFile ~/.ssh/canonical
Match !final originalhost other
 ConnectTimeout 30
`)
	doc, err := Read(path, opts)
	if err != nil || len(doc.Hosts) != 3 {
		t.Fatal(doc, err)
	}
	stable, alpha, beta := doc.Hosts[0], doc.Hosts[1], doc.Hosts[2]
	if stable.Alias != "stable" || stable.Error != "" || stable.Port != 2200 || stable.Timeout != 40 ||
		alpha.Alias != "alpha" || alpha.Error != "" || alpha.Port != 2222 || alpha.Timeout != 40 ||
		beta.Alias != "beta" || beta.Error != "" || beta.Port != 22 || len(beta.IdentityFiles) != 1 {
		t.Fatal(doc)
	}
}

func TestConfigMatchScopedIncludes(t *testing.T) {
	path, opts := fixture(t, `Host affected safe
Match originalhost affected
 Include 'conditional.conf'
Match originalhost safe exec "untrusted command"
 Include harmless.conf
Match originalhost unrelated
 Include %n.conf
Host affected safe
 Include resets-in-child.conf
 ConnectTimeout 30
`)
	for name, body := range map[string]string{
		"conditional.conf":     "Match all\n ProxyCommand untrusted-command\nHost hidden\n Port 2222\n",
		"harmless.conf":        "Host safe\n LogLevel DEBUG\n",
		"resets-in-child.conf": "Host absent*\n ConnectTimeout 60\n",
	} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := Read(path, opts)
	if err != nil || len(doc.Hosts) != 2 || doc.Hosts[0].Error != "ProxyCommand 尚不支持导入；不会执行或改为直连。" || doc.Hosts[1].Error != "" || doc.Hosts[1].Timeout != 30 {
		t.Fatal(doc, err)
	}
	path, opts = fixture(t, "Host sample\n Match originalhost sample\n Include %n.conf\n")
	doc, err = Read(path, opts)
	if err != nil || len(doc.Hosts) != 1 || doc.Hosts[0].Error != "包含尚不支持导入的 Include 路径变量，请使用固定路径。" {
		t.Fatal(doc, err)
	}
}
