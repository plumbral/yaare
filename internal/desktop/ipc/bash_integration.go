package ipc

import (
	"context"
	"errors"
	"strconv"

	sshclient "yaare/internal/adapters/ssh"
)

func (s *SSHService) PreviewBashIntegration(ctx context.Context, id string, attempt int) (sshclient.BashIntegrationPreview, error) {
	run := s.owned(ctx, id, attempt)
	if run == nil {
		return sshclient.BashIntegrationPreview{}, errors.New("SSH 已断开，请重新连接。")
	}
	return run.ManageBashIntegration(ctx, id+":"+strconv.Itoa(attempt), "", "")
}

func (s *SSHService) ApplyBashIntegration(ctx context.Context, id string, attempt int, action, revision string) error {
	run := s.owned(ctx, id, attempt)
	if run == nil {
		return errors.New("SSH 已断开，请重新连接。")
	}
	if action != "install" && action != "remove" || len(revision) != 64 {
		return errors.New("Bash 集成配置已变化，请重新预览。")
	}
	_, err := run.ManageBashIntegration(ctx, id+":"+strconv.Itoa(attempt), action, revision)
	return err
}
