package workspace

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxCommandBytes = 64 * 1024
const MaxCommandLibraryBytes = 4 * 1024 * 1024

func CommandIcon(icon string) string {
	if icon == "" {
		return "linux"
	}
	return icon
}

func normalizeCommand(c Command) Command {
	c.Name = strings.TrimSpace(c.Name)
	c.Group = strings.TrimSpace(c.Group)
	if c.Group == "" {
		c.Group = Ungrouped
	}
	c.Command = strings.ReplaceAll(c.Command, "\r\n", "\n")
	c.Icon = CommandIcon(c.Icon)
	return c
}

func ValidCommandText(text string) bool {
	return strings.TrimSpace(text) != "" && utf8.ValidString(text) && len(text) <= MaxCommandBytes &&
		!strings.ContainsAny(text, "\u2028\u2029") &&
		!strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' })
}

func validateCommandLibrary(s Snapshot) error {
	if len(s.CommandGroups) == 0 || len(s.CommandGroups) > 256 {
		return Fail("INVALID_COMMAND_GROUP", "命令分组数量无效。")
	}
	groups := map[string]bool{}
	for _, group := range s.CommandGroups {
		if !ValidGroup(group) || groups[group] {
			return Fail("INVALID_COMMAND_GROUP", "命令分组名称无效或重复。")
		}
		groups[group] = true
	}
	if !groups[Ungrouped] {
		return Fail("INVALID_COMMAND_GROUP", "缺少命令的未分组目录。")
	}
	ids := map[string]bool{}
	total := 0
	for _, c := range s.Commands {
		icon := CommandIcon(c.Icon)
		if !icons[icon] || strings.HasPrefix(icon, "cloud-") {
			return Fail("INVALID_COMMAND_ICON", "快捷命令只能使用操作系统图标。")
		}
		if !idPattern.MatchString(c.ID) || ids[c.ID] || !cleanText(c.Name, 40) || !groups[c.Group] || !ValidCommandText(c.Command) {
			return Fail("INVALID_COMMAND", "请检查命令名称、分组与内容；内容最多 64 KiB，允许换行和缩进，不允许其他控制字符。")
		}
		ids[c.ID] = true
		total += len(c.Command)
	}
	if total > MaxCommandLibraryBytes {
		return Fail("LIMIT_EXCEEDED", "快捷命令内容总量不能超过 4 MiB。")
	}
	return nil
}

func changeCommandLibrary(s *Snapshot, change Change) (string, bool, error) {
	indexOf := func(name string) int {
		for i, g := range s.CommandGroups {
			if g == name {
				return i
			}
		}
		return -1
	}
	switch change.Kind {
	case "saveCommand":
		c := normalizeCommand(change.Command)
		if c.ID == "" {
			c.ID = NewID()
		}
		for i, old := range s.Commands {
			if old.ID == c.ID {
				s.Commands[i] = c
				return c.ID, true, nil
			}
		}
		s.Commands = append(s.Commands, c)
		return c.ID, true, nil
	case "deleteCommand":
		for i, c := range s.Commands {
			if c.ID == change.ID {
				s.Commands = append(s.Commands[:i], s.Commands[i+1:]...)
				return "", true, nil
			}
		}
		return "", true, Fail("NOT_FOUND", "此命令已不存在。")
	case "addCommandGroup":
		name := strings.TrimSpace(change.Name)
		if indexOf(name) >= 0 {
			return "", true, Fail("DUPLICATE_GROUP", "此命令分组已存在。")
		}
		s.CommandGroups = append(s.CommandGroups, name)
		return "", true, nil
	case "renameCommandGroup", "deleteCommandGroup":
		if change.Name == Ungrouped {
			return "", true, Fail("PROTECTED_GROUP", "未分组不能重命名或删除。")
		}
		i := indexOf(change.Name)
		if i < 0 {
			return "", true, Fail("NOT_FOUND", "此命令分组已不存在。")
		}
		target := Ungrouped
		if change.Kind == "renameCommandGroup" {
			target = strings.TrimSpace(change.NewName)
			if target == change.Name {
				return "", true, nil
			}
			if indexOf(target) >= 0 {
				return "", true, Fail("DUPLICATE_GROUP", "此命令分组已存在。")
			}
			s.CommandGroups[i] = target
		} else {
			s.CommandGroups = append(s.CommandGroups[:i], s.CommandGroups[i+1:]...)
		}
		for i, c := range s.Commands {
			if c.Group == change.Name {
				s.Commands[i].Group = target
			}
		}
		return "", true, nil
	case "reorderCommandGroups":
		if len(change.CommandGroups) != len(s.CommandGroups) || len(change.CommandGroups) == 0 || change.CommandGroups[0] != Ungrouped {
			return "", true, Fail("INVALID_COMMAND_GROUP", "分组排序无效。")
		}
		seen := map[string]bool{}
		for _, g := range change.CommandGroups {
			if indexOf(g) < 0 || seen[g] {
				return "", true, Fail("INVALID_COMMAND_GROUP", "分组排序必须保留全部命令分组。")
			}
			seen[g] = true
		}
		s.CommandGroups = append([]string{}, change.CommandGroups...)
		return "", true, nil
	}
	return "", false, nil
}
