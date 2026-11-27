package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const Ungrouped = "未分组"

type Host struct {
	HasCredential  bool          `json:"hasCredential,omitempty"`
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Address        string        `json:"address"`
	Port           int           `json:"port"`
	Username       string        `json:"username"`
	Group          string        `json:"group"`
	System         string        `json:"system"`
	Icon           string        `json:"icon"`
	Authentication string        `json:"authentication"`
	KeyID          string        `json:"keyId"`
	UseAgent       bool          `json:"useAgent,omitempty"`
	IdentityID     string        `json:"identityId"`
	Notes          string        `json:"notes"`
	Pinned         bool          `json:"pinned"`
	LastUsed       int64         `json:"lastUsed"`
	JumpHostIDs    []string      `json:"jumpHostIds"`
	Forwards       []ForwardRule `json:"forwards"`
	TimeoutSeconds int           `json:"timeoutSeconds"`
	Proxy          ProxyConfig   `json:"proxy"`
}
type DeletedHost struct {
	Host     Host   `json:"host"`
	BackupID string `json:"backupId"`
	Created  string `json:"created"`
}

type Command struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Group   string `json:"group"`
	Icon    string `json:"icon,omitempty"`
}
type Snapshot struct {
	PinOrder      []string  `json:"pinOrder,omitempty"`
	Revision      int64     `json:"revision"`
	Hosts         []Host    `json:"hosts"`
	Groups        []string  `json:"groups"`
	Commands      []Command `json:"commands"`
	CommandGroups []string  `json:"commandGroups"`
}

type SavedKey struct {
	ID, Name, Format string
	Path             string `json:"-"`
	Cipher           []byte `json:"-"`
}

type KeyMeta struct {
	ID, Name, Format string
	Path             string `json:"-"`
	CipherLen        int    `json:"-"`
}
type Change struct {
	Revision      int64     `json:"revision"`
	Kind          string    `json:"kind"`
	Pinned        bool      `json:"pinned"`
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	NewName       string    `json:"newName"`
	Host          Host      `json:"host"`
	Hosts         []Host    `json:"hosts"`
	IDs           []string  `json:"ids"`
	Groups        []string  `json:"groups"`
	Commands      []Command `json:"commands"`
	Command       Command   `json:"command"`
	CommandGroups []string  `json:"commandGroups"`
}
type Result struct {
	Snapshot Snapshot `json:"snapshot"`
	EntityID string   `json:"entityId"`
	Warning  string   `json:"warning"`
}
type Report struct {
	Ready       bool     `json:"ready"`
	Snapshot    Snapshot `json:"snapshot"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	CanRecover  bool     `json:"canRecover"`
	BackupCount int      `json:"backupCount"`
	Warning     string   `json:"warning"`
}
type Error struct{ Code, Message string }

func (e *Error) Error() string        { return e.Code + ": " + e.Message }
func Fail(code, message string) error { return &Error{code, message} }
func NewID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(data[:])
}
func Empty() Snapshot {
	return Snapshot{Hosts: []Host{}, Groups: []string{Ungrouped}, CommandGroups: []string{Ungrouped}, Commands: []Command{}}
}

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var addressPattern = regexp.MustCompile(`^[a-zA-Z0-9.:[\]-]{1,253}$`)
var icons = map[string]bool{"linux": true, "ubuntu": true, "debian": true, "alpinelinux": true, "archlinux": true, "fedora": true,
	"redhat": true, "centos": true, "rockylinux": true, "almalinux": true, "opensuse": true, "gentoo": true, "nixos": true, "slackware": true, "devuan": true, "raspberrypi": true, "manjaro": true, "linuxmint": true, "kalilinux": true, "freebsd": true, "openbsd": true,
	"cloud-aws": true, "cloud-alibaba": true, "cloud-tencent": true, "cloud-oracle": true,
	"cloud-azure": true, "cloud-google": true, "cloud-huawei": true, "cloud-baidu": true,
	"cloud-volcengine": true, "cloud-jd": true, "cloud-kingsoft": true, "cloud-ucloud": true,
	"cloud-qiniu": true, "cloud-ctyun": true, "cloud-ecloud": true, "cloud-unicom": true,
	"cloud-digitalocean": true, "cloud-linode": true, "cloud-vultr": true, "cloud-hetzner": true,
	"cloud-ovh": true, "cloud-ibm": true, "cloud-cloudflare": true, "cloud-scaleway": true,
	"cloud-sakura": true, "cloud-leaseweb": true, "cloud-upcloud": true, "cloud-contabo": true,
	"cloud-hivelocity": true, "cloud-nebius": true,
}

func cleanText(s string, limit int) bool {
	return s != "" && utf8.ValidString(s) && strings.TrimSpace(s) == s && utf8.RuneCountInString(s) <= limit && !strings.ContainsFunc(s, unicode.IsControl) && !strings.ContainsAny(s, "\u2028\u2029")
}

const (
	MaxSavedKeys           = 256
	MaxKeyCipherBytes      = 2 << 20
	MaxTotalKeyCipherBytes = 16 << 20
)

func ValidKey(key SavedKey) bool {
	location := (filepath.IsAbs(key.Path) && len(key.Path) <= 32768 && !strings.ContainsRune(key.Path, 0)) || (key.Path == "" && len(key.Cipher) > 0)
	return idPattern.MatchString(key.ID) && cleanText(key.Name, 255) && location && len(key.Cipher) <= MaxKeyCipherBytes && (key.Format == "OpenSSH" || key.Format == "PEM")
}

func ValidGroup(name string) bool { return cleanText(name, 32) }
func ValidHostGroup(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) > 8 {
		return false
	}
	for _, p := range parts {
		if !cleanText(p, 32) || p == "." || p == ".." || strings.Contains(p, "\\") {
			return false
		}
	}
	return len(parts) == 1 || parts[0] != Ungrouped
}
func GroupWithin(group, parent string) bool {
	return group == parent || strings.HasPrefix(group, parent+"/")
}
func ensureGroups(groups *[]string, name string) {
	parts := strings.Split(name, "/")
	for i := range parts {
		current := strings.Join(parts[:i+1], "/")
		found := false
		for _, g := range *groups {
			if g == current {
				found = true
			}
		}
		if !found {
			*groups = append(*groups, current)
		}
	}
}
func NormalizeHost(h Host) Host {
	h.Name = strings.TrimSpace(h.Name)
	h.Address = strings.TrimSpace(h.Address)
	h.Username = strings.TrimSpace(h.Username)
	h.Group = strings.TrimSpace(h.Group)
	if h.Icon == "" {
		h.Icon = "linux"
	}
	if h.System == "" {
		h.System = "Linux"
	}
	if h.Authentication == "" {
		h.Authentication = "none"
	}
	if h.Authentication != "key" {
		h.KeyID = ""
	}
	return h
}
func Validate(snapshot Snapshot, keys map[string]bool) error {
	if snapshot.Revision < 0 || snapshot.Revision > 9007199254740990 || len(snapshot.Hosts) > 10000 || len(snapshot.Groups) > 256 || len(snapshot.Commands) > 1000 {
		return Fail("LIMIT_EXCEEDED", "配置数量或版本超出支持范围。")
	}
	groups := map[string]bool{}
	ids := map[string]bool{}
	for _, name := range snapshot.Groups {
		if !ValidHostGroup(name) || groups[name] {
			return Fail("INVALID_GROUP", "分组名称无效或重复。")
		}
		groups[name] = true
	}
	if !groups[Ungrouped] {
		return Fail("INVALID_GROUP", "缺少未分组目录。")
	}
	customIcons := map[string]bool{}
	customIconBytes := 0
	for _, h := range snapshot.Hosts {
		if !icons[h.Icon] {
			customIconBytes += len(h.Icon)
			if customIconBytes > MaxWorkspaceIconBytes {
				return Fail("ICON_LIMIT", "自定义图标总量超过 1 MiB。")
			}
			if !customIcons[h.Icon] {
				if !validCustomIcon(h.Icon) {
					return Fail("INVALID_ICON", "自定义图标无效。")
				}
				customIcons[h.Icon] = true
			}
		}
		if utf8.RuneCountInString(h.Notes) > 2048 || !utf8.ValidString(h.Notes) || strings.ContainsAny(h.Notes, "\x00\r") || h.LastUsed < 0 || h.LastUsed > 9007199254740990 {
			return Fail("INVALID_HOST", "备注或最近连接记录无效。")
		}
		if h.IdentityID != "" && !idPattern.MatchString(h.IdentityID) {
			return Fail("INVALID_IDENTITY", "身份引用无效。")
		}
		if !idPattern.MatchString(h.ID) || ids[h.ID] || !cleanText(h.Name, 64) || !addressPattern.MatchString(h.Address) || h.Port < 1 || h.Port > 65535 || !cleanText(h.Username, 64) || !groups[h.Group] || !cleanText(h.System, 64) {
			return Fail("INVALID_HOST", "请检查主机名称、地址、端口、用户名、分组或图标。")
		}
		if h.Authentication != "none" && h.Authentication != "password" && h.Authentication != "key" {
			return Fail("INVALID_AUTH", "认证方式无效。")
		}
		if h.Authentication == "key" && h.KeyID != "" && !keys[h.KeyID] {
			return Fail("KEY_UNAVAILABLE", "请选择一个已登记的私钥引用。")
		}
		if h.UseAgent && (h.Authentication != "key" || h.KeyID != "") {
			return Fail("INVALID_AUTHENTICATION", "SSH Agent 不能同时选择私钥文件。")
		}
		if h.Authentication != "key" && h.KeyID != "" {
			return Fail("INVALID_AUTH", "此认证方式不能关联私钥。")
		}
		ids[h.ID] = true
	}
	seenPins := map[string]bool{}
	for _, id := range snapshot.PinOrder {
		if !ids[id] || seenPins[id] {
			return Fail("INVALID_HOST_ORDER", "置顶顺序记录无效，原数据已保留。")
		}
		seenPins[id] = true
	}
	if err := ValidateNetworks(snapshot.Hosts); err != nil {
		return err
	}
	return validateCommandLibrary(snapshot)
}

func Next(current Snapshot, change Change, keys map[string]bool) (Snapshot, string, error) {
	if change.Revision != current.Revision {
		return current, "", Fail("CONFLICT", "配置已变更，请重新载入后再保存。")
	}
	next := Snapshot{PinOrder: append([]string(nil), current.PinOrder...), Revision: current.Revision, Hosts: append([]Host{}, current.Hosts...), Groups: append([]string{}, current.Groups...), Commands: append([]Command{}, current.Commands...), CommandGroups: append([]string{}, current.CommandGroups...)}
	entity := ""
	groupExists := func(name string) bool {
		for _, g := range next.Groups {
			if g == name {
				return true
			}
		}
		return false
	}
	switch change.Kind {
	case "saveHosts", "deleteHosts":
		requested := map[string]bool{}
		replacements := map[string]Host{}
		if change.Kind == "saveHosts" {
			for _, host := range change.Hosts {
				if requested[host.ID] {
					return current, "", Fail("INVALID_BATCH", "批量主机重复。")
				}
				requested[host.ID] = true
				replacements[host.ID] = NormalizeHost(host)
			}
		} else {
			for _, id := range change.IDs {
				if requested[id] {
					return current, "", Fail("INVALID_BATCH", "批量主机重复。")
				}
				requested[id] = true
			}
		}
		if len(requested) == 0 || len(requested) > 10000 {
			return current, "", Fail("INVALID_BATCH", "请选择需要操作的主机。")
		}
		found := 0
		next.Hosts = nil
		for _, host := range current.Hosts {
			if !requested[host.ID] {
				next.Hosts = append(next.Hosts, host)
				continue
			}
			found++
			if change.Kind == "saveHosts" {
				replacement := replacements[host.ID]
				replacement.LastUsed = host.LastUsed
				if !groupExists(replacement.Group) {
					if !ValidHostGroup(replacement.Group) {
						return current, "", Fail("INVALID_GROUP", "批量分组无效。")
					}
					ensureGroups(&next.Groups, replacement.Group)
				}
				next.Hosts = append(next.Hosts, replacement)
			}
		}
		if found != len(requested) {
			return current, "", Fail("NOT_FOUND", "部分主机已不存在，请重新选择。")
		}
		if change.Kind == "deleteHosts" {
			for _, host := range next.Hosts {
				for _, jump := range host.JumpHostIDs {
					if requested[jump] {
						return current, "", Fail("HOST_IN_USE", "所选主机仍被其他连接用作跳板机，请先解除关联。")
					}
				}
			}
		}
	case "saveHost":
		h := NormalizeHost(change.Host)
		entity = h.ID
		found := false
		if !groupExists(h.Group) {
			if !ValidHostGroup(h.Group) {
				return current, "", Fail("INVALID_GROUP", "分组最多 8 层，每层 1–32 个字符。")
			}
			ensureGroups(&next.Groups, h.Group)
		}
		for i, old := range next.Hosts {
			if old.ID == h.ID {
				h.LastUsed = old.LastUsed
				next.Hosts[i] = h
				found = true
				break
			}
		}
		if !found {
			next.Hosts = append(next.Hosts, h)
		}
	case "reorderHosts", "reorderPinnedHosts":
		if err := reorderHosts(&next, change.IDs, change.Kind == "reorderPinnedHosts"); err != nil {
			return current, "", err
		}
	case "pinHost", "markHostUsed":
		found := false
		for i := range next.Hosts {
			if next.Hosts[i].ID == change.ID {
				found = true
				if change.Kind == "pinHost" {
					next.Hosts[i].Pinned = change.Pinned
				} else {
					next.Hosts[i].LastUsed = time.Now().UnixMilli()
				}
				break
			}
		}
		if !found {
			return current, "", Fail("NOT_FOUND", "此主机配置已不存在。")
		}
	case "setHostSystemIcon":
		icon := strings.TrimSpace(change.Name)
		if icon == "" || !IsSystemIcon(icon) {
			return current, "", Fail("INVALID_ICON", "系统图标无效。")
		}
		found := false
		for i := range next.Hosts {
			if next.Hosts[i].ID == change.ID {
				found = true
				if !IsSystemIcon(next.Hosts[i].Icon) || next.Hosts[i].Icon == icon {
					return current, "", nil
				}
				next.Hosts[i].Icon = icon
				entity = next.Hosts[i].ID
				break
			}
		}
		if !found {
			return current, "", Fail("NOT_FOUND", "此主机配置已不存在。")
		}
	case "deleteHost", "duplicateHost":
		index := -1
		for i, h := range next.Hosts {
			if h.ID == change.ID {
				index = i
				break
			}
		}
		if index < 0 {
			return current, "", Fail("NOT_FOUND", "此主机配置已不存在。")
		}
		if change.Kind == "deleteHost" {
			for _, host := range next.Hosts {
				for _, jump := range host.JumpHostIDs {
					if jump == change.ID {
						return current, "", Fail("HOST_IN_USE", "此主机被用作跳板机，请先从其他连接的网络配置中移除。")
					}
				}
			}
			next.Hosts = append(next.Hosts[:index], next.Hosts[index+1:]...)
			break
		}
		h := next.Hosts[index]
		h.LastUsed = 0
		h.ID = NewID()
		if h.ID == "" {
			return current, "", Fail("INTERNAL", "无法创建主机编号。")
		}
		base := []rune(h.Name)
		names := map[string]bool{}
		for _, old := range next.Hosts {
			names[old.Name] = true
		}
		for number := 1; ; number++ {
			suffix := "（副本）"
			if number > 1 {
				suffix = "（副本 " + strconv.Itoa(number) + "）"
			}
			name := string(base[:min(len(base), 64-utf8.RuneCountInString(suffix))]) + suffix
			if !names[name] {
				h.Name = name
				break
			}
		}
		next.Hosts = append(next.Hosts, h)
		entity = h.ID
	case "reorderGroups":
		if len(change.Groups) != len(next.Groups) || slices.Index(change.Groups, Ungrouped) != slices.Index(next.Groups, Ungrouped) {
			return current, "", Fail("INVALID_GROUP", "分组排序无效。")
		}
		seen := make(map[string]bool, len(change.Groups))
		for _, name := range change.Groups {
			if seen[name] || !groupExists(name) {
				return current, "", Fail("INVALID_GROUP", "分组排序无效。")
			}
			seen[name] = true
		}
		next.Groups = slices.Clone(change.Groups)
	case "addGroup":
		name := strings.TrimSpace(change.Name)
		if !ValidHostGroup(name) {
			return current, "", Fail("INVALID_GROUP", "分组最多 8 层，每层 1–32 个字符。")
		}
		if groupExists(name) {
			return current, "", Fail("DUPLICATE_GROUP", "此分组已存在。")
		}
		ensureGroups(&next.Groups, name)
	case "renameGroup", "deleteGroup":
		name := change.Name
		if name == Ungrouped {
			return current, "", Fail("PROTECTED_GROUP", "未分组目录不能重命名或删除。")
		}
		if !groupExists(name) {
			return current, "", Fail("NOT_FOUND", "此分组已不存在。")
		}
		target := Ungrouped
		if change.Kind == "renameGroup" {
			target = strings.TrimSpace(change.NewName)
			if !ValidHostGroup(target) {
				return current, "", Fail("INVALID_GROUP", "分组最多 8 层，每层 1–32 个字符。")
			}
			if target == name {
				return current, "", nil
			}
			if groupExists(target) {
				return current, "", Fail("DUPLICATE_GROUP", "此分组已存在。")
			}
		}
		if change.Kind == "renameGroup" && GroupWithin(target, name) {
			return current, "", Fail("INVALID_GROUP", "不能将分组移入自身。")
		}
		for _, g := range next.Groups {
			if GroupWithin(g, name) && change.Kind == "renameGroup" {
				renamed := target + strings.TrimPrefix(g, name)
				if groupExists(renamed) && !GroupWithin(renamed, name) {
					return current, "", Fail("DUPLICATE_GROUP", "目标层级已存在同名分组。")
				}
			}
		}
		for i, h := range next.Hosts {
			if GroupWithin(h.Group, name) {
				if change.Kind == "renameGroup" {
					next.Hosts[i].Group = target + strings.TrimPrefix(h.Group, name)
				} else {
					next.Hosts[i].Group = Ungrouped
				}
			}
		}
		groups := []string{}
		for _, g := range next.Groups {
			if GroupWithin(g, name) {
				if change.Kind == "renameGroup" {
					groups = append(groups, target+strings.TrimPrefix(g, name))
				}
			} else {
				groups = append(groups, g)
			}
		}
		next.Groups = groups
		if change.Kind == "renameGroup" {
			ensureGroups(&next.Groups, target)
		}
	case "saveCommands":
		next.Commands = append([]Command{}, change.Commands...)
		for i, c := range next.Commands {
			next.Commands[i] = normalizeCommand(c)
		}
	default:
		var handled bool
		var err error
		entity, handled, err = changeCommandLibrary(&next, change)
		if err != nil {
			return current, "", err
		}
		if !handled {
			return current, "", Fail("INVALID_ACTION", "不支持的配置操作。")
		}
	}
	if len(next.PinOrder) > 0 {
		next.PinOrder = OrderedPins(next)
	}
	if err := Validate(next, keys); err != nil {
		return current, "", err
	}
	return next, entity, nil
}
