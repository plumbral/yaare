package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"yaare/internal/domain/workspace"
)

func readSessionLayout(ctx context.Context, q queryer) (workspace.SessionLayout, error) {
	var data string
	var version int64
	err := q.QueryRowContext(ctx, "SELECT revision,payload FROM session_layout WHERE id=1").Scan(&version, &data)
	if err == sql.ErrNoRows {
		return workspace.EmptySessionLayout(), nil
	}
	if err != nil {
		return workspace.SessionLayout{}, err
	}
	var s workspace.SessionLayout
	if len(data) > 262144 || json.Unmarshal([]byte(data), &s) != nil || s.Version != version || !workspace.ValidSessionLayout(s) {
		return workspace.SessionLayout{}, workspace.Fail("LAYOUT_CORRUPT", "工作区记录无效，未覆盖原记录。")
	}
	return s, nil
}
func (s *Store) SessionLayout(ctx context.Context) (workspace.SessionLayout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return workspace.SessionLayout{}, s.unavailable()
	}
	return readSessionLayout(ctx, s.db)
}
func (s *Store) SaveSessionLayout(ctx context.Context, next workspace.SessionLayout) (workspace.SessionLayout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return next, s.unavailable()
	}
	if !workspace.ValidSessionLayout(next) {
		return next, workspace.Fail("LAYOUT_INVALID", "工作区布局无效，未保存。")
	}
	if !next.Enabled {
		empty := workspace.EmptySessionLayout()
		empty.Enabled = false
		empty.Version = next.Version
		next = empty
	}
	current, err := readSessionLayout(ctx, s.db)
	if err != nil {
		return next, err
	}
	if current.Version != next.Version {
		return next, workspace.Fail("LAYOUT_CONFLICT", "工作区已变化，请重新读取后重试。")
	}
	next.Version++
	data, err := json.Marshal(next)
	if err != nil || len(data) > 262144 {
		return next, workspace.Fail("LAYOUT_LIMIT", "工作区记录过大，未保存。")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return next, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO session_layout VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,payload=excluded.payload", next.Version, string(data)); err != nil {
		return next, storageWriteError(err, nil)
	}
	if err = tx.Commit(); err != nil {
		return next, storageWriteError(err, workspace.Fail("LAYOUT_SAVE", "工作区保存未确认，请重试。"))
	}
	s.scheduleRoutineBackup(s.now())
	return next, nil
}
