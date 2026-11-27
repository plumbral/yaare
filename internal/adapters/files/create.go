package files

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
)

func CreateEmpty(ctx context.Context, f FS, parent, name string) error {
	if !Leaf(name) || strings.IndexFunc(name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return problem("请输入有效的文件名称。")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	target := path.Join(parent, name)
	if _, err := f.Lstat(target); err == nil {
		return problem("同名文件或目录已存在，请使用其他名称。")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	handle, err := f.Create(target)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return problem("同名文件或目录已存在，请使用其他名称。")
		}
		return err
	}
	if err = handle.Close(); err != nil {
		return problem("创建结果尚未确认，请刷新目录后检查；已有文件不会自动覆盖。")
	}
	return ctx.Err()
}
