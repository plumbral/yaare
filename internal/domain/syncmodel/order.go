package syncmodel

import (
	"encoding/json"
	"slices"
)

func mergeEntryOrder(id string, before, left, right json.RawMessage) (json.RawMessage, bool) {
	if id != "order/hosts" && id != "order/pins" && id != "order/groups" && id != "order/commands" && id != "order/commandGroups" {
		return nil, false
	}
	var base, local, remote []string
	if json.Unmarshal(before, &base) != nil || json.Unmarshal(left, &local) != nil || json.Unmarshal(right, &remote) != nil {
		return nil, false
	}
	l, r := map[string]bool{}, map[string]bool{}
	for _, id := range local {
		l[id] = true
	}
	for _, id := range remote {
		r[id] = true
	}
	shared := map[string]bool{}
	common := []string{}
	for _, id := range base {
		if l[id] && r[id] {
			shared[id] = true
			common = append(common, id)
		}
	}
	project := func(ids []string) []string {
		out := []string{}
		for _, id := range ids {
			if shared[id] {
				out = append(out, id)
			}
		}
		return out
	}
	lo, ro := project(local), project(remote)
	lMoved, rMoved := !slices.Equal(lo, common), !slices.Equal(ro, common)
	if lMoved && rMoved && !slices.Equal(lo, ro) {
		return nil, false
	}
	preferred, extra := local, remote
	if rMoved && !lMoved {
		preferred, extra = remote, local
	}
	seen := map[string]bool{}
	out := []string{}
	for _, ids := range [][]string{preferred, extra} {
		for _, id := range ids {
			if !seen[id] {
				out = append(out, id)
				seen[id] = true
			}
		}
	}
	raw, err := json.Marshal(out)
	return raw, err == nil
}
