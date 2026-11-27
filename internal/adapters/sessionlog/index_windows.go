package sessionlog

import "yaare/internal/adapters/storage/durable"

func replaceIndex(temp, path string) error {
	return durable.Rename(temp, path)
}
