package ipc

import (
	"context"

	"sort"
)

type FileConnection struct {
	ID      string `json:"id"`
	Attempt int    `json:"attempt"`
	Name    string `json:"name"`
}

func (s *FileService) FileConnections(ctx context.Context) ([]FileConnection, error) {
	if e := workspaceCaller(ctx); e != nil {
		return nil, e
	}
	return s.fileConnections(callerPage(ctx)), nil
}
func (s *FileService) fileConnections(page *callerScope) []FileConnection {
	result := []FileConnection{}
	if s.ssh == nil {
		return result
	}
	s.ssh.mu.Lock()
	defer s.ssh.mu.Unlock()
	for id, run := range s.ssh.runs {
		if run.page == page && run.session.Connected() {
			result = append(result, FileConnection{id, run.attempt, run.name})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result
}
