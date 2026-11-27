package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestScopedFilenameSearchHiddenAndCancellation(t *testing.T) {
	f, root := sandbox(t)
	os.MkdirAll(filepath.Join(root, "scope", "nested"), 0700)
	os.MkdirAll(filepath.Join(root, "scope", ".hidden"), 0700)
	for _, p := range []string{"scope/nested/中文-NOTE.txt", "scope/.hidden/note.txt", "scope/.note", "outside-note.txt"} {
		os.WriteFile(filepath.Join(root, filepath.FromSlash(p)), []byte("not searched contents"), 0600)
	}
	hits := []SearchHit{}
	var last SearchProgress
	err := SearchNames(context.Background(), f, "/scope", "note", false, func(p SearchProgress) error { hits = append(hits, p.Hits...); last = p; return nil })
	if err != nil || len(hits) != 1 || hits[0].Path != "/scope/nested/中文-NOTE.txt" || !last.Done {
		t.Fatal("scope", hits, err)
	}
	shallow := 0
	err = SearchNamesOptions(context.Background(), f, "/scope", "note", true, false, func(p SearchProgress) error { shallow += len(p.Hits); return nil })
	if err != nil || shallow != 1 {
		t.Fatal("nonrecursive search", shallow, err)
	}
	hits = nil
	err = SearchNames(context.Background(), f, "/scope", "note", true, func(p SearchProgress) error { hits = append(hits, p.Hits...); return nil })
	if err != nil || len(hits) != 3 {
		t.Fatal("hidden", len(hits), err)
	}
	for i := 0; i < 1100; i++ {
		os.WriteFile(filepath.Join(root, "scope", fmt.Sprintf("match-%d.txt", i)), nil, 0600)
	}
	hits = nil
	err = SearchNames(context.Background(), f, "/scope", "match", false, func(p SearchProgress) error { hits = append(hits, p.Hits...); last = p; return nil })
	if err != nil || len(hits) != 1000 || last.Reason != "result-limit" {
		t.Fatal("bounded", len(hits), last.Reason, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hits = nil
	err = SearchNames(ctx, f, "/scope", "match", false, func(p SearchProgress) error { hits = append(hits, p.Hits...); cancel(); return nil })
	if !errors.Is(err, context.Canceled) || len(hits) != 32 {
		t.Fatal("stream cancellation", len(hits), err)
	}
}
func TestFilenameSearchRejectsInvalidScopesAndNeverFollowsLinks(t *testing.T) {
	f, root := sandbox(t)
	os.Mkdir(filepath.Join(root, "scope"), 0700)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret-note.txt"), nil, 0600)
	if os.Symlink(outside, filepath.Join(root, "scope", "linked")) == nil {
		n := 0
		if e := SearchNames(context.Background(), f, "/scope", "secret", true, func(p SearchProgress) error { n += len(p.Hits); return nil }); e != nil || n != 0 {
			t.Fatal("followed link", e)
		}
	}
	for _, query := range []string{"", "../x", "a/b", "a\\b"} {
		if SearchNames(context.Background(), f, "/scope", query, true, func(SearchProgress) error { return nil }) == nil {
			t.Fatal("invalid query")
		}
	}
	if SearchNames(context.Background(), f, "/missing", "note", false, func(SearchProgress) error { return nil }) == nil {
		t.Fatal("missing root accepted")
	}
}
