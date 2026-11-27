package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"image/png"
	"net/url"
	"strings"
	"sync"
)

const CustomIconPrefix = "custom-png:"
const maxCustomIconBytes = 20 * 1024
const MaxWorkspaceIconBytes = 1024 * 1024

var (
	customIconCacheMu sync.RWMutex
	customIconCache   = map[[32]byte]struct{}{}
)

func validCustomIcon(icon string) bool {
	value, ok := strings.CutPrefix(icon, CustomIconPrefix)
	if !ok || len(value) > 600+base64.StdEncoding.EncodedLen(maxCustomIconBytes) {
		return false
	}
	sum := sha256.Sum256([]byte(icon))
	customIconCacheMu.RLock()
	_, cached := customIconCache[sum]
	customIconCacheMu.RUnlock()
	if cached {
		return true
	}
	label, data, ok := strings.Cut(value, ":")
	if !ok || strings.ContainsAny(data, "\r\n") {
		return false
	}
	name, err := url.PathUnescape(label)
	if err != nil || !cleanText(name, 40) {
		return false
	}
	b, err := base64.StdEncoding.Strict().DecodeString(data)
	if err != nil || len(b) == 0 || len(b) > maxCustomIconBytes {
		return false
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 64 || cfg.Height > 64 {
		return false
	}
	reader := bytes.NewReader(b)
	_, err = png.Decode(reader)
	if err != nil || reader.Len() != 0 {
		return false
	}
	customIconCacheMu.Lock()
	if len(customIconCache) >= 512 {
		clear(customIconCache)
	}
	customIconCache[sum] = struct{}{}
	customIconCacheMu.Unlock()
	return true
}
