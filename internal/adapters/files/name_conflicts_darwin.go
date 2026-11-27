package files

import (
	"os"
	"path"

	"golang.org/x/sys/unix"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

func macVolumeNameConflicts(left FS, root string, names map[string]bool) map[string]bool {
	local, ok := left.(Local)
	if !ok {
		return nil
	}
	sensitive := func(parent string) bool {
		for dir := path.Join(root, parent); ; dir = path.Dir(dir) {
			f, err := local.Root.OpenFile(localPath(dir), os.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK, 0)
			if err == nil {
				const pcCaseSensitive = 11
				v, err := unix.Fpathconf(int(f.Fd()), pcCaseSensitive)
				f.Close()
				return err == nil && v == 1
			}
			if dir == "/" || dir == "." {
				return false
			}
		}
	}
	return macCollidingPaths(names, sensitive)
}

func macCollidingPaths(names map[string]bool, sensitive func(string) bool) map[string]bool {
	fold := cases.Fold()
	groups := make(map[string][]string)
	for name := range names {
		key := path.Join(path.Dir(name), norm.NFD.String(fold.String(path.Base(name))))
		groups[key] = append(groups[key], name)
	}
	bad := make(map[string]bool)
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		if !sensitive(path.Dir(group[0])) {
			for _, name := range group {
				bad[name] = true
			}
			continue
		}
		canonical := make(map[string]string)
		for _, name := range group {
			key := norm.NFD.String(name)
			if previous, ok := canonical[key]; ok {
				bad[previous], bad[name] = true, true
			}
			canonical[key] = name
		}
	}
	if len(bad) != 0 {
		for name := range names {
			for parent := path.Dir(name); parent != "." && parent != "/"; parent = path.Dir(parent) {
				if bad[parent] {
					bad[name] = true
					break
				}
			}
		}
	}
	return bad
}
