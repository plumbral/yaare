package ipc

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"time"
	filefs "yaare/internal/adapters/files"
)

type FileSearchRequest struct {
	Recursive bool         `json:"recursive"`
	Location  FileLocation `json:"location"`
	Query     string       `json:"query"`
	Hidden    bool         `json:"hidden"`
}

func (s *FileService) Search(ctx context.Context, request FileSearchRequest, events *mygo.Channel[filefs.SearchProgress]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.searchStream(ctx, request, events)
}
func (s *FileService) searchStream(ctx context.Context, request FileSearchRequest, events eventSink[filefs.SearchProgress]) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	if events == nil {
		return errors.New("搜索结果通道不可用。")
	}
	select {
	case s.searches <- struct{}{}:
		defer func() { <-s.searches }()
	default:
		return errors.New("正在进行的搜索较多，请先停止其他搜索。")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	filesystem, closeFS, p, err := s.filesystem(ctx, callerPage(ctx), request.Location)
	if err != nil {
		return err
	}
	defer closeFS()
	err = filefs.SearchNamesOptions(ctx, filesystem, p, request.Query, request.Hidden, request.Recursive, func(progress filefs.SearchProgress) error { return events.Send(progress) })
	if err != nil {
		return fileProblem(err)
	}
	return nil
}
