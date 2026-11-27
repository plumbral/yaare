package ipc

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"path"
	"runtime"
	"time"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/domain/workspace"
)

type ComparisonRequest struct {
	Left      FileLocation `json:"left"`
	Right     FileLocation `json:"right"`
	Recursive bool         `json:"recursive"`
}
type ComparisonView struct {
	ID         string            `json:"id"`
	Comparison filefs.Comparison `json:"comparison"`
}
type ComparisonEvent struct {
	Scanned int             `json:"scanned"`
	Bytes   int64           `json:"bytes"`
	Result  *ComparisonView `json:"result,omitempty"`
}
type ownedComparison struct {
	page    *callerScope
	request ComparisonRequest
	view    ComparisonView
	at      time.Time
}
type ownedSync struct {
	page         *callerScope
	source, dest FileLocation
	plan         *filefs.ReviewedSync
	at           time.Time
}
type SyncIntent struct {
	ID        string               `json:"id"`
	Direction string               `json:"direction"`
	Items     []filefs.SyncPreview `json:"items"`
	Retained  int                  `json:"retained"`
}
type ComparedText struct {
	Left  filefs.TextFile `json:"left"`
	Right filefs.TextFile `json:"right"`
}

func (s *FileService) Compare(ctx context.Context, request ComparisonRequest, events *mygo.Channel[ComparisonEvent]) error {
	if events == nil {
		return errors.New("数据通道不可用，请重新打开。")
	}
	return s.compareStream(ctx, request, events)
}
func (s *FileService) compareStream(ctx context.Context, request ComparisonRequest, events eventSink[ComparisonEvent]) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	if request.Left.Side != "local" || request.Right.Side != "remote" || events == nil {
		return errors.New("请选择已授权本地路径与当前远程路径。")
	}
	select {
	case s.searches <- struct{}{}:
		defer func() { <-s.searches }()
	default:
		return errors.New("正在扫描的任务较多，请先停止其他扫描。")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	page := callerPage(ctx)
	left, cl, lp, err := s.filesystem(ctx, page, request.Left)
	if err != nil {
		return err
	}
	defer cl()
	right, cr, rp, err := s.filesystem(ctx, page, request.Right)
	if err != nil {
		return err
	}
	defer cr()
	request.Left.Path, request.Right.Path = lp, rp
	comparison, err := filefs.CompareTrees(ctx, left, right, lp, rp, request.Recursive, runtime.GOOS == "windows", func(scanned int, bytes int64) { _ = events.Send(ComparisonEvent{Scanned: scanned, Bytes: bytes}) })
	if err != nil {
		return fileProblem(err)
	}
	view := ComparisonView{workspace.NewID(), comparison}
	s.mu.Lock()
	for id, p := range s.comparisons {
		if time.Since(p.at) > 5*time.Minute {
			delete(s.comparisons, id)
		}
	}
	owned := 0
	for _, p := range s.comparisons {
		if p.page == page {
			owned++
		}
	}
	if owned >= 4 {
		s.mu.Unlock()
		return errors.New("已有四份比较快照，请关闭旧比较后重新开始。")
	}
	s.comparisons[view.ID] = ownedComparison{page, request, view, time.Now()}
	s.mu.Unlock()
	err = events.Send(ComparisonEvent{Scanned: comparison.Scanned, Bytes: comparison.Bytes, Result: &view})
	if err != nil {
		s.mu.Lock()
		delete(s.comparisons, view.ID)
		s.mu.Unlock()
	}
	return err
}
func (s *FileService) comparison(ctx context.Context, id string) (ownedComparison, error) {
	if err := workspaceCaller(ctx); err != nil {
		return ownedComparison{}, err
	}
	s.mu.Lock()
	p, ok := s.comparisons[id]
	s.mu.Unlock()
	if !ok || p.page != callerPage(ctx) || time.Since(p.at) > 5*time.Minute {
		return p, errors.New("比较快照已过期，请重新比较。")
	}
	return p, nil
}
func (s *FileService) ForgetComparison(ctx context.Context, id string) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.comparisons[id]; ok && p.page == callerPage(ctx) {
		delete(s.comparisons, id)
	}
	return nil
}
func (s *FileService) PreviewSync(ctx context.Context, id, direction string, selected []string) (SyncIntent, error) {
	p, err := s.comparison(ctx, id)
	if err != nil {
		return SyncIntent{}, err
	}
	plan, err := filefs.BuildReviewedSync(p.view.Comparison, p.request.Left.Path, p.request.Right.Path, direction, selected)
	if err != nil {
		return SyncIntent{}, fileProblem(err)
	}
	source, dest := p.request.Left, p.request.Right
	if direction == "download" {
		source, dest = dest, source
	}
	dest.Path = plan.DestinationRoot
	token := workspace.NewID()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.syncIntents {
		if time.Since(p.at) > 2*time.Minute {
			delete(s.syncIntents, id)
		}
	}
	if len(s.syncIntents) >= 8 {
		return SyncIntent{}, errors.New("待确认复制较多，请稍后重试。")
	}
	s.syncIntents[token] = ownedSync{callerPage(ctx), source, dest, plan, time.Now()}
	return SyncIntent{token, direction, plan.Preview, plan.Retained}, nil
}
func (s *FileService) ExecuteSync(ctx context.Context, token string, confirmed bool) (string, error) {
	if err := workspaceCaller(ctx); err != nil {
		return "", err
	}
	if !confirmed {
		return "", errors.New("请先确认复制方向和文件清单。")
	}
	s.mu.Lock()
	intent, ok := s.syncIntents[token]
	if ok && intent.page == callerPage(ctx) {
		delete(s.syncIntents, token)
	}
	s.mu.Unlock()
	if !ok || intent.page != callerPage(ctx) || time.Since(intent.at) > 2*time.Minute {
		return "", errors.New("复制确认已过期，请重新预览。")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	source, cs, _, err := s.filesystem(ctx, callerPage(ctx), intent.source)
	if err != nil {
		return "", err
	}
	defer cs()
	dest, cd, _, err := s.filesystem(ctx, callerPage(ctx), intent.dest)
	if err != nil {
		return "", err
	}
	defer cd()
	if err = intent.plan.CheckMetadata(ctx, source, dest); err != nil {
		return "", fileProblem(err)
	}
	request := TransferRequest{Source: intent.source, Destination: intent.dest, Names: []string{"比较同步"}, Policy: "overwrite"}
	return s.enqueueTransferWithReview(ctx, request, nil, intent.plan)
}
func (s *FileService) ComparisonText(ctx context.Context, id, relative, leftEncoding, rightEncoding string) (ComparedText, error) {
	p, err := s.comparison(ctx, id)
	if err != nil {
		return ComparedText{}, err
	}
	var entry *filefs.CompareEntry
	for i := range p.view.Comparison.Entries {
		if p.view.Comparison.Entries[i].Relative == relative {
			entry = &p.view.Comparison.Entries[i]
			break
		}
	}
	if entry == nil || entry.Directory {
		return ComparedText{}, errors.New("请选择比较结果中的普通文本文件。")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out := ComparedText{}
	for _, side := range []struct {
		location FileLocation
		stamp    filefs.CompareStamp
		encoding string
		target   *filefs.TextFile
	}{{p.request.Left, entry.Left, leftEncoding, &out.Left}, {p.request.Right, entry.Right, rightEncoding, &out.Right}} {
		f, closeFS, base, e := s.filesystem(ctx, callerPage(ctx), side.location)
		if e != nil {
			return out, e
		}
		target := base
		if p.view.Comparison.Directory {
			target = path.Join(base, relative)
		}
		e = func() error {
			defer closeFS()
			now, e := filefs.CompareFingerprint(ctx, f, target)
			if e != nil {
				return e
			}
			if !side.stamp.Matches(now) {
				return &filefs.Problem{Message: "文件在比较后变化，请重新比较。"}
			}
			if !side.stamp.Exists {
				*side.target = filefs.TextFile{Path: target, Encoding: "UTF-8", Content: ""}
				return nil
			}
			text, e := filefs.ReadTextEncoding(ctx, f, target, side.encoding)
			if e != nil {
				return e
			}
			after, e := filefs.CompareFingerprint(ctx, f, target)
			if e != nil {
				return e
			}
			if !side.stamp.Matches(after) {
				return &filefs.Problem{Message: "文件在读取期间变化，请重新比较。"}
			}
			*side.target = text
			return nil
		}()
		if e != nil {
			return out, fileProblem(e)
		}
	}
	return out, nil
}
