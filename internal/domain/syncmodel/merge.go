package syncmodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"yaare/internal/domain/preferences"
	"yaare/internal/domain/workspace"
)

type SyncConflict struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	LocalDeleted  bool   `json:"localDeleted"`
	RemoteDeleted bool   `json:"remoteDeleted"`
}
type syncEntries map[string]json.RawMessage

func (es syncEntries) clearOwned() {
	for id, raw := range es {
		if !strings.HasPrefix(id, "extension/") {
			clear(raw)
		}
	}
}

func (es syncEntries) replaceOwned(id string, raw json.RawMessage) {
	clear(es[id])
	es[id] = raw
}

type portableHostEntry struct {
	workspace.Host
	ProxyCredential *HostCredential `json:"proxyCredential,omitempty"`
	Credential      *HostCredential `json:"credential,omitempty"`
}

func (h portableHostEntry) clearMaterial() {
	if h.ProxyCredential != nil {
		clear(h.ProxyCredential.Material)
	}
	if h.Credential != nil {
		clear(h.Credential.Material)
	}
}

func entries(p Payload) syncEntries {
	out := syncEntries{}
	put := func(kind, id string, v any) { b, _ := json.Marshal(v); out.replaceOwned(kind+"/"+id, b) }
	hostIDs := []string{}
	for _, h := range p.Snapshot.Hosts {
		hostIDs = append(hostIDs, h.ID)
	}
	put("order", "hosts", hostIDs)
	put("order", "pins", workspace.OrderedPins(p.Snapshot))
	put("order", "groups", p.Snapshot.Groups)
	commandIDs := make([]string, 0, len(p.Snapshot.Commands))
	for _, c := range p.Snapshot.Commands {
		commandIDs = append(commandIDs, c.ID)
	}
	put("order", "commands", commandIDs)
	put("order", "commandGroups", p.Snapshot.CommandGroups)
	credentials := map[string]HostCredential{}
	for _, c := range p.HostCredentials {
		credentials[c.HostID] = c
	}
	proxyCredentials := map[string]HostCredential{}
	for _, c := range p.ProxyCredentials {
		proxyCredentials[c.HostID] = c
	}
	for _, h := range p.Snapshot.Hosts {
		v := portableHostEntry{Host: h}
		if c, ok := proxyCredentials[h.ID]; ok {
			v.ProxyCredential = &c
		}
		if c, ok := credentials[h.ID]; ok {
			v.Credential = &c
		}
		v.Host.HasCredential = v.Credential != nil
		put("host", h.ID, v)
	}
	for _, g := range p.Snapshot.Groups {
		put("group", g, g)
	}
	for _, g := range p.Snapshot.CommandGroups {
		put("commandGroup", g, g)
	}
	for _, c := range p.Snapshot.Commands {
		c.Icon = workspace.CommandIcon(c.Icon)
		put("command", c.ID, c)
	}
	for _, k := range p.Keys {
		put("key", k.ID, k)
	}
	for _, i := range p.Identities {
		put("identity", i.Metadata.ID, i)
	}
	var prefs map[string]json.RawMessage
	_ = json.Unmarshal(p.Preferences, &prefs)
	for k, v := range prefs {
		out["preference/"+k] = v
	}
	for k, v := range p.Extensions {
		out["extension/"+k] = v
	}
	return out
}
func fromEntries(es syncEntries) (p Payload, err error) {
	defer func() {
		if err != nil {
			ClearPayload(p)
			p = Payload{}
		}
	}()
	p = Payload{Version: Version, Snapshot: workspace.Snapshot{Groups: []string{}, CommandGroups: []string{}, Hosts: []workspace.Host{}, Commands: []workspace.Command{}}, Keys: []Key{}, Identities: []Identity{}}
	prefs := map[string]json.RawMessage{}
	extensions := workspace.PortableExtensions{}
	orders := map[string][]string{}
	keys := make([]string, 0, len(es))
	for k := range es {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, id := range keys {
		value := es[id]
		parts := strings.SplitN(id, "/", 2)
		if len(parts) != 2 {
			return p, errors.New("合并配置标识无效。")
		}
		switch parts[0] {
		case "order":
			var ids []string
			if (parts[1] != "hosts" && parts[1] != "pins" && parts[1] != "groups" && parts[1] != "commands" && parts[1] != "commandGroups") || json.Unmarshal(value, &ids) != nil || len(ids) > 10000 {
				return p, errors.New("主机排序范围无效，请刷新后重试。")
			}
			if (parts[1] == "groups" || parts[1] == "commandGroups") && len(ids) > 256 {
				return p, errors.New("分组排序无效。")
			}
			seen := map[string]bool{}
			for _, id := range ids {
				if seen[id] {
					return p, errors.New("主机排序范围无效，请刷新后重试。")
				}
				seen[id] = true
			}
			orders[parts[1]] = ids
		case "host":
			var v portableHostEntry
			if json.Unmarshal(value, &v) != nil || v.ID != parts[1] {
				v.clearMaterial()
				return p, errors.New("合并主机无效。")
			}
			p.Snapshot.Hosts = append(p.Snapshot.Hosts, v.Host)
			if v.ProxyCredential != nil {
				p.ProxyCredentials = append(p.ProxyCredentials, *v.ProxyCredential)
			}
			if v.Credential != nil {
				p.HostCredentials = append(p.HostCredentials, *v.Credential)
			}
		case "command":
			var v workspace.Command
			if json.Unmarshal(value, &v) != nil || v.ID != parts[1] {
				return p, errors.New("合并命令无效。")
			}
			p.Snapshot.Commands = append(p.Snapshot.Commands, v)
		case "group", "commandGroup":
			var v string
			if json.Unmarshal(value, &v) != nil || v != parts[1] {
				return p, errors.New("合并分组无效。")
			}
			if parts[0] == "group" {
				p.Snapshot.Groups = append(p.Snapshot.Groups, v)
			} else {
				p.Snapshot.CommandGroups = append(p.Snapshot.CommandGroups, v)
			}
		case "key":
			var v Key
			if json.Unmarshal(value, &v) != nil || v.ID != parts[1] {
				clear(v.Material)
				return p, errors.New("合并密钥无效。")
			}
			p.Keys = append(p.Keys, v)
		case "identity":
			var v Identity
			if json.Unmarshal(value, &v) != nil || v.Metadata.ID != parts[1] {
				clear(v.Material)
				return p, errors.New("合并身份无效。")
			}
			p.Identities = append(p.Identities, v)
		case "preference":
			prefs[parts[1]] = value
		case "extension":
			extensions[parts[1]] = value
		default:
			return p, errors.New("合并配置类型无效。")
		}
	}
	byID := map[string]workspace.Host{}
	for _, h := range p.Snapshot.Hosts {
		byID[h.ID] = h
	}
	ordered := []workspace.Host{}
	for _, id := range orders["hosts"] {
		if h, ok := byID[id]; ok {
			ordered = append(ordered, h)
			delete(byID, id)
		}
	}
	for _, h := range p.Snapshot.Hosts {
		if _, ok := byID[h.ID]; ok {
			ordered = append(ordered, h)
		}
	}
	p.Snapshot.Hosts = ordered
	pins := map[string]bool{}
	for _, h := range ordered {
		pins[h.ID] = h.Pinned
	}
	for _, id := range orders["pins"] {
		if pins[id] {
			p.Snapshot.PinOrder = append(p.Snapshot.PinOrder, id)
		}
	}
	ensure := func(values []string) []string {
		out := []string{workspace.Ungrouped}
		for _, v := range values {
			if v != workspace.Ungrouped {
				out = append(out, v)
			}
		}
		return out
	}
	groups := make(map[string]bool, len(p.Snapshot.Groups))
	for _, name := range p.Snapshot.Groups {
		groups[name] = true
	}
	orderedGroups := []string{}
	for _, name := range orders["groups"] {
		if groups[name] {
			orderedGroups = append(orderedGroups, name)
			delete(groups, name)
		}
	}
	for _, name := range p.Snapshot.Groups {
		if groups[name] {
			orderedGroups = append(orderedGroups, name)
		}
	}
	p.Snapshot.Groups = ensure(orderedGroups)
	cmdByID := make(map[string]workspace.Command, len(p.Snapshot.Commands))
	for _, c := range p.Snapshot.Commands {
		cmdByID[c.ID] = c
	}
	orderedCommands := make([]workspace.Command, 0, len(p.Snapshot.Commands))
	for _, id := range orders["commands"] {
		if c, ok := cmdByID[id]; ok {
			orderedCommands = append(orderedCommands, c)
			delete(cmdByID, id)
		}
	}
	for _, c := range p.Snapshot.Commands {
		if _, ok := cmdByID[c.ID]; ok {
			orderedCommands = append(orderedCommands, c)
		}
	}
	p.Snapshot.Commands = orderedCommands
	cmdGroups := make(map[string]bool, len(p.Snapshot.CommandGroups))
	for _, name := range p.Snapshot.CommandGroups {
		cmdGroups[name] = true
	}
	orderedCmdGroups := make([]string, 0, len(p.Snapshot.CommandGroups))
	for _, name := range orders["commandGroups"] {
		if cmdGroups[name] {
			orderedCmdGroups = append(orderedCmdGroups, name)
			delete(cmdGroups, name)
		}
	}
	for _, name := range p.Snapshot.CommandGroups {
		if cmdGroups[name] {
			orderedCmdGroups = append(orderedCmdGroups, name)
		}
	}
	p.Snapshot.CommandGroups = ensure(orderedCmdGroups)

	rawPreferences, _ := json.Marshal(prefs)
	validated, err := preferences.DecodePortable(rawPreferences)
	if err != nil {
		return p, err
	}
	p.Preferences, _ = json.Marshal(validated)
	if err := workspace.ValidatePortableExtensions(extensions); err != nil {
		return p, err
	}
	if len(extensions) > 0 {
		p.Extensions = extensions
	}
	return p, nil
}
func conflictEntry(id string, left, right json.RawMessage) SyncConflict {
	parts := strings.SplitN(id, "/", 2)
	kind, name := parts[0], parts[1]
	switch id {
	case "order/hosts":
		name = "主机顺序"
	case "order/pins":
		name = "置顶顺序"
	case "order/groups":
		name = "主机分组顺序"
	case "order/commands":
		name = "快捷命令顺序"
	case "order/commandGroups":
		name = "命令分组顺序"
	}
	var item struct {
		Name     string `json:"name"`
		Metadata struct{ Name string }
	}
	if json.Unmarshal(left, &item) != nil || item.Name == "" {
		_ = json.Unmarshal(right, &item)
	}
	if item.Name != "" {
		name = item.Name
	} else if item.Metadata.Name != "" {
		name = item.Metadata.Name
	}
	return SyncConflict{ID: id, Name: name, Kind: kind, LocalDeleted: len(left) == 0, RemoteDeleted: len(right) == 0}
}

func jumpCycleHostIDs(hosts []workspace.Host) []string {
	byID := make(map[string]workspace.Host, len(hosts))
	for _, h := range hosts {
		byID[h.ID] = h
	}
	state := make(map[string]int, len(hosts))
	flagged := map[string]bool{}
	var stack []string
	var visit func(string)
	visit = func(id string) {
		if state[id] == 2 {
			return
		}
		if state[id] == 1 {
			for i := len(stack) - 1; i >= 0; i-- {
				flagged[stack[i]] = true
				if stack[i] == id {
					break
				}
			}
			return
		}
		if len(stack) > 64 {
			for _, member := range stack {
				flagged[member] = true
			}
			flagged[id] = true
			return
		}
		state[id] = 1
		stack = append(stack, id)
		for _, next := range byID[id].JumpHostIDs {
			if _, ok := byID[next]; ok {
				visit(next)
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
	}
	for _, h := range hosts {
		visit(h.ID)
	}
	out := make([]string, 0, len(flagged))
	for id := range flagged {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func updateMergedHost(merged, edits syncEntries, hostID string, edit func(*portableHostEntry)) bool {
	key := "host/" + hostID
	raw, ok := merged[key]
	if !ok {
		return false
	}
	var entry portableHostEntry
	err := json.Unmarshal(raw, &entry)
	defer entry.clearMaterial()
	if err != nil {
		return false
	}
	edit(&entry)
	updated, err := json.Marshal(entry)
	if err != nil || bytes.Equal(updated, raw) {
		clear(updated)
		return false
	}
	edits.replaceOwned(key, updated)
	merged[key] = updated
	return true
}

func Merge(base, local, remote Payload, choices map[string]string) (Payload, []SyncConflict, error) {
	b, l, r := entries(base), entries(local), entries(remote)
	defer b.clearOwned()
	defer l.clearOwned()
	defer r.clearOwned()
	edits := syncEntries{}
	defer edits.clearOwned()
	merged := syncEntries{}
	ids := map[string]bool{}
	for k := range b {
		ids[k] = true
	}
	for k := range l {
		ids[k] = true
	}
	for k := range r {
		ids[k] = true
	}
	conflicts := []SyncConflict{}
	conflictSet := map[string]bool{}
	addConflict := func(id string) {
		if conflictSet[id] {
			return
		}
		conflictSet[id] = true
		conflicts = append(conflicts, conflictEntry(id, l[id], r[id]))
	}
	for id := range ids {
		before, left, right := b[id], l[id], r[id]
		var chosen json.RawMessage
		switch {
		case bytes.Equal(left, right):
			chosen = left
		case bytes.Equal(left, before):
			chosen = right
		case bytes.Equal(right, before):
			chosen = left
		default:
			order, canMergeOrder := mergeEntryOrder(id, before, left, right)
			if canMergeOrder {
				chosen = order
			} else if choices[id] == "local" {
				chosen = left
			} else if choices[id] == "remote" {
				chosen = right
			} else {
				addConflict(id)
				chosen = left
			}
		}
		if len(chosen) > 0 {
			merged[id] = chosen
		}
	}
	resolveMissing := func(targetID string) (restored bool, decidedDelete bool) {
		left, right := l[targetID], r[targetID]
		if len(left) == 0 && len(right) == 0 {
			return false, false
		}
		switch choices[targetID] {
		case "local":
			if len(left) > 0 {
				merged[targetID] = left
				return true, false
			}
			return false, true
		case "remote":
			if len(right) > 0 {
				merged[targetID] = right
				return true, false
			}
			return false, true
		default:
			addConflict(targetID)
			if len(left) > 0 {
				merged[targetID] = left
				return true, false
			}
			merged[targetID] = right
			return true, false
		}
	}
	var p Payload
	var e error
	for range 8 {
		ClearPayload(p)
		p, e = fromEntries(merged)
		if e != nil {
			return p, conflicts, e
		}
		groups := make(map[string]bool, len(p.Snapshot.Groups))
		for _, g := range p.Snapshot.Groups {
			groups[g] = true
		}
		cmdGroups := make(map[string]bool, len(p.Snapshot.CommandGroups))
		for _, g := range p.Snapshot.CommandGroups {
			cmdGroups[g] = true
		}
		keys := make(map[string]bool, len(p.Keys))
		for _, k := range p.Keys {
			keys[k.ID] = true
		}
		identities := make(map[string]bool, len(p.Identities))
		for _, i := range p.Identities {
			identities[i.Metadata.ID] = true
		}
		hosts := make(map[string]bool, len(p.Snapshot.Hosts))
		for _, h := range p.Snapshot.Hosts {
			hosts[h.ID] = true
		}
		changed := false
		for _, h := range p.Snapshot.Hosts {
			if h.Group != "" && !groups[h.Group] {
				restored, del := resolveMissing("group/" + h.Group)
				if restored {
					changed = true
				} else if del {
					if updateMergedHost(merged, edits, h.ID, func(entry *portableHostEntry) {
						entry.Group = workspace.Ungrouped
					}) {
						changed = true
					}
				}
			}
			if h.IdentityID != "" && !identities[h.IdentityID] {
				restored, del := resolveMissing("identity/" + h.IdentityID)
				if restored {
					changed = true
				} else if del {
					if updateMergedHost(merged, edits, h.ID, func(entry *portableHostEntry) {
						entry.IdentityID = ""
						entry.Credential = nil
						entry.HasCredential = false
					}) {
						changed = true
					}
				}
			}
			if h.KeyID != "" && !keys[h.KeyID] {
				restored, del := resolveMissing("key/" + h.KeyID)
				if restored {
					changed = true
				} else if del {
					if updateMergedHost(merged, edits, h.ID, func(entry *portableHostEntry) {
						entry.KeyID = ""
						entry.Credential = nil
						entry.HasCredential = false
					}) {
						changed = true
					}
				}
			}
			for _, jumpID := range h.JumpHostIDs {
				if !hosts[jumpID] {
					restored, del := resolveMissing("host/" + jumpID)
					if restored {
						changed = true
					} else if del {
						missingJump := jumpID
						if updateMergedHost(merged, edits, h.ID, func(entry *portableHostEntry) {
							filtered := make([]string, 0, len(entry.JumpHostIDs))
							for _, id := range entry.JumpHostIDs {
								if id != missingJump {
									filtered = append(filtered, id)
								}
							}
							entry.JumpHostIDs = filtered
						}) {
							changed = true
						}
					}
				}
			}
		}
		for _, c := range p.Snapshot.Commands {
			if c.Group != "" && !cmdGroups[c.Group] {
				restored, del := resolveMissing("commandGroup/" + c.Group)
				if restored {
					changed = true
				} else if del {
					c.Group = workspace.Ungrouped
					if raw, err := json.Marshal(c); err == nil {
						id := "command/" + c.ID
						edits.replaceOwned(id, raw)
						merged[id] = raw
						changed = true
					}
				}
			}
		}
		for _, i := range p.Identities {
			if i.Metadata.KeyID != "" && !keys[i.Metadata.KeyID] {
				restored, del := resolveMissing("key/" + i.Metadata.KeyID)
				if restored {
					changed = true
				} else if del {
					i.Metadata.KeyID = ""
					if raw, err := json.Marshal(i); err == nil {
						id := "identity/" + i.Metadata.ID
						edits.replaceOwned(id, raw)
						merged[id] = raw
						changed = true
					}
				}
			}
		}
		if !changed {
			for _, hostID := range jumpCycleHostIDs(p.Snapshot.Hosts) {
				entryID := "host/" + hostID
				if bytes.Equal(l[entryID], r[entryID]) {
					continue
				}
				switch choices[entryID] {
				case "local":
					if !bytes.Equal(merged[entryID], l[entryID]) {
						if len(l[entryID]) > 0 {
							merged[entryID] = l[entryID]
						} else {
							delete(merged, entryID)
						}
						changed = true
					}
				case "remote":
					if !bytes.Equal(merged[entryID], r[entryID]) {
						if len(r[entryID]) > 0 {
							merged[entryID] = r[entryID]
						} else {
							delete(merged, entryID)
						}
						changed = true
					}
				default:
					addConflict(entryID)
				}
			}
		}
		if !changed {
			break
		}
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].ID < conflicts[j].ID })
	return p, conflicts, nil
}
func Canonicalize(p Payload) (Payload, error) {
	if err := workspace.ValidatePortableExtensions(p.Extensions); err != nil {
		return Payload{}, err
	}
	if len(p.Preferences) == 0 {
		p.Preferences = json.RawMessage(`{}`)
	}
	inputPreferences, err := preferences.DecodePortable(p.Preferences)
	if err != nil {
		return Payload{}, err
	}
	p.Preferences, _ = json.Marshal(inputPreferences)
	credentialIDs := map[string]bool{}
	hostIDs := map[string]bool{}
	for _, h := range p.Snapshot.Hosts {
		hostIDs[h.ID] = true
	}
	for _, c := range p.HostCredentials {
		if credentialIDs[c.HostID] || !hostIDs[c.HostID] {
			return Payload{}, errors.New("连接认证记录无效。")
		}
		credentialIDs[c.HostID] = true
	}
	proxyIDs := map[string]bool{}
	for _, c := range p.ProxyCredentials {
		if proxyIDs[c.HostID] || !hostIDs[c.HostID] {
			return Payload{}, errors.New("连接认证记录无效。")
		}
		proxyIDs[c.HostID] = true
	}
	values := entries(p)
	defer values.clearOwned()
	var prefs map[string]json.RawMessage
	if json.Unmarshal(p.Preferences, &prefs) != nil {
		return Payload{}, errors.New("同步偏好格式无效。")
	}
	want := 5 + len(p.Snapshot.Hosts) + len(p.Snapshot.Groups) + len(p.Snapshot.CommandGroups) + len(p.Snapshot.Commands) + len(p.Keys) + len(p.Identities) + len(prefs) + len(p.Extensions)
	if len(values) != want {
		return Payload{}, errors.New("同步配置包含重复标识。")
	}
	return fromEntries(values)
}
