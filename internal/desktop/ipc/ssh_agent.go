package ipc

import (
	"context"
	"yaare/internal/adapters/sshagent"
)

func (s *SSHService) AgentInfo(ctx context.Context) (sshagent.AgentInfo, error) {
	if err := workspaceCaller(ctx); err != nil {
		return sshagent.AgentInfo{}, err
	}
	return sshagent.Probe(ctx), nil
}
