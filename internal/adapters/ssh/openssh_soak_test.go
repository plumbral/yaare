package sshclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	filefs "yaare/internal/adapters/files"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealOpenSSHMixedLoadSoak(t *testing.T) {
	seconds, _ := strconv.Atoi(os.Getenv("YAARE_SOAK_SECONDS"))
	if seconds == 0 {
		t.Skip("bounded mixed-load soak opt-in")
	}
	if seconds < 60 || seconds > 7200 {
		t.Fatal("soak duration must be 60..7200 seconds")
	}
	artifacts := t.TempDir()
	if parent := os.Getenv("YAARE_SOAK_ARTIFACT_DIR"); parent != "" {
		if !filepath.IsAbs(parent) {
			t.Fatal("artifact directory must be absolute")
		}
		var err error
		artifacts, err = os.MkdirTemp(parent, "yaare-ssh-soak-")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("soak artifacts: %s", artifacts)
	}
	left, right := realFixture(t, "yaaretest"), realFixture(t, "yaaretest")
	left.wait(t, "connected")
	right.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+40)*time.Second)
	defer cancel()
	lc, cl, e := left.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer cl()
	rc, cr, e := right.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer cr()
	remoteL, remoteR := filefs.Remote{Client: lc}, filefs.Remote{Client: rc}
	base := path.Join("/home/yaaretest/files", fmt.Sprintf("yaare-soak-%d", time.Now().UnixNano()))
	if e = lc.Mkdir(base); e != nil {
		t.Fatal(e)
	}
	defer func() {
		budget := 30
		if e := filefs.RemoveTree(context.Background(), remoteL, base, &budget); e != nil {
			t.Error("owned remote cleanup", e)
		}
	}()
	for _, dir := range []string{"source", "target"} {
		if e = lc.Mkdir(path.Join(base, dir)); e != nil {
			t.Fatal(e)
		}
	}
	localDir, e := os.MkdirTemp(artifacts, "mixed-soak-")
	if e != nil {
		t.Fatal(e)
	}
	data := bytes.Repeat([]byte("soak binary \x00\xff 中文\n"), 120000)
	name := "中文 ' literal.bin"
	if e = os.WriteFile(filepath.Join(localDir, name), data, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(localDir, "download"), 0700); e != nil {
		t.Fatal(e)
	}
	root, e := os.OpenRoot(localDir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	local := filefs.Local{Root: root}
	expected := sha256.Sum256(data)
	started := time.Now()
	cycles := 0
	copies := 0
	peakGoroutines := 0
	type sample struct {
		Seconds    float64 `json:"seconds"`
		Cycles     int     `json:"cycles"`
		HeapMiB    float64 `json:"heapMiB"`
		Goroutines int     `json:"goroutines"`
	}
	var samples []sample
	copyFile := func(source, target filefs.FS, sourceDir, targetDir, job string) {
		t.Helper()
		op, stop := context.WithTimeout(ctx, 15*time.Second)
		defer stop()
		plan, total, e := filefs.Plan(op, source, sourceDir, []string{name})
		if e != nil {
			t.Fatal(e)
		}
		checkpoint := filefs.CopyCheckpoint{}
		if e = filefs.CopyResumable(op, source, target, plan, targetDir, job, total, &checkpoint, func(string, bool) (string, error) { return "overwrite", nil }, func(filefs.Progress) {}, func(string) {}); e != nil {
			t.Fatal("mixed copy", e)
		}
		copies++
	}
	for time.Since(started) < time.Duration(seconds)*time.Second {
		cycleAt := time.Now()
		if cycles%2 == 0 {
			scp := filefs.SCP{FS: remoteL, Context: ctx, OpenCommand: left.session.OpenCommand}
			copyFile(local, scp, "/", path.Join(base, "source"), fmt.Sprintf("soak-scp-%d", cycles))
		} else {
			copyFile(local, remoteL, "/", path.Join(base, "source"), fmt.Sprintf("soak-sftp-%d", cycles))
		}
		copyFile(remoteL, remoteR, path.Join(base, "source"), path.Join(base, "target"), fmt.Sprintf("soak-relay-%d", cycles))
		copyFile(remoteR, local, path.Join(base, "target"), "/download", fmt.Sprintf("soak-download-%d", cycles))
		f, e := root.Open(filepath.Join("download", name))
		if e != nil {
			t.Fatal(e)
		}
		hash := sha256.New()
		_, e = io.Copy(hash, f)
		f.Close()
		if e != nil || !bytes.Equal(hash.Sum(nil), expected[:]) {
			t.Fatal("mixed transfer SHA256 mismatch", e)
		}
		cycles++
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		g := runtime.NumGoroutine()
		if g > peakGoroutines {
			peakGoroutines = g
		}
		samples = append(samples, sample{time.Since(started).Seconds(), cycles, float64(memory.HeapAlloc) / (1 << 20), g})
		if cycles%12 == 0 {
			t.Logf("%d cycles / %d hash-verified copies, %.1fs", cycles, copies, time.Since(started).Seconds())
		}
		wait := 5*time.Second - time.Since(cycleAt)
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	for _, r := range []*runResult{left, right} {
		if !r.session.Write([]byte("printf 'mixed-soak-%s\\n' terminal-ok\r")) {
			t.Fatal("PTY unavailable after mixed load")
		}
		eventually(t, func() bool { return strings.Contains(r.text(), "mixed-soak-terminal-ok") })
	}
	report := map[string]any{"durationSeconds": time.Since(started).Seconds(), "cycles": cycles, "hashVerifiedCopies": copies, "fileBytes": len(data), "sha256": fmt.Sprintf("%x", expected), "peakGoroutines": peakGoroutines, "samples": samples, "scope": "real Go SSH adapters; two independent clients to the owned OpenSSH fixture; native process sampled separately"}
	raw, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(artifacts, "mixed-soak.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Logf("PASS %.1fs: %d verified copies; original PTYs alive", time.Since(started).Seconds(), copies)
}
