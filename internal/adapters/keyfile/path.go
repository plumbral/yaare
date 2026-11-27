package keyfile

import (
	"os"
	"strings"
)

func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if !strings.EqualFold(a, b) {
		return false
	}
	first, err := os.Stat(a)
	if err != nil {
		return false
	}
	second, err := os.Stat(b)
	return err == nil && os.SameFile(first, second)
}
