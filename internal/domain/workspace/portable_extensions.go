package workspace

import (
	"encoding/json"
	"errors"
	"regexp"
)

type PortableExtensions map[string]json.RawMessage

var portableExtensionName = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

func ValidatePortableExtensions(value PortableExtensions) error {
	if len(value) > 16 {
		return errors.New("配置扩展数量超出限制。")
	}
	total := 0
	for name, raw := range value {
		total += len(name) + len(raw)
		if !portableExtensionName.MatchString(name) || len(raw) > 16<<10 || total > 64<<10 || !json.Valid(raw) {
			return errors.New("配置扩展内容无效。")
		}
	}
	return nil
}
