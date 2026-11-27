package workspace

func OrderedPins(s Snapshot) []string {
	available := map[string]bool{}
	for _, h := range s.Hosts {
		if h.Pinned {
			available[h.ID] = true
		}
	}
	out := []string{}
	for _, id := range s.PinOrder {
		if available[id] {
			out = append(out, id)
			delete(available, id)
		}
	}
	for _, h := range s.Hosts {
		if available[h.ID] {
			out = append(out, h.ID)
			delete(available, h.ID)
		}
	}
	return out
}

func reorderHosts(s *Snapshot, ids []string, pinned bool) error {
	if len(ids) < 1 || len(ids) > len(s.Hosts) {
		return Fail("INVALID_HOST_ORDER", "主机排序范围无效，请刷新后重试。")
	}
	available := map[string]Host{}
	for _, h := range s.Hosts {
		if h.Pinned == pinned {
			available[h.ID] = h
		}
	}
	selected := map[string]bool{}
	for _, id := range ids {
		if _, ok := available[id]; !ok || selected[id] {
			return Fail("INVALID_HOST_ORDER", "主机排序范围无效，请刷新后重试。")
		}
		selected[id] = true
	}
	if pinned {
		order := OrderedPins(*s)
		n := 0
		for i, id := range order {
			if selected[id] {
				order[i] = ids[n]
				n++
			}
		}
		s.PinOrder = order
	} else {
		n := 0
		for i, h := range s.Hosts {
			if selected[h.ID] {
				s.Hosts[i] = available[ids[n]]
				n++
			}
		}
	}
	return nil
}
