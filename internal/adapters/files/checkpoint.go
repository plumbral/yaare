package files

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
)

func EncodeCheckpoint(cp CopyCheckpoint) ([]byte, error) {
	if cp.File != nil {
		file := *cp.File
		cp.File = &file
		if file.Digest != nil {
			file.ExpectedDigest = file.Digest.Sum(nil)
		}
	}
	return json.Marshal(cp)
}
func DecodeCheckpoint(data []byte) (CopyCheckpoint, error) {
	var cp CopyCheckpoint
	if len(data) > 8*1024*1024 || json.Unmarshal(data, &cp) != nil || cp.Index < 0 || cp.Progress.Done < 0 || cp.SkippedBytes < 0 {
		return CopyCheckpoint{}, errors.New("续传记录无效，请重新传输。")
	}
	if cp.File != nil && (cp.File.Offset < 0 || len(cp.File.ExpectedDigest) != sha256.Size) {
		return CopyCheckpoint{}, errors.New("续传摘要无效，请重新传输。")
	}
	return cp, nil
}
