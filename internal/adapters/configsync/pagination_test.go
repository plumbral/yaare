package configsync

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestS3ListingPaginationAndTokenEncoding(t *testing.T) {
	calls := 0
	first := "backups/20261117T000000Z-" + strings.Repeat("a", 32) + ".yaaresync"
	second := "backups/20261117T000001Z-" + strings.Repeat("b", 32) + ".yaaresync"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("prefix") != "space + 中文/backups/" {
			t.Error("prefix changed")
		}
		w.Header().Set("Content-Type", "application/xml")
		if calls == 1 {
			fmt.Fprintf(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>token+/=%%</NextContinuationToken><Contents><Key>space + 中文/%s</Key><Size>10</Size></Contents></ListBucketResult>`, first)
		} else {
			if r.URL.Query().Get("continuation-token") != "token+/=%" {
				t.Error("token not preserved")
			}
			fmt.Fprintf(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>space + 中文/%s</Key><Size>20</Size></Contents><Contents><Key>%s</Key></Contents><Contents><Key>space + 中文/%s.other</Key></Contents></ListBucketResult>`, second, first, strings.TrimSuffix(first, ObjectExtension))
		}
	}))
	defer server.Close()
	h, e := NewHTTP(Config{Provider: "s3", Endpoint: server.URL, Bucket: "bucket", Region: "us-east-1", Prefix: "space + 中文", Username: "key"}, []byte("secret"))
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	objects, e := h.List(context.Background())
	if e != nil || calls != 2 || len(objects) != 2 || objects[0].Name != second {
		t.Fatal(objects, calls, e)
	}
}

func TestS3SigV4TildeEncodingVirtualHostedAndChunkedErrorReuse(t *testing.T) {
	var gotRawQuery string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			gotRawQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
			return
		}
		if r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/current.yaaresync") {
			w.Header().Set("ETag", `"head-etag"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		flusher, _ := w.(http.Flusher)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "<Error><Code>NoSuchKey</Code></Error>")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	var conns atomic.Int32
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	h, err := NewHTTP(Config{Provider: "s3", Endpoint: server.URL, Bucket: "bucket", Region: "us-east-1", Prefix: "~user/yaare", Username: "key"}, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err = h.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotRawQuery, "%7E") || !strings.Contains(gotRawQuery, "prefix=~user%2Fyaare%2Fbackups%2F") {
		t.Fatalf("SigV4 query did not preserve tilde: %q", gotRawQuery)
	}
	if _, _, err = h.Get(t.Context(), "current.yaaresync"); err != ErrMissing {
		t.Fatalf("expected ErrMissing, got %v", err)
	}
	if tag, err := h.Head(t.Context(), "current.yaaresync"); err != nil || tag != `"head-etag"` {
		t.Fatalf("HEAD failed: tag=%q err=%v", tag, err)
	}
	if conns.Load() != 1 {
		t.Fatalf("chunked error body prevented keep-alive reuse: %d connections", conns.Load())
	}

	vh, err := NewHTTP(Config{Provider: "s3", Endpoint: "https://my-bucket.s3.us-east-1.amazonaws.com", Bucket: "my-bucket", Region: "us-east-1", Prefix: "yaare"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer vh.Close()
	if got := vh.objectURL("current.yaaresync").Path; got != "/yaare/current.yaaresync" {
		t.Fatalf("virtual-hosted objectURL duplicated bucket: %q", got)
	}
	ps, err := NewHTTP(Config{Provider: "s3", Endpoint: "https://s3.us-east-1.amazonaws.com", Bucket: "my-bucket", Region: "us-east-1", Prefix: "yaare"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()
	if got := ps.objectURL("current.yaaresync").Path; got != "/my-bucket/yaare/current.yaaresync" {
		t.Fatalf("path-style objectURL missing bucket: %q", got)
	}
}

func TestS3PruneBackupsAbove4000AndWebDAVStaleProbeCleanup(t *testing.T) {
	const total = 4200
	names := make([]string, total)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < total; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Minute).Format("20060102T150405Z")
		names[i] = fmt.Sprintf("backups/%s-%032x.yaaresync", ts, i)
	}
	var mu sync.Mutex
	deleted := map[string]bool{}
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodDelete {
			key := strings.TrimPrefix(r.URL.Path, "/bucket/")
			deleted[key] = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		start := 0
		if tok := r.URL.Query().Get("continuation-token"); tok != "" {
			_, _ = fmt.Sscanf(tok, "p%d", &start)
		}
		end := start + 1000
		if end > len(names) {
			end = len(names)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<ListBucketResult>`)
		if end < len(names) {
			fmt.Fprintf(w, `<IsTruncated>true</IsTruncated><NextContinuationToken>p%d</NextContinuationToken>`, end)
		} else {
			_, _ = io.WriteString(w, `<IsTruncated>false</IsTruncated>`)
		}
		for _, n := range names[start:end] {
			if !deleted[n] {
				fmt.Fprintf(w, `<Contents><Key>%s</Key><Size>128</Size></Contents>`, n)
			}
		}
		_, _ = io.WriteString(w, `</ListBucketResult>`)
	}))
	defer s3Server.Close()

	s3Client, err := NewHTTP(Config{Provider: "s3", Endpoint: s3Server.URL, Bucket: "bucket", Region: "us-east-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s3Client.Close()

	listed, err := s3Client.List(t.Context())
	if err != nil || len(listed) != maxListedBackups || listed[0].Name != names[total-1] {
		t.Fatalf("List above 4000 failed or lost newest backup: len=%d first=%v err=%v", len(listed), listed[:min(1, len(listed))], err)
	}
	if err = s3Client.PruneBackups(t.Context(), 120, "20260101T000000Z"); err != nil {
		t.Fatalf("PruneBackups above 4000 failed: %v", err)
	}
	mu.Lock()
	deletedCount := len(deleted)
	deletedNewest := deleted[names[total-1]]
	mu.Unlock()
	if deletedCount != maxPruneDeletes || deletedNewest {
		t.Fatalf("expected bounded incremental prune preserving newest: deleted=%d newestDeleted=%v", deletedCount, deletedNewest)
	}

	staleProbe := "probe-" + strings.Repeat("a", 32)
	freshProbe := "probe-" + strings.Repeat("b", 32)
	var davDeleted []string
	davServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			if strings.HasSuffix(r.URL.Path, "/backups/") {
				_, _ = io.WriteString(w, `<d:multistatus xmlns:d="DAV:"></d:multistatus>`)
				return
			}
			oldTime := time.Now().UTC().Add(-30 * time.Minute).Format(http.TimeFormat)
			newTime := time.Now().UTC().Add(-1 * time.Minute).Format(http.TimeFormat)
			fmt.Fprintf(w, `<d:multistatus xmlns:d="DAV:">
				<d:response><d:href>/%s</d:href><d:propstat><d:prop><d:getlastmodified>%s</d:getlastmodified></d:prop></d:propstat></d:response>
				<d:response><d:href>/%s</d:href><d:propstat><d:prop><d:getlastmodified>%s</d:getlastmodified></d:prop></d:propstat></d:response>
				<d:response><d:href>/current.yaaresync</d:href><d:propstat><d:prop><d:getlastmodified>%s</d:getlastmodified></d:prop></d:propstat></d:response>
			</d:multistatus>`, staleProbe, oldTime, freshProbe, newTime, oldTime)
		case http.MethodDelete:
			davDeleted = append(davDeleted, strings.TrimPrefix(r.URL.Path, "/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer davServer.Close()

	davClient, err := NewHTTP(Config{Provider: "webdav", Endpoint: davServer.URL, Username: "u"}, []byte("s"))
	if err != nil {
		t.Fatal(err)
	}
	defer davClient.Close()
	if err = davClient.PruneBackups(t.Context(), 120, "20260101T000000Z"); err != nil {
		t.Fatal(err)
	}
	if len(davDeleted) != 1 || davDeleted[0] != staleProbe {
		t.Fatalf("unexpected WebDAV probe cleanup deletions: %v", davDeleted)
	}
}
