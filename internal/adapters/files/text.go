package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/pkg/sftp"
)

const MaxTextBytes = 2 << 20

type TextProblem struct{ Code, Message string }

func (p *TextProblem) Error() string         { return p.Code + ": " + p.Message }
func textProblem(code, message string) error { return &TextProblem{code, message} }

type TextFile struct {
	Encoding    string `json:"encoding"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	Version     string `json:"version"`
	LineEnding  string `json:"lineEnding"`
	BOM         bool   `json:"bom"`
	Size        int64  `json:"size"`
	Modified    int64  `json:"modified"`
	Permissions string `json:"permissions"`
	info        fs.FileInfo
}

type TextSaveResult struct {
	Status string   `json:"status"`
	File   TextFile `json:"file"`
}

func textMetadata(info fs.FileInfo) string {
	value := fmt.Sprintf("%d:%d:%d", info.Size(), info.ModTime().UnixNano(), info.Mode())
	if stat, ok := info.Sys().(*sftp.FileStat); ok {
		value += fmt.Sprintf(":%d:%d", stat.UID, stat.GID)
	}
	return value
}

func checkText(content []byte) error {
	if len(content) > MaxTextBytes {
		return textProblem("TEXT_TOO_LARGE", "在线编辑仅支持不超过 2 MiB 的文本文件，请下载后编辑。")
	}
	if !utf8.Valid(content) {
		return textProblem("TEXT_ENCODING", "此文件不是有效的 UTF-8 文本，请下载后使用对应编码编辑。")
	}
	for _, b := range content {
		if b < 32 && b != '\t' && b != '\r' && b != '\n' || b == 127 {
			return textProblem("TEXT_BINARY", "此文件包含二进制或控制数据，无法作为文本在线编辑。")
		}
	}
	return nil
}

func ReadText(ctx context.Context, f FS, p string) (TextFile, error) {
	return ReadTextEncoding(ctx, f, p, "auto")
}
func ReadTextEncoding(ctx context.Context, f FS, p, encoding string) (TextFile, error) {
	if err := ctx.Err(); err != nil {
		return TextFile{}, err
	}
	before, err := f.Lstat(p)
	if err != nil {
		return TextFile{}, err
	}
	if !before.Mode().IsRegular() {
		return TextFile{}, textProblem("TEXT_FILE_TYPE", "请选择普通文本文件；在线编辑不跟随符号链接。")
	}
	if before.Size() > MaxTextBytes {
		return TextFile{}, textProblem("TEXT_TOO_LARGE", "在线编辑仅支持不超过 2 MiB 的文本文件，请下载后编辑。")
	}
	input, err := f.Open(p)
	if err != nil {
		return TextFile{}, err
	}
	data, err := io.ReadAll(io.LimitReader(input, MaxTextBytes+1))
	closeErr := input.Close()
	if err != nil {
		return TextFile{}, err
	}
	if closeErr != nil {
		return TextFile{}, closeErr
	}
	if err = ctx.Err(); err != nil {
		return TextFile{}, err
	}
	source, codec, bom, decodeErr := decodeText(data, encoding)
	if decodeErr != nil {
		return TextFile{}, decodeErr
	}
	after, err := f.Lstat(p)
	if err != nil {
		return TextFile{}, err
	}
	if !after.Mode().IsRegular() || textMetadata(before) != textMetadata(after) || int64(len(data)) != after.Size() {
		return TextFile{}, textProblem("TEXT_CHANGED_DURING_READ", "文件在读取时发生变化，请重新打开。")
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(textMetadata(after)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(data)
	crlf := bytes.Count(source, []byte("\r\n"))
	lf := bytes.Count(source, []byte("\n"))
	cr := bytes.Count(source, []byte("\r"))
	if cr != crlf || crlf > 0 && lf != crlf {
		return TextFile{}, textProblem("TEXT_LINE_ENDING", "此文件含混合或旧式换行符，请下载后编辑，以免改变原有格式。")
	}
	ending := "LF"
	if crlf > 0 {
		ending = "CRLF"
	}
	return TextFile{Encoding: codec, Path: p, Content: strings.ReplaceAll(string(source), "\r\n", "\n"), Version: hex.EncodeToString(hash.Sum(nil)), LineEnding: ending, BOM: bom, Size: after.Size(), Modified: after.ModTime().UnixMilli(), Permissions: fmt.Sprintf("%03o", after.Mode().Perm()), info: after}, nil
}

func SaveText(ctx context.Context, f FS, p, expected, content, nonce string) (TextSaveResult, error) {
	return SaveTextEncoding(ctx, f, p, expected, content, nonce, "auto")
}
func SaveTextEncoding(ctx context.Context, f FS, p, expected, content, nonce, encoding string) (TextSaveResult, error) {
	return SaveTextConverted(ctx, f, p, expected, content, nonce, encoding, "", nil)
}
func SaveTextConverted(ctx context.Context, f FS, p, expected, content, nonce, encoding, targetEncoding string, targetBOM *bool) (TextSaveResult, error) {
	return saveTextConverted(ctx, f, p, expected, content, nonce, encoding, targetEncoding, targetBOM, "")
}
func saveTextConverted(ctx context.Context, f FS, p, expected, content, nonce, encoding, targetEncoding string, targetBOM *bool, targetLineEnding string) (TextSaveResult, error) {
	if len(expected) != 64 {
		return TextSaveResult{}, textProblem("TEXT_VERSION", "文件版本无效，请重新打开。")
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return TextSaveResult{}, textProblem("TEXT_VERSION", "文件版本无效，请重新打开。")
	}
	if !Leaf(nonce) {
		return TextSaveResult{}, textProblem("TEXT_INVALID", "无法创建临时文件。")
	}
	if strings.ContainsRune(content, '\r') {
		return TextSaveResult{}, textProblem("TEXT_INVALID", "编辑内容含不支持的换行符。")
	}
	if err := checkText([]byte(content)); err != nil {
		return TextSaveResult{}, err
	}
	current, err := ReadTextEncoding(ctx, f, p, encoding)
	if err != nil {
		return TextSaveResult{}, err
	}
	if current.Version != expected {
		return TextSaveResult{Status: "conflict", File: current}, nil
	}
	codec, bom := current.Encoding, current.BOM
	if targetEncoding != "" {
		codec = targetEncoding
		if codec != "UTF-8" {
			if _, ok := textCodec(codec); !ok {
				return TextSaveResult{}, textProblem("TEXT_ENCODING", "目标编码不受支持。")
			}
		}
		bom = false
	}
	if targetBOM != nil {
		bom = *targetBOM
	}
	lineEnding := current.LineEnding
	if targetLineEnding != "" {
		lineEnding = targetLineEnding
	}
	if current.Content == content && codec == current.Encoding && bom == current.BOM && lineEnding == current.LineEnding {
		return TextSaveResult{Status: "saved", File: current}, nil
	}
	if current.info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
		return TextSaveResult{}, textProblem("TEXT_PERMISSIONS", "此文件带特殊权限，请使用终端编辑；原文件已保留。")
	}
	if remote, ok := f.(Remote); ok {
		if _, ok = remote.Client.HasExtension("posix-rename@openssh.com"); !ok {
			return TextSaveResult{}, textProblem("TEXT_ATOMIC_UNSUPPORTED", "服务器不支持安全替换文件，原文件已保留，请下载后编辑。")
		}
	}
	encoded, err := encodeText(content, codec, bom, lineEnding)
	if err != nil {
		return TextSaveResult{}, err
	}
	temporary := path.Join(path.Dir(p), ".yaare-edit-"+nonce+".part")
	output, err := f.Create(temporary)
	if err != nil {
		return TextSaveResult{}, err
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			_ = f.Remove(temporary, false)
		}
	}()
	if err = f.Chmod(temporary, 0600); err != nil {
		return TextSaveResult{}, err
	}
	if _, err = io.Copy(output, bytes.NewReader(encoded)); err != nil {
		return TextSaveResult{}, err
	}
	if remote, ok := f.(Remote); ok {
		if _, supported := remote.Client.HasExtension("fsync@openssh.com"); supported {
			if err = output.(interface{ Sync() error }).Sync(); err != nil {
				return TextSaveResult{}, err
			}
		}
	} else if syncer, ok := output.(interface{ Sync() error }); ok {
		if err = syncer.Sync(); err != nil {
			return TextSaveResult{}, err
		}
	}
	if err = output.Close(); err != nil {
		return TextSaveResult{}, err
	}
	if remote, ok := f.(Remote); ok {
		if stat, ok := current.info.Sys().(*sftp.FileStat); ok {
			info, e := remote.Lstat(temporary)
			if e != nil {
				return TextSaveResult{}, e
			}
			if other, ok := info.Sys().(*sftp.FileStat); ok && (stat.UID != other.UID || stat.GID != other.GID) {
				if err = remote.Client.Chown(temporary, int(stat.UID), int(stat.GID)); err != nil {
					return TextSaveResult{}, textProblem("TEXT_PERMISSIONS", "无法保留原文件的所有者或用户组，原文件已保留。")
				}
			}
		}
	}
	if err = f.Chmod(temporary, current.info.Mode().Perm()); err != nil {
		return TextSaveResult{}, err
	}
	latest, err := ReadTextEncoding(ctx, f, p, encoding)
	if err != nil {
		return TextSaveResult{}, err
	}
	if latest.Version != expected {
		return TextSaveResult{Status: "conflict", File: latest}, nil
	}
	if err = ctx.Err(); err != nil {
		return TextSaveResult{}, err
	}
	if err = f.Rename(temporary, p, true); err != nil {
		return TextSaveResult{}, err
	}
	committed = true
	saved, err := ReadTextEncoding(ctx, f, p, codec)
	if err != nil {
		return TextSaveResult{}, textProblem("TEXT_SAVE_UNCONFIRMED", "服务器已接受替换，但未能确认最新内容。请保留编辑内容，重新读取文件后再决定是否保存。")
	}
	if saved.Content != content {
		return TextSaveResult{Status: "conflict", File: saved}, nil
	}
	return TextSaveResult{Status: "saved", File: saved}, nil
}
