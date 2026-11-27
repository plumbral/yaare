package sqlite

import (
	"context"

	"encoding/json"

	"yaare/internal/domain/workspace"
)

func readPinOrder(ctx context.Context, q queryer) ([]string, error) {
	var raw string
	if e := q.QueryRowContext(ctx, "SELECT pins FROM host_order WHERE id=1").Scan(&raw); e != nil {
		return nil, e
	}
	var ids []string
	if len(raw) > 1<<20 || json.Unmarshal([]byte(raw), &ids) != nil {
		return nil, workspace.Fail("CONFIG_CORRUPT", "置顶顺序记录无效，原数据已保留。")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, nil
}
