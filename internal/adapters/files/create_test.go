package files

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCreateEmptyExclusiveAndConcurrent(t *testing.T) {
	f, root := sandbox(t)
	ctx := context.Background()
	if err := CreateEmpty(ctx, f, "/", "新文件.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "新文件.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if CreateEmpty(ctx, f, "/", "新文件.txt") == nil {
		t.Fatal("overwrote existing file")
	}
	data, _ := os.ReadFile(filepath.Join(root, "新文件.txt"))
	if string(data) != "original" {
		t.Fatal("existing bytes changed")
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- CreateEmpty(ctx, f, "/", "concurrent.txt") }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("exclusive create count", success)
	}
	for _, name := range []string{"", "..", "a/b", "a\\b", "a\x00b", "line\nbreak"} {
		if CreateEmpty(ctx, f, "/", name) == nil {
			t.Fatal("accepted invalid name", name)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if CreateEmpty(canceled, f, "/", "canceled.txt") == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := os.Stat(filepath.Join(root, "canceled.txt")); !os.IsNotExist(err) {
		t.Fatal("created canceled file")
	}
}
