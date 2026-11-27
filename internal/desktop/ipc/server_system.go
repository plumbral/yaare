package ipc

import (
	"context"
	"time"

	"yaare/internal/adapters/server"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/domain/workspace"
)

var systemProbeSlots = make(chan struct{}, 8)

func (s *SSHService) ProbeServerSystem(ctx context.Context, sessionID string, attempt int) *workspace.ServerSystem {
	run := s.ownedRun(ctx, sessionID, attempt)
	if run == nil || !run.session.Connected() || ctx.Err() != nil {
		return nil
	}
	started := time.Now().UnixNano()
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	go func() {
		select {
		case <-run.session.Done():
			cancel()
		case <-probeCtx.Done():
		}
	}()
	select {
	case systemProbeSlots <- struct{}{}:
	case <-probeCtx.Done():
		return nil
	case <-run.session.Done():
		return nil
	}
	defer func() { <-systemProbeSlots }()
	if probeCtx.Err() != nil || !run.session.Connected() {
		return nil
	}
	info, err := server.DetectSystem(probeCtx, run.session.OpenCommand)
	if err != nil || !info.Valid() || probeCtx.Err() != nil {
		return nil
	}
	if s.ownedRun(ctx, sessionID, attempt) != run || !run.session.Connected() {
		return nil
	}
	if s.workspace != nil {
		_ = s.workspace.SaveServerSystem(probeCtx, run.profileID, run.systemRoute, started, info)
	}
	if probeCtx.Err() != nil {
		return nil
	}
	return &info
}

func activeSystemRoute(request SSHConnectRequest, cfg sshclient.Config) string {
	if len(request.JumpHostIDs) != len(cfg.Jumps) {
		return ""
	}
	host := workspace.Host{Address: cfg.Host, Port: cfg.Port, Username: cfg.Username, Proxy: request.Proxy, JumpHostIDs: request.JumpHostIDs}
	hops := make(map[string]workspace.Host, len(cfg.Jumps))
	for i, jump := range cfg.Jumps {
		hops[request.JumpHostIDs[i]] = workspace.Host{Address: jump.Host, Port: jump.Port, Username: jump.Username}
	}
	return workspace.ServerSystemRoute(host, func(id string) (workspace.Host, bool) {
		h, ok := hops[id]
		return h, ok
	})
}
