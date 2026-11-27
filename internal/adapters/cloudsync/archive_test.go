package cloudsync

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreCopyRetention(t *testing.T) {
	for _, count := range []int{30, 65} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			folder := t.TempDir()
			now := time.Now().UTC()
			write := func(name string, at time.Time) {
				t.Helper()
				path := filepath.Join(folder, name)
				if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, at, at); err != nil {
					t.Fatal(err)
				}
			}
			for i := range count {
				at := now.Add(-time.Duration(i) * time.Hour)
				if count == 30 && i >= 20 {
					at = at.Add(-31 * 24 * time.Hour)
				}
				if i%2 == 0 {
					write(fmt.Sprintf("before-import-%032x.yaare", i), at)
				} else {
					write(fmt.Sprintf("restore-sync-%s-%032x.yaare", at.Format(restoreTimestamp), i), at)
				}
			}
			unknown := []string{"my-export.yaare", "before-import-fake.yaare", "restore-sync-20269999T000000Z-00000000000000000000000000000000.yaare", "keep.txt"}
			for _, name := range unknown {
				write(name, now.Add(-90*24*time.Hour))
			}
			pruneRestoreCopies(folder, "", now)
			entries, err := os.ReadDir(folder)
			if err != nil {
				t.Fatal(err)
			}
			want := min(count, portableBackupMaxKeep)
			if count == 30 {
				want = portableBackupMinKeep
			}
			if len(entries) != want+len(unknown) {
				t.Fatalf("retained %d files, expected %d", len(entries), want+len(unknown))
			}
			for _, name := range unknown {
				if _, err = os.Stat(filepath.Join(folder, name)); err != nil {
					t.Fatal("unmanaged file pruned", name, err)
				}
			}
		})
	}
}

func TestReadImportRestoreCopyValidatesEnvelopeAndSize(t *testing.T) {
	root := t.TempDir()
	blob, err := Seal([]byte("fixture"), "test backup password")
	if err != nil {
		t.Fatal(err)
	}
	id, err := SaveRestoreCopy(t.Context(), root, blob, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if data, e := ReadImportRestoreCopy(root, id); e != nil || string(data) != string(blob) {
		t.Fatal("cannot read saved restore copy", e)
	}
	path := filepath.Join(root, "portable-backups", "before-import-"+id+".yaare")
	if err = os.WriteFile(path, blob[:headerSize], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadImportRestoreCopy(root, id); err == nil {
		t.Fatal("accepted truncated envelope")
	}
	if err = os.Truncate(path, MaxBlob+1); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadImportRestoreCopy(root, id); err == nil {
		t.Fatal("accepted oversized archive")
	}
}
