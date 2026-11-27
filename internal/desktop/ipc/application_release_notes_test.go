package ipc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchApplicationReleaseNotesIncludesPublishedPreviews(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected release notes request: %s", r.Method)
		}
		_, _ = io.WriteString(w, `[
			{"tag_name":"v0.2.0","name":"Draft","published_at":null,"draft":true,"body":"Not public"},
			{"tag_name":"v0.1.1-rc.1","name":"Preview","published_at":"2026-11-20T08:00:00Z","prerelease":true,"body":"# Notes\n- Fix layout"},
			{"tag_name":"v0.1.0","published_at":"2026-11-19T08:00:00Z","body":"  "}
		]`)
	}))
	defer server.Close()
	releases, err := fetchGitHubReleases(context.Background(), server.Client(), server.URL)
	notes := applicationReleaseNotes(releases)
	if err != nil || len(notes) != 1 || notes[0].Tag != "v0.1.1-rc.1" || notes[0].Body != "# Notes\n- Fix layout" {
		t.Fatalf("expected only the published preview with notes, got %+v, %v", notes, err)
	}
}

func TestFetchApplicationReleaseNotesRejectsUnavailableOrInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing repository", http.StatusNotFound, "{}"},
		{"invalid JSON", http.StatusOK, "{"},
		{"missing releases list", http.StatusOK, "null"},
		{"oversized response", http.StatusOK, strings.Repeat(" ", maxReleaseNotesSize+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			notes, err := fetchGitHubReleases(context.Background(), server.Client(), server.URL)
			if err == nil || len(notes) != 0 {
				t.Fatalf("expected a failed read, got %+v, %v", notes, err)
			}
		})
	}
}
