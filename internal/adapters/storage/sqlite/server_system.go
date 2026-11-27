package sqlite

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"yaare/internal/domain/workspace"
)

type systemObservation struct {
	Route   string
	System  workspace.ServerSystem
	Started int64
}

func (s *Store) loadSystemsLocked() {
	if s.systems != nil {
		return
	}
	s.systems = map[string]systemObservation{}
	f, err := os.Open(filepath.Join(s.dir, "server-systems.json"))
	if err != nil {
		return
	}
	defer f.Close()
	const maximum = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(f, maximum+1))
	var records map[string]systemObservation
	if err == nil && len(raw) <= maximum && json.Unmarshal(raw, &records) == nil && records != nil && len(records) <= 10000 {
		s.systems = records
	}
}

func (s *Store) ServerSystems(ctx context.Context) (map[string]workspace.ServerSystem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, s.unavailable()
	}
	s.loadSystemsLocked()
	result := map[string]workspace.ServerSystem{}
	if len(s.systems) == 0 {
		return result, nil
	}
	hosts, err := s.systemHostsLocked(ctx)
	if err != nil {
		return nil, err
	}
	for id, h := range hosts {
		if record, ok := s.systems[id]; ok && record.System.Valid() &&
			record.Route == systemRoute(h, hosts) && record.Route != "" {
			result[id] = record.System
		}
	}
	return result, nil
}

func (s *Store) SaveServerSystem(ctx context.Context, id, route string, started int64, info workspace.ServerSystem) error {
	if id == "" || route == "" || !info.Valid() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return s.unavailable()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.loadSystemsLocked()
	hostMap, err := s.systemHostsLocked(ctx)
	if err != nil {
		return err
	}
	host, ok := hostMap[id]
	if !ok || systemRoute(host, hostMap) != route {
		return nil
	}
	prior, exists := s.systems[id]
	if exists && prior.Route == route && prior.Started > started {
		return nil
	}
	if exists && prior.Route == route {
		if prior.System == info || (prior.System.Family == info.Family && prior.System.ID != "" && info.ID == "") {
			prior.Started = started
			s.systems[id] = prior
			return nil
		}
	}
	updated := make(map[string]systemObservation, len(s.systems)+1)
	for previousID, current := range s.systems {
		if _, ok := hostMap[previousID]; ok {
			updated[previousID] = current
		}
	}
	updated[id] = systemObservation{route, info, started}
	if err = s.writeSystemsLocked(ctx, updated); err != nil {
		return err
	}
	s.systems = updated
	return nil
}

func systemRoute(host workspace.Host, hosts map[string]workspace.Host) string {
	return workspace.ServerSystemRoute(host, func(id string) (workspace.Host, bool) {
		h, ok := hosts[id]
		return h, ok
	})
}

func (s *Store) systemHostsLocked(ctx context.Context) (map[string]workspace.Host, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,address,port,username,jump_hosts,proxy_config FROM hosts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hosts := map[string]workspace.Host{}
	for rows.Next() {
		var h workspace.Host
		var jumps, proxy string
		if err = rows.Scan(&h.ID, &h.Address, &h.Port, &h.Username, &jumps, &proxy); err != nil {
			return nil, err
		}
		if len(jumps) > 2048 || json.Unmarshal([]byte(jumps), &h.JumpHostIDs) != nil ||
			len(proxy) > 4096 || json.Unmarshal([]byte(proxy), &h.Proxy) != nil {
			return nil, workspace.Fail("CONFIG_CORRUPT", "网络配置记录无效。")
		}
		hosts[h.ID] = h
		if len(hosts) > 10000 {
			return nil, workspace.Fail("CONFIG_CORRUPT", "主机数量超出支持范围。")
		}
	}
	return hosts, rows.Err()
}

func (s *Store) writeSystemsLocked(ctx context.Context, records map[string]systemObservation) error {
	f, err := os.CreateTemp(s.dir, ".server-systems-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(records)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.dir, "server-systems.json"))
}
