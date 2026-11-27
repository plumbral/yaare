package workspace

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"testing"
)

func TestLocalIconCatalogAcceptedByWorkspace(t *testing.T) {
	raw, err := os.ReadFile("../../../frontend/src/shared/icons/os/paths.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]json.RawMessage
	if err = json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	vendorBytes, err := os.ReadFile("../../../frontend/src/shared/icons/cloudVendors.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(vendorBytes, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog) != len(icons) || len(catalog) < 20 {
		t.Fatal("UI catalog and persisted icon whitelist differ")
	}
	for name := range catalog {
		host := NormalizeHost(Host{ID: "icon-host", Name: "Icon", Address: "127.0.0.1", Port: 22, Username: "user", Group: Ungrouped, Icon: name, Authentication: "password"})
		snapshot := Snapshot{Groups: []string{Ungrouped}, CommandGroups: []string{Ungrouped}, Hosts: []Host{host}}
		if err := Validate(snapshot, map[string]bool{}); err != nil {
			t.Fatalf("local icon %s cannot be saved: %v", name, err)
		}
	}
}

func makeValidCustomIcon(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return CustomIconPrefix + "logo:" + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestSystemIconMappingAndAutoUpdateGuards(t *testing.T) {
	for _, tc := range []struct {
		info ServerSystem
		want string
	}{
		{ServerSystem{Family: "linux", ID: "ubuntu", Label: "Ubuntu 24.04"}, "ubuntu"},
		{ServerSystem{Family: "linux", ID: "alpine", Label: "Alpine Linux 3.20"}, "alpinelinux"},
		{ServerSystem{Family: "linux", ID: "arch", Label: "Arch Linux"}, "archlinux"},
		{ServerSystem{Family: "linux", ID: "rhel", Label: "Red Hat Enterprise Linux 9"}, "redhat"},
		{ServerSystem{Family: "linux", ID: "rocky", Label: "Rocky Linux 9"}, "rockylinux"},
		{ServerSystem{Family: "linux", ID: "alma", Label: "AlmaLinux 9"}, "almalinux"},
		{ServerSystem{Family: "linux", ID: "kali", Label: "Kali Linux"}, "kalilinux"},
		{ServerSystem{Family: "linux", ID: "mint", Label: "Linux Mint"}, "linuxmint"},
		{ServerSystem{Family: "linux", ID: "centos-stream", Label: "CentOS Stream 9"}, "centos"},
		{ServerSystem{Family: "linux", ID: "opensuse-tumbleweed", Label: "openSUSE Tumbleweed"}, "opensuse"},
		{ServerSystem{Family: "linux", ID: "raspbian", Label: "Raspbian"}, "raspberrypi"},
		{ServerSystem{Family: "linux", ID: "amzn", Label: "Amazon Linux 2023"}, "linux"},
		{ServerSystem{Family: "freebsd", ID: "freebsd", Label: "FreeBSD 14"}, "freebsd"},
		{ServerSystem{Family: "openbsd", ID: "openbsd", Label: "OpenBSD 7.6"}, "openbsd"},
		{ServerSystem{Family: "darwin", ID: "darwin", Label: "macOS 15"}, ""},
		{ServerSystem{Family: "netbsd", ID: "netbsd", Label: "NetBSD 10"}, ""},
	} {
		if got := SystemIcon(tc.info); got != tc.want {
			t.Fatalf("SystemIcon(%+v) = %q, want %q", tc.info, got, tc.want)
		}
	}

	custom := makeValidCustomIcon(t)
	if !IsSystemIcon("") || !IsSystemIcon("linux") || !IsSystemIcon("ubuntu") || !IsSystemIcon("freebsd") {
		t.Fatal("default or built-in OS icon rejected")
	}
	if IsSystemIcon("cloud-aws") || IsSystemIcon(custom) {
		t.Fatal("cloud vendor or custom icon treated as OS icon")
	}

	base := Snapshot{
		Revision:      3,
		Groups:        []string{Ungrouped},
		CommandGroups: []string{Ungrouped},
		Hosts: []Host{
			NormalizeHost(Host{ID: "default", Name: "Default", Address: "127.0.0.1", Port: 22, Username: "u", Group: Ungrouped, Authentication: "password"}),
			NormalizeHost(Host{ID: "system", Name: "System", Address: "127.0.0.2", Port: 22, Username: "u", Group: Ungrouped, Icon: "debian", Authentication: "password"}),
			NormalizeHost(Host{ID: "cloud", Name: "Cloud", Address: "127.0.0.3", Port: 22, Username: "u", Group: Ungrouped, Icon: "cloud-aws", Authentication: "password"}),
			NormalizeHost(Host{ID: "custom", Name: "Custom", Address: "127.0.0.4", Port: 22, Username: "u", Group: Ungrouped, Icon: custom, Authentication: "password"}),
		},
	}

	next, entity, err := Next(base, Change{Revision: 3, Kind: "setHostSystemIcon", ID: "default", Name: "ubuntu"}, map[string]bool{})
	if err != nil || entity != "default" || next.Hosts[0].Icon != "ubuntu" {
		t.Fatalf("default icon not updated: entity=%q icon=%q err=%v", entity, next.Hosts[0].Icon, err)
	}
	next, entity, err = Next(base, Change{Revision: 3, Kind: "setHostSystemIcon", ID: "system", Name: "alpinelinux"}, map[string]bool{})
	if err != nil || entity != "system" || next.Hosts[1].Icon != "alpinelinux" {
		t.Fatalf("system icon not updated: entity=%q icon=%q err=%v", entity, next.Hosts[1].Icon, err)
	}
	next, entity, err = Next(base, Change{Revision: 3, Kind: "setHostSystemIcon", ID: "system", Name: "debian"}, map[string]bool{})
	if err != nil || entity != "" || next.Hosts[1].Icon != "debian" {
		t.Fatalf("unchanged icon should no-op: entity=%q err=%v", entity, err)
	}
	for _, protected := range []struct {
		id   string
		want string
		idx  int
	}{{"cloud", "cloud-aws", 2}, {"custom", custom, 3}} {
		next, entity, err = Next(base, Change{Revision: 3, Kind: "setHostSystemIcon", ID: protected.id, Name: "ubuntu"}, map[string]bool{})
		if err != nil || entity != "" || next.Hosts[protected.idx].Icon != protected.want {
			t.Fatalf("protected host %s icon changed: entity=%q icon=%q err=%v", protected.id, entity, next.Hosts[protected.idx].Icon, err)
		}
	}
	if _, _, err = Next(base, Change{Revision: 3, Kind: "setHostSystemIcon", ID: "default", Name: "cloud-aws"}, map[string]bool{}); err == nil {
		t.Fatal("non-system target icon was accepted")
	}
}
