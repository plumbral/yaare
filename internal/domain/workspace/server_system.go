package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

type ServerSystem struct {
	Family  string `json:"family"`
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
	Label   string `json:"label"`
}

var systemPart = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+~-]{0,31}$`)

func ValidSystemPart(s string) bool { return systemPart.MatchString(s) }

func (s ServerSystem) Valid() bool {
	switch s.Family {
	case "linux", "freebsd", "openbsd", "netbsd", "darwin", "windows", "sunos":
	default:
		return false
	}
	return (s.ID == "" || ValidSystemPart(s.ID)) && (s.Version == "" || ValidSystemPart(s.Version)) && cleanText(s.Label, 64)
}

var systemDistroIcons = map[string]string{
	"alpine":              "alpinelinux",
	"arch":                "archlinux",
	"rhel":                "redhat",
	"rocky":               "rockylinux",
	"alma":                "almalinux",
	"centos-stream":       "centos",
	"opensuse-leap":       "opensuse",
	"opensuse-tumbleweed": "opensuse",
	"opensuse-microos":    "opensuse",
	"raspbian":            "raspberrypi",
	"raspberry-pi-os":     "raspberrypi",
	"kali":                "kalilinux",
	"mint":                "linuxmint",
}

func IsSystemIcon(icon string) bool {
	return icon == "" || (icons[icon] && !strings.HasPrefix(icon, "cloud-"))
}

func SystemIcon(info ServerSystem) string {
	if !info.Valid() {
		return ""
	}
	if info.Family == "linux" {
		id := strings.ToLower(info.ID)
		if mapped, ok := systemDistroIcons[id]; ok {
			id = mapped
		}
		if id != "" && IsSystemIcon(id) {
			return id
		}
		return "linux"
	}
	if IsSystemIcon(info.Family) {
		return info.Family
	}
	return ""
}

func ServerSystemRoute(host Host, lookup func(string) (Host, bool)) string {
	type endpoint struct {
		Address, Username string
		Port              int
	}
	identity := func(h Host) endpoint {
		return endpoint{strings.ToLower(strings.Trim(h.Address, "[]")), h.Username, h.Port}
	}
	if host.Address == "" || host.Port < 1 {
		return ""
	}
	proxy := host.Proxy
	if proxy.Kind == "none" {
		proxy.Kind = ""
	}
	hops := make([]endpoint, 0, len(host.JumpHostIDs))
	for _, id := range host.JumpHostIDs {
		jump, ok := lookup(id)
		if !ok {
			return ""
		}
		hops = append(hops, identity(jump))
	}
	raw, _ := json.Marshal(struct {
		Host  endpoint
		Proxy ProxyConfig
		Jumps []endpoint
	}{identity(host), proxy, hops})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
