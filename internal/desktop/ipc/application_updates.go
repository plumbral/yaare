package ipc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"golang.org/x/mod/semver"
)

const automaticUpdateInterval = 30 * time.Minute

type applicationUpdateChecker interface {
	Enabled() bool
	Check(context.Context) (*mygo.Update, error)
}

type ApplicationUpdateStatus struct {
	State          string `json:"state"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Notes          string `json:"notes"`
	CheckedAt      int64  `json:"checkedAt"`
}

func (s *ApplicationService) GetUpdateStatus(ctx context.Context) ApplicationUpdateStatus {
	status := ApplicationUpdateStatus{State: "unconfigured", CurrentVersion: s.GetInfo().Version}
	if mygo.CallerWindow(ctx) == nil {
		status.State = "preview"
	} else if s.releaseSource != "" || s.updates.Enabled() {
		status.State = "idle"
	}
	return status
}

func (s *ApplicationService) CheckForUpdates(ctx context.Context, automatic bool) ApplicationUpdateStatus {
	status := s.GetUpdateStatus(ctx)
	if status.State != "idle" {
		return status
	}
	return s.checkApplicationUpdatesWithPolicy(ctx, automatic)
}

func (s *ApplicationService) checkApplicationUpdatesWithPolicy(ctx context.Context, automatic bool) ApplicationUpdateStatus {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if automatic && time.Since(s.lastUpdateAttempt) < automaticUpdateInterval {
		return s.lastUpdateStatus
	}
	s.lastUpdateStatus = s.checkApplicationUpdates(ctx)
	s.lastUpdateAttempt = time.Now()
	return s.lastUpdateStatus
}

func (s *ApplicationService) checkApplicationUpdates(ctx context.Context) ApplicationUpdateStatus {
	status := ApplicationUpdateStatus{State: "unconfigured", CurrentVersion: s.GetInfo().Version}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	update, err := s.findApplicationUpdate(bounded, status.CurrentVersion)
	if errors.Is(err, mygo.ErrUpdatesDisabled) {
		return status
	}
	status.CheckedAt = time.Now().UnixMilli()
	if err != nil {
		status.State = "failed"
	} else if update == nil {
		status.State = "current"
	} else {
		status.State = "available"
		status.LatestVersion = update.Version
		status.Notes = update.Notes
	}
	return status
}

func (s *ApplicationService) findApplicationUpdate(ctx context.Context, current string) (*mygo.Update, error) {
	if s.updates.Enabled() {
		return s.updates.Check(ctx)
	}
	if s.releaseSource == "" {
		return nil, mygo.ErrUpdatesDisabled
	}
	version := "v" + strings.TrimPrefix(current, "v")
	if !semver.IsValid(version) {
		return nil, errors.New("invalid Yaare version")
	}
	releases, err := s.loadGitHubReleases(ctx)
	if err != nil {
		return nil, err
	}
	var update *mygo.Update
	for _, release := range releases {
		candidate := "v" + strings.TrimPrefix(release.Tag, "v")
		if release.Draft || release.PublishedAt == "" || !semver.IsValid(candidate) || semver.Compare(candidate, version) <= 0 {
			continue
		}
		version = candidate
		update = &mygo.Update{Version: strings.TrimPrefix(candidate, "v"), Notes: strings.TrimSpace(release.Body)}
	}
	return update, nil
}
