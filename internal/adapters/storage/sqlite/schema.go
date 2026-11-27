package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"yaare/internal/domain/workspace"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type keyMeta struct {
	ID        string
	Name      string
	Format    string
	Path      string
	CipherLen int
}

func (k keyMeta) valid() bool {
	probe := workspace.SavedKey{ID: k.ID, Name: k.Name, Format: k.Format, Path: k.Path}
	if k.CipherLen > 0 {
		if k.CipherLen > workspace.MaxKeyCipherBytes {
			return false
		}
		probe.Cipher = []byte{1}
	}
	return workspace.ValidKey(probe)
}

func readKeyMetadata(ctx context.Context, q queryer) ([]keyMeta, int, error) {
	rows, err := q.QueryContext(ctx, "SELECT id,name,format,path,COALESCE(length(cipher),0) FROM keys ORDER BY id")
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	keys := []keyMeta{}
	totalCipher := 0
	for rows.Next() {
		var key keyMeta
		if err = rows.Scan(&key.ID, &key.Name, &key.Format, &key.Path, &key.CipherLen); err != nil {
			return nil, 0, err
		}
		totalCipher += key.CipherLen
		if len(keys) >= workspace.MaxSavedKeys || !key.valid() || totalCipher > workspace.MaxTotalKeyCipherBytes {
			return nil, 0, workspace.Fail("CONFIG_CORRUPT", "密钥引用记录无效。")
		}
		keys = append(keys, key)
	}
	return keys, totalCipher, rows.Err()
}

func readKeys(ctx context.Context, q queryer) ([]workspace.SavedKey, error) {
	rows, err := q.QueryContext(ctx, "SELECT id,name,format,path,cipher FROM keys ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []workspace.SavedKey{}
	totalCipher := 0
	for rows.Next() {
		var key workspace.SavedKey
		if err = rows.Scan(&key.ID, &key.Name, &key.Format, &key.Path, &key.Cipher); err != nil {
			return nil, err
		}
		totalCipher += len(key.Cipher)
		if len(keys) >= workspace.MaxSavedKeys || !workspace.ValidKey(key) || totalCipher > workspace.MaxTotalKeyCipherBytes {
			return nil, workspace.Fail("CONFIG_CORRUPT", "密钥引用记录无效。")
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

const hostSelectColumns = "id,name,address,port,username,group_name,system,icon,authentication,COALESCE(key_id,''),position,COALESCE(identity_id,''),notes,pinned,last_used,jump_hosts,forward_rules,timeout_seconds,proxy_config,use_agent"

func scanHost(scan func(dest ...any) error) (workspace.Host, error) {
	var h workspace.Host
	var position int
	var jumps, forwards, proxy string
	if err := scan(&h.ID, &h.Name, &h.Address, &h.Port, &h.Username, &h.Group, &h.System, &h.Icon, &h.Authentication, &h.KeyID, &position, &h.IdentityID, &h.Notes, &h.Pinned, &h.LastUsed, &jumps, &forwards, &h.TimeoutSeconds, &proxy, &h.UseAgent); err != nil {
		return h, err
	}
	if len(jumps) > 2048 || len(forwards) > 32768 || json.Unmarshal([]byte(jumps), &h.JumpHostIDs) != nil || json.Unmarshal([]byte(forwards), &h.Forwards) != nil {
		return h, workspace.Fail("CONFIG_CORRUPT", "网络配置记录无效。")
	}
	if len(proxy) > 4096 || json.Unmarshal([]byte(proxy), &h.Proxy) != nil {
		return h, workspace.Fail("CONFIG_CORRUPT", "代理配置记录无效。")
	}
	return h, nil
}

func readSnapshot(ctx context.Context, q queryer) (workspace.Snapshot, []keyMeta, error) {
	return readSnapshotRaw(ctx, q, true)
}

func readSnapshotFast(ctx context.Context, q queryer) (workspace.Snapshot, []keyMeta, int, error) {
	return readSnapshotInternal(ctx, q, false)
}

func readSnapshotRaw(ctx context.Context, q queryer, validate bool) (workspace.Snapshot, []keyMeta, error) {
	s, keys, _, err := readSnapshotInternal(ctx, q, validate)
	return s, keys, err
}

func readSnapshotInternal(ctx context.Context, q queryer, validate bool) (workspace.Snapshot, []keyMeta, int, error) {
	s := workspace.Snapshot{Hosts: []workspace.Host{}, Groups: []string{}, Commands: []workspace.Command{}, CommandGroups: []string{}}
	if err := q.QueryRowContext(ctx, "SELECT revision FROM meta WHERE id=1").Scan(&s.Revision); err != nil {
		return s, nil, 0, err
	}
	rows, err := q.QueryContext(ctx, "SELECT name FROM groups ORDER BY position,name")
	if err != nil {
		return s, nil, 0, err
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			break
		}
		s.Groups = append(s.Groups, name)
		if len(s.Groups) > 256 {
			err = workspace.Fail("CONFIG_CORRUPT", "分组数量超出支持范围。")
			break
		}
	}
	if rowErr := rows.Err(); err == nil {
		err = rowErr
	}
	rows.Close()
	if err != nil {
		return s, nil, 0, err
	}
	s.PinOrder, err = readPinOrder(ctx, q)
	if err != nil {
		return s, nil, 0, err
	}
	rows, err = q.QueryContext(ctx, "SELECT "+hostSelectColumns+" FROM hosts ORDER BY position,id")
	if err != nil {
		return s, nil, 0, err
	}
	for rows.Next() {
		h, scanErr := scanHost(rows.Scan)
		if scanErr != nil {
			err = scanErr
			break
		}
		s.Hosts = append(s.Hosts, h)
		if len(s.Hosts) > 10000 {
			err = workspace.Fail("CONFIG_CORRUPT", "主机数量超出支持范围。")
			break
		}
	}
	if rowErr := rows.Err(); err == nil {
		err = rowErr
	}
	rows.Close()
	if err != nil {
		return s, nil, 0, err
	}
	rows, err = q.QueryContext(ctx, "SELECT name FROM command_groups ORDER BY position,name")
	if err != nil {
		return s, nil, 0, err
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			break
		}
		s.CommandGroups = append(s.CommandGroups, name)
		if len(s.CommandGroups) > 256 {
			err = workspace.Fail("CONFIG_CORRUPT", "命令分组数量超出支持范围。")
			break
		}
	}
	if rowErr := rows.Err(); err == nil {
		err = rowErr
	}
	rows.Close()
	if err != nil {
		return s, nil, 0, err
	}
	rows, err = q.QueryContext(ctx, "SELECT id,name,command,group_name,icon FROM commands ORDER BY position,id")
	if err != nil {
		return s, nil, 0, err
	}
	for rows.Next() {
		var c workspace.Command
		if err = rows.Scan(&c.ID, &c.Name, &c.Command, &c.Group, &c.Icon); err != nil {
			break
		}
		s.Commands = append(s.Commands, c)
		if len(s.Commands) > 1000 {
			err = workspace.Fail("CONFIG_CORRUPT", "快捷命令数量超出支持范围。")
			break
		}
	}
	if rowErr := rows.Err(); err == nil {
		err = rowErr
	}
	rows.Close()
	if err != nil {
		return s, nil, 0, err
	}
	keys, totalCipher, err := readKeyMetadata(ctx, q)
	if err != nil {
		return s, nil, 0, err
	}
	credentials, e := readHostCredentialMetadata(ctx, q)
	if e != nil {
		return s, nil, 0, e
	}
	hosts := make(map[string]int, len(s.Hosts))
	for i := range s.Hosts {
		hosts[s.Hosts[i].ID] = i
		s.Hosts[i].HasCredential = false
	}
	for _, c := range credentials {
		i, ok := hosts[c.HostID]
		if !ok || !workspace.CredentialMatches(s.Hosts[i], c) {
			return s, nil, 0, workspace.Fail("CREDENTIAL_INVALID", "连接认证记录无效。")
		}
		s.Hosts[i].HasCredential = true
	}
	if validate {
		ids := make(map[string]bool, len(keys))
		for _, key := range keys {
			ids[key.ID] = true
		}
		if err = workspace.Validate(s, ids); err != nil {
			return s, nil, 0, err
		}
	}
	return s, keys, totalCipher, nil
}

func writeDiff(ctx context.Context, tx *sql.Tx, old, next workspace.Snapshot) ([]workspace.Host, error) {
	if !reflect.DeepEqual(old.PinOrder, next.PinOrder) {
		ids := next.PinOrder
		if ids == nil {
			ids = []string{}
		}
		raw, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE host_order SET pins=? WHERE id=1", string(raw)); err != nil {
			return nil, err
		}
	}
	oldGroupPos := make(map[string]int, len(old.Groups))
	for i, g := range old.Groups {
		oldGroupPos[g] = i
	}
	newGroups := make(map[string]bool, len(next.Groups))
	for i, g := range next.Groups {
		newGroups[g] = true
		if pos, ok := oldGroupPos[g]; ok && pos == i {
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO groups(name,position) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET position=excluded.position WHERE position<>excluded.position", g, i); err != nil {
			return nil, err
		}
	}
	newHosts := make(map[string]bool, len(next.Hosts))
	oldHosts := make(map[string]workspace.Host, len(old.Hosts))
	oldPositions := make(map[string]int, len(old.Hosts))
	for i, h := range old.Hosts {
		oldHosts[h.ID] = h
		oldPositions[h.ID] = i
	}
	for _, h := range next.Hosts {
		newHosts[h.ID] = true
	}
	for _, h := range old.Hosts {
		if !newHosts[h.ID] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM hosts WHERE id=?", h.ID); err != nil {
				return nil, err
			}
		}
	}
	var modifiedHosts []workspace.Host
	for i, h := range next.Hosts {
		before, existed := oldHosts[h.ID]
		if existed && reflect.DeepEqual(before, h) {
			if oldPositions[h.ID] != i {
				if _, err := tx.ExecContext(ctx, "UPDATE hosts SET position=? WHERE id=?", i, h.ID); err != nil {
					return nil, err
				}
			}
			continue
		}
		if existed {
			modifiedHosts = append(modifiedHosts, h)
		}
		var keyID any
		if h.Authentication == "key" && h.KeyID != "" {
			keyID = h.KeyID
		}
		var identityID any
		if h.IdentityID != "" {
			identityID = h.IdentityID
		}
		jumps, _ := json.Marshal(h.JumpHostIDs)
		forwards, _ := json.Marshal(h.Forwards)
		proxy, _ := json.Marshal(h.Proxy)
		if _, err := tx.ExecContext(ctx, `INSERT INTO hosts(id,name,address,port,username,group_name,system,icon,authentication,key_id,position,identity_id,notes,pinned,last_used,jump_hosts,forward_rules,timeout_seconds,proxy_config,use_agent) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,address=excluded.address,port=excluded.port,username=excluded.username,group_name=excluded.group_name,system=excluded.system,icon=excluded.icon,authentication=excluded.authentication,key_id=excluded.key_id,position=excluded.position,identity_id=excluded.identity_id,notes=excluded.notes,pinned=excluded.pinned,last_used=excluded.last_used,jump_hosts=excluded.jump_hosts,forward_rules=excluded.forward_rules,timeout_seconds=excluded.timeout_seconds,proxy_config=excluded.proxy_config,use_agent=excluded.use_agent`, h.ID, h.Name, h.Address, h.Port, h.Username, h.Group, h.System, h.Icon, h.Authentication, keyID, i, identityID, h.Notes, h.Pinned, h.LastUsed, string(jumps), string(forwards), h.TimeoutSeconds, string(proxy), h.UseAgent); err != nil {
			return nil, err
		}
	}
	for _, g := range old.Groups {
		if !newGroups[g] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM groups WHERE name=?", g); err != nil {
				return nil, err
			}
		}
	}
	oldCmdGroupPos := make(map[string]int, len(old.CommandGroups))
	for i, g := range old.CommandGroups {
		oldCmdGroupPos[g] = i
	}
	newCommandGroups := make(map[string]bool, len(next.CommandGroups))
	for i, g := range next.CommandGroups {
		newCommandGroups[g] = true
		if pos, ok := oldCmdGroupPos[g]; ok && pos == i {
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO command_groups(name,position) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET position=excluded.position WHERE position<>excluded.position", g, i); err != nil {
			return nil, err
		}
	}
	newCommands := make(map[string]bool, len(next.Commands))
	oldCommands := make(map[string]workspace.Command, len(old.Commands))
	positions := make(map[string]int, len(old.Commands))
	for i, c := range old.Commands {
		oldCommands[c.ID] = c
		positions[c.ID] = i
	}
	for _, c := range next.Commands {
		newCommands[c.ID] = true
	}
	for _, c := range old.Commands {
		if !newCommands[c.ID] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM commands WHERE id=?", c.ID); err != nil {
				return nil, err
			}
		}
	}
	for i, c := range next.Commands {
		if before, ok := oldCommands[c.ID]; ok && before == c {
			if positions[c.ID] != i {
				if _, err := tx.ExecContext(ctx, "UPDATE commands SET position=? WHERE id=?", i, c.ID); err != nil {
					return nil, err
				}
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO commands(id,name,command,position,group_name,icon) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,command=excluded.command,position=excluded.position,group_name=excluded.group_name,icon=excluded.icon", c.ID, c.Name, c.Command, i, c.Group, workspace.CommandIcon(c.Icon)); err != nil {
			return nil, err
		}
	}
	for _, g := range old.CommandGroups {
		if !newCommandGroups[g] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM command_groups WHERE name=?", g); err != nil {
				return nil, err
			}
		}
	}
	return modifiedHosts, nil
}
