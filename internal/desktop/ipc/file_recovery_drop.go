package ipc

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func (s *FileService) AuthorizeRecoveredDrop(ctx context.Context, id, token string) error {
	if err := workspaceCaller(ctx); err != nil {
		return err
	}
	return s.authorizeRecoveredDrop(callerPage(ctx), id, token)
}
func (s *FileService) authorizeRecoveredDrop(page *callerScope, id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.recoverable[id]
	if !ok || len(record.DropPaths) == 0 {
		return errors.New("拖入续传记录已处理，请刷新列表。")
	}
	grant, ok := s.drops[token]
	if !ok || grant.page != page || time.Since(grant.at) > 2*time.Minute {
		return errors.New("拖放授权无效或已过期，请重新拖入原文件。")
	}
	delete(s.drops, token)
	record.dropPage = nil
	record.authorizedDrop = nil
	s.recoverable[id] = record
	return s.authorizeRecoveryPathsLocked(page, id, grant.paths)
}

func (s *FileService) ChooseRecoveredSources(ctx context.Context, id string) (bool, error) {
	if err := workspaceCaller(ctx); err != nil {
		return false, err
	}
	page, window := callerPage(ctx), mygo.CallerWindow(ctx)
	if page == nil || window == nil {
		return false, errors.New("请在桌面窗口中选择原来源。")
	}
	s.mu.Lock()
	record, ok := s.recoverable[id]
	s.mu.Unlock()
	if !ok || len(record.DropPaths) == 0 {
		return false, errors.New("拖入续传记录已处理，请刷新列表。")
	}
	var groups [2][]string
	for _, name := range record.DropPaths {
		info, err := os.Lstat(name)
		if err != nil {
			return false, errors.New("原来源已移动或不可用，请先恢复原文件位置。")
		}
		index := 0
		if info.IsDir() {
			index = 1
		}
		groups[index] = append(groups[index], name)
	}
	var paths []string
	for index, group := range groups {
		if len(group) == 0 {
			continue
		}
		title := "重新选择原来的全部文件"
		if index == 1 {
			title = "重新选择原来的全部文件夹"
		}
		selected, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: uiText(callerPage(ctx), title), ButtonLabel: uiText(callerPage(ctx), "授权原来源"), DefaultPath: filepath.Dir(group[0]), Directory: index == 1, Multiple: true})
		if err != nil {
			return false, errors.New("无法打开来源选择框，请重试。")
		}
		if len(selected) == 0 {
			return false, nil
		}
		paths = append(paths, selected...)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.authorizeRecoveryPathsLocked(page, id, paths); err != nil {
		return false, err
	}
	return true, nil
}

func (s *FileService) authorizeRecoveryPathsLocked(page *callerScope, id string, paths []string) error {
	record, ok := s.recoverable[id]
	if !ok || len(record.DropPaths) == 0 {
		return errors.New("拖入续传记录已处理，请刷新列表。")
	}
	record.dropPage, record.authorizedDrop = nil, nil
	s.recoverable[id] = record
	normalize := func(paths []string) []string {
		result := make([]string, len(paths))
		for i, p := range paths {
			result[i] = recoveryPathKey(p)
		}
		sort.Strings(result)
		return result
	}
	expected, actual := normalize(record.DropPaths), normalize(paths)
	if len(expected) != len(actual) {
		return errors.New("请选择或拖入这条任务原来的全部文件和文件夹。")
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return errors.New("所选来源与原任务不同，未继续传输。")
		}
	}
	record.dropPage = page
	record.authorizedDrop = append([]string(nil), paths...)
	s.recoverable[id] = record
	return nil
}
