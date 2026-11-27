package files

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTextEncodingsRoundTripBOMAndConflict(t *testing.T) {
	for _, codec := range []string{"UTF-8", "UTF-16LE", "UTF-16BE", "GB18030", "GBK"} {
		for _, bom := range []bool{false, true} {
			if bom && (codec == "GB18030" || codec == "GBK") {
				continue
			}
			t.Run(codec+map[bool]string{true: "-bom", false: ""}[bom], func(t *testing.T) {
				f, root := sandbox(t)
				original, e := encodeText("中文\nhello\n", codec, bom, "CRLF")
				if e != nil {
					t.Fatal(e)
				}
				target := filepath.Join(root, "note.txt")
				os.WriteFile(target, original, 0600)
				requested := codec
				if bom {
					requested = "auto"
				}
				read, e := ReadTextEncoding(context.Background(), f, "/note.txt", requested)
				if e != nil || read.Content != "中文\nhello\n" || read.Encoding != codec || read.BOM != bom || read.LineEnding != "CRLF" {
					t.Fatal("decode", read.Encoding, e)
				}
				saved, e := SaveTextEncoding(context.Background(), f, "/note.txt", read.Version, read.Content+"修改\n", "encoding", codec)
				if e != nil || saved.Status != "saved" {
					t.Fatal("save", e)
				}
				got, _ := os.ReadFile(target)
				want, _ := encodeText(read.Content+"修改\n", codec, bom, "CRLF")
				if !bytes.Equal(got, want) {
					t.Fatal("changed encoding/newline")
				}
				conflict, e := SaveTextEncoding(context.Background(), f, "/note.txt", read.Version, "old draft", "conflict", codec)
				if e != nil || conflict.Status != "conflict" {
					t.Fatal("encoding bypassed conflict", e)
				}
			})
		}
	}
}
func TestTextEncodingNeverSilentlyReplacesCharacters(t *testing.T) {
	f, root := sandbox(t)
	source, _ := encodeText("中文", "GBK", false, "LF")
	target := filepath.Join(root, "note.txt")
	os.WriteFile(target, source, 0600)
	file, e := ReadTextEncoding(context.Background(), f, "/note.txt", "GBK")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = SaveTextEncoding(context.Background(), f, "/note.txt", file.Version, "中文🦂", "unrepresentable", "GBK"); e == nil {
		t.Fatal("lossy save accepted")
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, source) {
		t.Fatal("lossy failure changed original")
	}
	for _, c := range []struct {
		data  []byte
		codec string
	}{{[]byte{0x81}, "GBK"}, {[]byte{0x81, 0x30, 0x81}, "GB18030"}, {[]byte{0, 0xd8}, "UTF-16LE"}, {[]byte{0}, "UTF-16BE"}, {[]byte{0xff, 0xfe, 0x61, 0}, "UTF-8"}, {[]byte{0}, "UTF-8"}} {
		if _, _, _, e := decodeText(c.data, c.codec); e == nil {
			t.Fatal("invalid bytes accepted", c.codec)
		}
	}
	encoded, e := encodeText("中文🦂", "GB18030", false, "LF")
	if e != nil {
		t.Fatal(e)
	}
	decoded, _, _, e := decodeText(encoded, "GB18030")
	if e != nil || string(decoded) != "中文🦂" {
		t.Fatal("gb18030 supplementary character", e)
	}
}
func TestExplicitTextEncodingConversionPreservesVersionAndNewlines(t *testing.T) {
	f, root := sandbox(t)
	original, _ := encodeText("中文\n", "GBK", false, "CRLF")
	target := filepath.Join(root, "convert.txt")
	os.WriteFile(target, original, 0600)
	file, e := ReadTextEncoding(context.Background(), f, "/convert.txt", "GBK")
	if e != nil {
		t.Fatal(e)
	}
	bom := true
	converted, e := SaveTextConverted(context.Background(), f, "/convert.txt", file.Version, file.Content, "convert", "GBK", "UTF-8", &bom)
	if e != nil || converted.Status != "saved" || converted.File.Encoding != "UTF-8" || !converted.File.BOM {
		t.Fatal("conversion", e)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "\ufeff中文\r\n" {
		t.Fatal("bytes or newline")
	}
	if _, e = SaveTextConverted(context.Background(), f, "/convert.txt", file.Version, "stale", "stale", "UTF-8", "GBK", nil); e != nil {
		t.Fatal("expected content conflict", e)
	}
	got, _ = os.ReadFile(target)
	if string(got) != "\ufeff中文\r\n" {
		t.Fatal("stale conversion overwrote")
	}
}
