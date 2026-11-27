package ipc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"yaare/internal/app"
)

type testApplicationUpdater struct {
	enabled bool
	calls   int
	check   func(context.Context) (*mygo.Update, error)
}

func (u *testApplicationUpdater) Enabled() bool { return u.enabled }
func (u *testApplicationUpdater) Check(ctx context.Context) (*mygo.Update, error) {
	u.calls++
	return u.check(ctx)
}

func TestApplicationUpdatesDistinguishUnavailableCurrentNewAndFailed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		update  *mygo.Update
		err     error
		state   string
	}{
		{"no source", false, nil, nil, "unconfigured"},
		{"current", true, nil, nil, "current"},
		{"new version", true, &mygo.Update{Version: "0.2.0", Notes: "Fixes <script>literal text</script>"}, nil, "available"},
		{"network failure", true, nil, errors.New("private local path and remote response"), "failed"},
		{"disabled", true, nil, mygo.ErrUpdatesDisabled, "unconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checker := &testApplicationUpdater{enabled: tc.enabled, check: func(ctx context.Context) (*mygo.Update, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 15*time.Second {
					t.Fatal("check must be bounded")
				}
				return tc.update, tc.err
			}}
			service := NewApplicationService(app.New())
			service.updates = checker
			service.releaseSource = ""
			status := service.checkApplicationUpdates(context.Background())
			if status.State != tc.state || status.CurrentVersion == "" {
				t.Fatal(status)
			}
			if tc.state == "available" && (status.LatestVersion != "0.2.0" || status.Notes != tc.update.Notes) {
				t.Fatal(status)
			}
			if tc.state != "available" && (status.Notes != "" || status.LatestVersion != "") {
				t.Fatal("failed/empty check must not return a release", status)
			}
			if !tc.enabled && checker.calls != 0 {
				t.Fatal("unconfigured builds must not request a feed")
			}
		})
	}
}

func TestApplicationUpdatesUsePackagedVersionAndNeverCheckWithoutNativeWindow(t *testing.T) {
	previous := mygo.App.Version()
	mygo.App.SetVersion("1.2.3-beta.4")
	defer mygo.App.SetVersion(previous)
	service := NewApplicationService(app.New())
	checker := &testApplicationUpdater{enabled: true, check: func(context.Context) (*mygo.Update, error) {
		t.Fatal("a caller without a native window must not check for updates")
		return nil, nil
	}}
	service.updates = checker
	if info := service.GetInfo(); info.Version != "1.2.3-beta.4" || info.Platform == "" || info.Architecture == "" {
		t.Fatal(info)
	}
	for _, ctx := range []context.Context{context.Background(), testOwner(context.Background())} {
		if service.GetUpdateStatus(ctx).State != "preview" || service.CheckForUpdates(ctx, false).State != "preview" {
			t.Fatal("not a native updater caller")
		}
	}
}

func TestApplicationUpdatesShareHalfHourlyChecksAcrossWindows(t *testing.T) {
	service := NewApplicationService(app.New())
	checker := &testApplicationUpdater{enabled: true, check: func(context.Context) (*mygo.Update, error) { return &mygo.Update{Version: "0.2.0"}, nil }}
	service.updates = checker
	results := make(chan ApplicationUpdateStatus, 3)
	for range 3 {
		go func() { results <- service.checkApplicationUpdatesWithPolicy(context.Background(), true) }()
	}
	for range 3 {
		if (<-results).State != "available" {
			t.Fatal("cached release")
		}
	}
	if checker.calls != 1 {
		t.Fatal("automatic checks duplicated", checker.calls)
	}
	service.checkApplicationUpdatesWithPolicy(context.Background(), false)
	service.checkApplicationUpdatesWithPolicy(context.Background(), true)
	if checker.calls != 2 {
		t.Fatal("manual check must refresh the cache", checker.calls)
	}
	service.lastUpdateAttempt = time.Now().Add(-29 * time.Minute)
	service.checkApplicationUpdatesWithPolicy(context.Background(), true)
	if checker.calls != 2 {
		t.Fatal("an automatic check ran before 30 minutes")
	}
	service.lastUpdateAttempt = time.Now().Add(-30 * time.Minute)
	service.checkApplicationUpdatesWithPolicy(context.Background(), true)
	if checker.calls != 3 {
		t.Fatal("the half-hour check did not run")
	}
}

func TestApplicationUpdatesUsePublishedSemverWithoutSignedFeed(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `[
			{"tag_name":"v0.1.9","published_at":"2026-11-19T08:00:00Z","body":"older"},
			{"tag_name":"v9.0.0","published_at":"2026-11-19T08:00:00Z","draft":true,"body":"draft"},
			{"tag_name":"v0.1.10-rc.2","published_at":"2026-11-19T08:00:00Z","body":"preview"},
			{"tag_name":"v0.1.10","published_at":"2026-11-19T08:00:00Z","prerelease":true},
			{"tag_name":"nightly","published_at":"2026-11-19T08:00:00Z","body":"unversioned"}
		]`)
	}))
	defer server.Close()
	service := NewApplicationService(app.New())
	service.updates = &testApplicationUpdater{}
	service.releaseSource = server.URL
	ctx := context.Background()
	for _, test := range []struct{ installed, available string }{
		{"0.1.9", "0.1.10"}, {"0.1.10-rc.2", "0.1.10"}, {"0.1.10", ""}, {"v0.2.0", ""},
	} {
		update, err := service.findApplicationUpdate(ctx, test.installed)
		if err != nil || (test.available == "" && update != nil) || (test.available != "" && (update == nil || update.Version != test.available)) {
			t.Fatalf("installed %s: expected %q, got %+v, %v", test.installed, test.available, update, err)
		}
	}
	releases, err := service.loadGitHubReleases(ctx)
	if err != nil || len(applicationReleaseNotes(releases)) == 0 || requests.Load() != 1 {
		t.Fatalf("expected a shared release catalog, got %d requests and %v", requests.Load(), err)
	}
}

func TestApplicationUpdatesUnavailableGitHubDoesNotReportCurrent(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	service := NewApplicationService(app.New())
	service.updates = &testApplicationUpdater{}
	service.releaseSource = server.URL
	if status := service.checkApplicationUpdates(context.Background()); status.State != "failed" || status.LatestVersion != "" {
		t.Fatal(status)
	}
}

func TestApplicationUpdateCancellationIsNotReportedAsCurrent(t *testing.T) {
	service := NewApplicationService(app.New())
	service.updates = &testApplicationUpdater{enabled: true, check: func(ctx context.Context) (*mygo.Update, error) { <-ctx.Done(); return nil, ctx.Err() }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if service.checkApplicationUpdates(ctx).State != "failed" {
		t.Fatal("cancelled request is not a successful version check")
	}
}
