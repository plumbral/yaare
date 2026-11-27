package locales

import (
	_ "embed"
	"encoding/json"
)

//go:embed source/index.json
var uiSourceJSON []byte
var uiKeys = func() map[string]string {
	var keys map[string]string
	if err := json.Unmarshal(uiSourceJSON, &keys); err != nil {
		panic(err)
	}
	return keys
}()

func Text(language, source string) string {
	key, ok := uiKeys[source]
	if !ok {
		return source
	}
	return Catalog.Format(language, key, nil)
}
