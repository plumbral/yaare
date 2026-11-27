package sessionlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFixture(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func fixtureIndex(t *testing.T, path, id, state string) {
	t.Helper()
	info := Info{ID: id, SessionID: "pane", Attempt: 1, Started: 100, State: state}
	if err := writeIndex(path, info); err != nil {
		t.Fatal(err)
	}
}

func TestInitializeSkipsBadRecordsAndPreservesFiles(t *testing.T) {
	store, root := fresh(t)
	folder := store.path
	bad := map[string][]byte{
		"broken.json":    []byte("{index-marker"),
		"broken.jsonl":   []byte("preserved transcript"),
		"big.json":       bytes.Repeat([]byte("x"), maxIndexBytes+1),
		"wrong.jsonl":    []byte("wrong id transcript"),
		"invalid.jsonl":  []byte("invalid state transcript"),
		"orphan.jsonl":   []byte("orphan transcript"),
		".index-old.tmp": []byte("interrupted temporary file"),
	}
	for name, content := range bad {
		writeFixture(t, filepath.Join(folder, name), content)
	}
	fixtureIndex(t, filepath.Join(folder, "wrong.json"), "other-id", "stopped")
	fixtureIndex(t, filepath.Join(folder, "invalid.json"), "invalid", "unknown")
	fixtureIndex(t, filepath.Join(folder, "missing.json"), "missing", "stopped")
	fixtureIndex(t, filepath.Join(folder, "large.json"), "large", "stopped")
	large := filepath.Join(folder, "large.jsonl")
	writeFixture(t, large, nil)
	if err := os.Truncate(large, MaxBytes+1); err != nil {
		t.Fatal(err)
	}
	fixtureIndex(t, filepath.Join(folder, "good.json"), "good", "recording")
	transcript := []byte("{\"time\":100,\"text\":\"intact output\"}\n")
	writeFixture(t, filepath.Join(folder, "good.jsonl"), transcript)
	reader := New(func() (string, error) { return root, nil })
	defer reader.Close()
	if err := reader.Initialize(); err != nil {
		t.Fatal(err)
	}
	list, err := reader.List()
	if err != nil || len(list) != 1 || list[0].ID != "good" || list[0].State != "stopped" || list[0].Bytes != int64(len(transcript)) {
		t.Fatal("usable recording or interrupted state was not recovered", err)
	}
	for name, want := range bad {
		got, err := os.ReadFile(filepath.Join(folder, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("skipped file %s was changed: %v", name, err)
		}
	}
	index, ok := loadIndex(filepath.Join(folder, "good.json"), "good")
	if !ok || index.State != "recording" {
		t.Fatal("opening history rewrote the original index")
	}
	retained := MaxBytes + 1
	for name, content := range bad {
		if strings.HasSuffix(name, ".jsonl") {
			retained += int64(len(content))
		}
	}
	if reader.retainedBytes != retained {
		t.Fatalf("retained files occupy %d bytes, accounted %d", retained, reader.retainedBytes)
	}
	if _, err := reader.Start("pane", 2, "new recording", nil); err != nil {
		t.Fatal("damaged history blocked an otherwise within-budget recording", err)
	}
}

func TestReadAndExportSkipBadRowsWithoutLosingPagination(t *testing.T) {
	store, _ := fresh(t)
	record, err := store.Start("pane", 1, "mixed-validity", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := stop(t, record)
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for number := range 200 {
		if err := encoder.Encode(Line{Time: 100, Text: fmt.Sprintf("saved row %d\n", number)}); err != nil {
			t.Fatal(err)
		}
	}
	data.WriteString("{raw-index-marker\nnull\n{\"time\":100,\"text\":null}\n")
	data.WriteString("{\"time\":0,\"text\":\"bad timestamp\"}\n")
	data.Write([]byte("{\"time\":100,\"text\":\"\xff\"}\n"))
	data.Write(bytes.Repeat([]byte("x"), 2*maxLineBytes))
	data.WriteByte('\n')
	if err := encoder.Encode(Line{Time: 200, Text: "after damaged rows"}); err != nil {
		t.Fatal(err)
	}
	data.WriteString("{\"time\":250,\"text\":\"torn tail")
	path := record.path + ".jsonl"
	writeFixture(t, path, data.Bytes())
	ctx := context.Background()
	page, err := store.Read(ctx, Query{ID: info.ID})
	if err != nil || len(page.Lines) != 200 || !page.More || page.Next <= 0 || page.Next >= data.Len() {
		t.Fatalf("first page: count=%d, next=%d, more=%v, err=%v", len(page.Lines), page.Next, page.More, err)
	}
	next, err := store.Read(ctx, Query{ID: info.ID, Cursor: page.Next, Text: "AFTER", After: 200})
	if err != nil || len(next.Lines) != 1 || next.Lines[0].Text != "after damaged rows" || next.More || next.Next != data.Len() {
		t.Fatalf("malformed or oversized rows interrupted the next page: count=%d next=%d want=%d more=%v err=%v", len(next.Lines), next.Next, data.Len(), next.More, err)
	}
	var output strings.Builder
	if err := store.Export(ctx, info.ID, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "saved row ") != 200 || !strings.HasSuffix(output.String(), "after damaged rows\n") ||
		strings.Contains(output.String(), "raw-index-marker") || strings.Contains(output.String(), "bad timestamp") || strings.Contains(output.String(), "torn tail") {
		t.Fatal("export lost valid data or included a malformed record")
	}
	found, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(found, data.Bytes()) {
		t.Fatal("reading history changed the original transcript", err)
	}
}

func TestBufferedRecordingFlushesWhileActiveAndCommitsFinalIndex(t *testing.T) {
	store, _ := fresh(t)
	record, err := store.Start("pane", 1, "buffered", nil)
	if err != nil {
		t.Fatal(err)
	}
	started := record.Info()
	record.Capture([]byte("completed line\npartial line"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := store.Read(context.Background(), Query{ID: started.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Lines) == 2 {
			if page.Lines[0].Text != "completed line\n" || page.Lines[1].Text != "partial line" || record.Info().State != "recording" {
				t.Fatal("periodic flush did not preserve both complete and partial output")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("buffer did not flush while recording")
		}
		time.Sleep(20 * time.Millisecond)
	}
	record.Capture([]byte("final line"))
	stopped := stop(t, record)
	page, err := store.Read(context.Background(), Query{ID: stopped.ID})
	index, indexed := loadIndex(record.path+".json", stopped.ID)
	file, statErr := os.Stat(record.path + ".jsonl")
	if err != nil || statErr != nil || len(page.Lines) != 3 || page.Lines[2].Text != "final line" ||
		!indexed || index.State != "stopped" || index.Ended == 0 || index.Bytes != file.Size() || index != stopped {
		t.Fatal("stop did not drain and persist a consistent recording", err, statErr)
	}
}

func TestLimitedRecordingFlushesAcceptedRows(t *testing.T) {
	store, _ := fresh(t)
	record, err := store.Start("pane", 1, "limited buffer", nil)
	if err != nil {
		t.Fatal(err)
	}
	record.max = 64
	record.Capture([]byte("kept\n" + strings.Repeat("x", 200) + "\n"))
	select {
	case <-record.done:
	case <-time.After(3 * time.Second):
		t.Fatal("limited recording did not finish")
	}
	info := stop(t, record)
	page, err := store.Read(context.Background(), Query{ID: info.ID})
	if err != nil || info.State != "limited" || info.Bytes > 64 || len(page.Lines) != 1 || page.Lines[0].Text != "kept\n" {
		t.Fatal("recording limit discarded accepted buffered output", err)
	}
}

func TestIndexFailurePreservesTranscriptAndDoesNotExposePrivateData(t *testing.T) {
	store, _ := fresh(t)
	record, err := store.Start("pane", 1, "private-name-marker", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := record.path + ".json"
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(path, "marker"), []byte("keep me"))
	record.Capture([]byte("private-output-marker\n"))
	info := stop(t, record)
	if info.State != "limited" || info.Message == "" || strings.Contains(info.Message, "private-") || strings.Contains(info.Message, store.path) || strings.Contains(info.Message, record.Info().ID) {
		t.Fatal("failed index commit did not yield a generic status")
	}
	transcript, err := os.ReadFile(record.path + ".jsonl")
	if err != nil || !bytes.Contains(transcript, []byte("private-output-marker")) {
		t.Fatal("failed index commit discarded recorded output", err)
	}
	if marker, err := os.ReadFile(filepath.Join(path, "marker")); err != nil || string(marker) != "keep me" {
		t.Fatal("failed rename damaged the target", err)
	}
	if files, err := filepath.Glob(filepath.Join(store.path, ".index-*.tmp")); err != nil || len(files) != 0 {
		t.Fatal("failed rename abandoned a new temporary file", err)
	}
}

func TestActiveAndPreservedFilesCountAgainstGlobalBudget(t *testing.T) {
	store, _ := fresh(t)
	store.retainedBytes = 1
	for attempt := 1; attempt < int(TotalBytes/MaxBytes); attempt++ {
		if _, err := store.Start("pane", attempt, "active", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Start("pane", 20, "over budget", nil); err == nil {
		t.Fatal("empty active recordings and preserved data did not reserve their full budget")
	}
}

func TestIndexCommitWithLongDataPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("long-folder-", 9), strings.Repeat("data-folder-", 9), strings.Repeat("session-folder-", 7))
	store := New(func() (string, error) { return root, nil })
	defer store.Close()
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	record, err := store.Start("pane", 1, "long path", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := stop(t, record)
	if index, ok := loadIndex(record.path+".json", info.ID); !ok || index != info || index.State != "stopped" {
		t.Fatal("durable rename failed for a long data path")
	}
}

func TestOversizedHistoryStillListsUpToRecordLimit(t *testing.T) {
	store, root := fresh(t)
	for number := range MaxRecords + 1 {
		id := fmt.Sprintf("record-%03d", number)
		data, err := json.Marshal(Info{ID: id, SessionID: "pane", Attempt: 1, Started: 100, State: "stopped"})
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(store.path, id+".json"), data)
		writeFixture(t, filepath.Join(store.path, id+".jsonl"), nil)
	}
	reader := New(func() (string, error) { return root, nil })
	defer reader.Close()
	if err := reader.Initialize(); err != nil {
		t.Fatal(err)
	}
	list, err := reader.List()
	if err != nil || len(list) != MaxRecords {
		t.Fatalf("over-limit history listed %d records: %v", len(list), err)
	}
	if _, err := reader.Start("pane", 2, "over record limit", nil); err == nil {
		t.Fatal("record count limit was bypassed")
	}
	if files, err := filepath.Glob(filepath.Join(store.path, "*.jsonl")); err != nil || len(files) != MaxRecords+1 {
		t.Fatal("opening over-limit history removed an original", err)
	}
}

func TestReadSeeksDirectlyToByteOffsetCursor(t *testing.T) {
	store, _ := fresh(t)
	record, err := store.Start("pane", 1, "seek-cursor", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := stop(t, record)
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for i := range 205 {
		if err := encoder.Encode(Line{Time: int64(1000 + i), Text: fmt.Sprintf("line-%03d\n", i)}); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, record.path+".jsonl", data.Bytes())
	first, err := store.Read(context.Background(), Query{ID: info.ID})
	if err != nil || len(first.Lines) != 200 || !first.More {
		t.Fatalf("first page failed: %d %v %v", len(first.Lines), first.More, err)
	}
	raw := data.Bytes()
	copy(raw[:first.Next], bytes.Repeat([]byte("X"), first.Next))
	writeFixture(t, record.path+".jsonl", raw)
	second, err := store.Read(context.Background(), Query{ID: info.ID, Cursor: first.Next})
	if err != nil || len(second.Lines) != 5 || second.Lines[0].Text != "line-200\n" || second.Lines[4].Text != "line-204\n" || second.More {
		t.Fatalf("byte-offset cursor did not seek directly: %+v %v", second, err)
	}
}
