package configsync

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var ErrChanged = errors.New("远端配置已改变，请重新检查。")
var ErrMissing = errors.New("远端配置不存在。")
var ErrConditions = errors.New("此服务不支持安全的条件写入，不能开启同步。可使用备份与恢复。")

type Object struct {
	Name string
	Size int64
}
type Storage interface {
	Get(context.Context, string) ([]byte, string, error)
	GetCurrentIfChanged(context.Context, string) ([]byte, string, bool, error)
	Head(context.Context, string) (string, error)
	Put(context.Context, string, []byte, string) (string, error)
	List(context.Context) ([]Object, error)
	Delete(context.Context, string) error
	Close() error
}
type Config struct {
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Bucket   string `json:"bucket"`
	Region   string `json:"region"`
	Prefix   string `json:"prefix"`
}

const ObjectExtension = ".yaaresync"
const CurrentObject = "current" + ObjectExtension

var backupName = regexp.MustCompile(`^backups/[0-9]{8}T[0-9]{6}Z-[a-f0-9]{32}` + regexp.QuoteMeta(ObjectExtension) + `$`)
var probeName = regexp.MustCompile(`^probe-[a-f0-9]{32}$`)

func ValidBackupName(name string) bool { return backupName.MatchString(name) }
func ValidName(name string) bool {
	return name == CurrentObject || ValidBackupName(name) || probeName.MatchString(name)
}
func ValidPrefix(prefix string) bool {
	if len(prefix) > 512 || strings.ContainsAny(prefix, "\\\x00\r\n?#") {
		return false
	}
	for _, p := range strings.Split(prefix, "/") {
		if p == ".." || p == "." {
			return false
		}
	}
	return true
}
