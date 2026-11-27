package server

import (
	"context"
	"errors"
	"io"
	filefs "yaare/internal/adapters/files"
)

type Opener func(context.Context, string) (filefs.RemoteCommand, error)

func Run(ctx context.Context, open Opener, command string, limit int64) ([]byte, error) {
	channel, e := open(ctx, command)
	if e != nil {
		return nil, e
	}
	defer channel.Close()
	if channel.Input != nil {
		_ = channel.Input.Close()
	}
	raw, e := io.ReadAll(io.LimitReader(channel.Output, limit+1))
	if e != nil {
		return nil, errors.New("远程工具输出中断，请检查连接后重试。")
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("远程工具输出较多，请缩小查询范围。")
	}
	if e = channel.Wait(); e != nil {
		return nil, errors.New("服务器未能完成此操作，请检查工具是否可用及账户权限。")
	}
	return raw, nil
}
