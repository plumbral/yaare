package syncmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	"yaare/internal/domain/workspace"
)

const customIconRef = "custom-ref:"

func iconContentID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func PackIcons(p Payload) Payload {
	for i, host := range p.Snapshot.Hosts {
		if !strings.HasPrefix(host.Icon, workspace.CustomIconPrefix) {
			continue
		}
		if p.IconAssets == nil {
			p.IconAssets = map[string]string{}
			p.Snapshot.Hosts = slices.Clone(p.Snapshot.Hosts)
		}
		id := iconContentID(host.Icon)
		p.IconAssets[id] = host.Icon
		p.Snapshot.Hosts[i].Icon = customIconRef + id
	}
	return p
}

func UnpackIcons(p *Payload) error {
	used := map[string]bool{}
	total := 0
	for i, host := range p.Snapshot.Hosts {
		if id, ok := strings.CutPrefix(host.Icon, customIconRef); ok {
			icon := p.IconAssets[id]
			if len(id) != 64 || !strings.HasPrefix(icon, workspace.CustomIconPrefix) || (!used[id] && iconContentID(icon) != id) {
				return errors.New("自定义图标无效。")
			}
			p.Snapshot.Hosts[i].Icon = icon
			used[id] = true
			host.Icon = icon
		}
		if strings.HasPrefix(host.Icon, workspace.CustomIconPrefix) {
			total += len(host.Icon)
			if total > workspace.MaxWorkspaceIconBytes {
				return errors.New("自定义图标总量超过 1 MiB。")
			}
		}
	}
	if len(used) != len(p.IconAssets) {
		return errors.New("自定义图标无效。")
	}
	p.IconAssets = nil
	return nil
}
