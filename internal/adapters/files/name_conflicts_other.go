//go:build !darwin

package files

func macVolumeNameConflicts(FS, string, map[string]bool) map[string]bool { return nil }
