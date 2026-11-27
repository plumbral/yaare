package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"golang.org/x/text/encoding/simplifiedchinese"
	"io"
	"path"
	filefs "yaare/internal/adapters/files"
	"testing"
	"time"
)

func TestRealOpenSSHLegacyEncodingEditAndLosslessFailure(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, closeClient, e := r.session.OpenFiles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	remote := filefs.Remote{Client: client}
	p := path.Join("/home/yaaretest/files", fmt.Sprintf("encoding-%d.txt", time.Now().UnixNano()))
	defer client.Remove(p)
	original, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("中文\r\n"))
	out, e := client.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	out.Write(original)
	out.Close()
	read, e := filefs.ReadTextEncoding(ctx, remote, p, "GBK")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = filefs.SaveTextEncoding(ctx, remote, p, read.Version, "中文🦂\n", "encoding-bad", "GBK"); e == nil {
		t.Fatal("lossy save accepted")
	}
	f, _ := client.Open(p)
	got, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(got, original) {
		t.Fatal("original changed")
	}
	saved, e := filefs.SaveTextEncoding(ctx, remote, p, read.Version, "中文\n修改\n", "encoding-good", "GBK")
	if e != nil || saved.Status != "saved" || saved.File.Encoding != "GBK" {
		t.Fatal("save", e)
	}
	f, _ = client.Open(p)
	got, _ = io.ReadAll(f)
	f.Close()
	want, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("中文\r\n修改\r\n"))
	if !bytes.Equal(got, want) {
		t.Fatal("wire encoding changed")
	}
	bom := true
	converted, e := filefs.SaveTextConverted(ctx, remote, p, saved.File.Version, saved.File.Content, "encoding-convert", "GBK", "UTF-16LE", &bom)
	if e != nil || converted.Status != "saved" || converted.File.Encoding != "UTF-16LE" || !converted.File.BOM {
		t.Fatal("real conversion", e)
	}
	f, _ = client.Open(p)
	got, _ = io.ReadAll(f)
	f.Close()
	if !bytes.HasPrefix(got, []byte{0xff, 0xfe}) {
		t.Fatal("UTF16 BOM missing")
	}
	if !r.session.Connected() {
		t.Fatal("PTY disrupted")
	}
}
