package files

import (
	"bytes"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"strings"
	"unicode/utf8"
)

func textCodec(name string) (encoding.Encoding, bool) {
	switch name {
	case "GBK":
		return simplifiedchinese.GBK, true
	case "GB18030":
		return simplifiedchinese.GB18030, true
	case "UTF-16LE":
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), true
	case "UTF-16BE":
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM), true
	}
	return nil, false
}
func decodeText(data []byte, requested string) ([]byte, string, bool, error) {
	if len(data) > MaxTextBytes {
		return nil, "", false, textProblem("TEXT_TOO_LARGE", "在线编辑仅支持不超过 2 MiB 的文本文件。")
	}
	name, bom, source := requested, false, data
	detected, prefix := "", 0
	switch {
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		detected, prefix = "UTF-8", 3
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		detected, prefix = "UTF-16LE", 2
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		detected, prefix = "UTF-16BE", 2
	}
	if name == "" || name == "auto" {
		name = "UTF-8"
		if detected != "" {
			name = detected
		}
	}
	if detected != "" {
		if detected != name {
			return nil, "", false, textProblem("TEXT_ENCODING", "所选编码与文件 BOM 不一致，请选择自动识别或对应编码。")
		}
		bom = true
		source = data[prefix:]
	}
	decoded := source
	if name == "UTF-8" {
		if !utf8.Valid(source) {
			return nil, "", false, textProblem("TEXT_ENCODING", "此文件不是有效的 UTF-8，请选择原始编码后重新读取。")
		}
	} else {
		codec, ok := textCodec(name)
		if !ok {
			return nil, "", false, textProblem("TEXT_ENCODING", "请选择支持的文本编码。")
		}
		var err error
		decoded, err = codec.NewDecoder().Bytes(source)
		if err != nil {
			return nil, "", false, textProblem("TEXT_ENCODING", "此文件无法按所选编码完整解码。")
		}
		roundtrip, e := codec.NewEncoder().Bytes(decoded)
		if e != nil || !bytes.Equal(roundtrip, source) {
			return nil, "", false, textProblem("TEXT_ENCODING", "所选编码无法无损读取此文件，请选择原始编码。")
		}
	}
	if err := checkText(decoded); err != nil {
		return nil, "", false, err
	}
	return decoded, name, bom, nil
}
func encodeText(content, name string, bom bool, ending string) ([]byte, error) {
	if ending == "CRLF" {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	data := []byte(content)
	if name != "UTF-8" {
		codec, ok := textCodec(name)
		if !ok {
			return nil, textProblem("TEXT_ENCODING", "文件编码无效，请重新读取。")
		}
		var err error
		data, err = codec.NewEncoder().Bytes(data)
		if err != nil {
			return nil, textProblem("TEXT_UNREPRESENTABLE", "当前编码不能表示部分字符，原文件已保留。请移除这些字符或下载后转换编码。")
		}
		decoded, e := codec.NewDecoder().Bytes(data)
		if e != nil || string(decoded) != content {
			return nil, textProblem("TEXT_UNREPRESENTABLE", "无法无损保存这些字符，原文件已保留。")
		}
	}
	if bom {
		switch name {
		case "UTF-8":
			data = append([]byte{0xef, 0xbb, 0xbf}, data...)
		case "UTF-16LE":
			data = append([]byte{0xff, 0xfe}, data...)
		case "UTF-16BE":
			data = append([]byte{0xfe, 0xff}, data...)
		default:
			return nil, textProblem("TEXT_ENCODING", "此编码不支持 BOM。")
		}
	}
	if len(data) > MaxTextBytes {
		return nil, textProblem("TEXT_TOO_LARGE", "编码后的文件超过 2 MiB，请减少内容。")
	}
	return data, nil
}
