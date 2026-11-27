package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

const (
	yaareReleaseNotesURL = "https://api.github.com/repos/plumbral/yaare/releases?per_page=10"
	maxReleaseNotesSize  = 512 * 1024
)

type ApplicationReleaseNote struct {
	Tag         string `json:"tag"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	PublishedAt string `json:"publishedAt"`
}

type githubRelease struct {
	Tag         string `json:"tag_name"`
	Title       string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
}

func (s *ApplicationService) GetReleaseNotes(ctx context.Context) []ApplicationReleaseNote {
	if mygo.CallerWindow(ctx) == nil {
		return nil
	}
	releases, _ := s.loadGitHubReleases(ctx)
	return applicationReleaseNotes(releases)
}

func (s *ApplicationService) loadGitHubReleases(ctx context.Context) ([]githubRelease, error) {
	s.releaseMu.Lock()
	defer s.releaseMu.Unlock()
	if time.Since(s.releaseCheckedAt) < 5*time.Minute {
		return s.releases, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	releases, err := fetchGitHubReleases(ctx, http.DefaultClient, s.releaseSource)
	if err == nil {
		s.releases = releases
		s.releaseCheckedAt = time.Now()
	}
	return s.releases, err
}

func fetchGitHubReleases(ctx context.Context, client *http.Client, source string) ([]githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Yaare")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release notes request returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseNotesSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxReleaseNotesSize {
		return nil, fmt.Errorf("release notes response exceeded size limit")
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, err
	}
	if releases == nil {
		return nil, fmt.Errorf("missing GitHub releases list")
	}
	return releases, nil
}

func applicationReleaseNotes(releases []githubRelease) []ApplicationReleaseNote {
	notes := make([]ApplicationReleaseNote, 0, min(len(releases), 10))
	for _, release := range releases {
		body := strings.TrimSpace(release.Body)
		if release.Draft || release.PublishedAt == "" || release.Tag == "" || body == "" {
			continue
		}
		notes = append(notes, ApplicationReleaseNote{
			Tag: release.Tag, Title: strings.TrimSpace(release.Title), Body: body, PublishedAt: release.PublishedAt,
		})
		if len(notes) == 10 {
			break
		}
	}
	return notes
}
