package sshconfig

import (
	"net/netip"
	"slices"
	"strings"
)

const unsupportedMatch = "包含尚不支持的 Match 条件，请使用独立 Host 配置。"

type matchRule struct {
	all, requestsFinal bool
	complete           bool
	tests              []matchTest
}

type matchTest struct {
	key      string
	negated  bool
	patterns []string
}

func parseMatch(fields []string) matchRule {
	if len(fields) == 1 && strings.EqualFold(fields[0], "all") {
		return matchRule{all: true}
	}
	var rule matchRule
	for len(fields) > 0 {
		attribute, argument, inline := strings.Cut(strings.TrimPrefix(fields[0], "!"), "=")
		key := strings.ToLower(attribute)
		test := matchTest{key: key, negated: strings.HasPrefix(fields[0], "!")}
		fields = fields[1:]
		switch key {
		case "all":
			if inline || len(fields) != 0 || len(rule.tests) != 0 && (len(rule.tests) != 1 || rule.tests[0].key != "canonical" && rule.tests[0].key != "final") {
				return matchRule{requestsFinal: rule.requestsFinal}
			}
		case "final", "canonical":
			if inline {
				return rule
			}
			rule.requestsFinal = rule.requestsFinal || key == "final" && !test.negated
		case "host", "originalhost", "user", "localuser", "exec", "localnetwork", "tagged", "command", "sessiontype", "version":
			if !inline {
				if len(fields) == 0 {
					return matchRule{requestsFinal: rule.requestsFinal}
				}
				argument, fields = fields[0], fields[1:]
			}
			if argument == "" && key != "tagged" && key != "command" {
				return matchRule{requestsFinal: rule.requestsFinal}
			}
			if key == "host" || key == "originalhost" || key == "user" || key == "localuser" {
				if key == "host" || key == "originalhost" {
					argument = asciiLower(argument)
				}
				test.patterns = strings.Split(argument, ",")
				for _, item := range test.patterns {
					if item == "" || item == "!" || len(item) >= 1024 {
						return matchRule{requestsFinal: rule.requestsFinal}
					}
				}
			}
		default:
			return rule
		}
		rule.tests = append(rule.tests, test)
	}
	rule.complete = len(rule.tests) > 0
	return rule
}

type applicability uint8

const (
	skipped applicability = iota
	conditional
	applied
)

type resolution struct {
	alias, localUser, finalHost string
	values                      map[string][]string
	final, wantFinal            bool
	aliasState                  applicability
	err                         string
}

func (s *resolution) matchHost() string {
	if s.final {
		return s.finalHost
	}
	if values := s.values["hostname"]; len(values) != 0 {
		if len(values) != 1 || strings.Contains(values[0], "${") {
			return ""
		}
		if !strings.Contains(values[0], "%") {
			return values[0]
		}
		if strings.Contains(strings.NewReplacer("%%", "", "%h", "").Replace(values[0]), "%") {
			return ""
		}
		return strings.NewReplacer("%%", "%", "%h", s.alias).Replace(values[0])
	}
	return s.alias
}

func (s *resolution) prepareFinal() {
	if host := asciiLower(s.matchHost()); host != "" && !s.mayFollowCNAME() {
		if addr, err := netip.ParseAddr(host); err == nil && addr.Is4() {
			s.finalHost = addr.String()
		} else if !strings.ContainsAny(host, ":%") && (host[0] < '0' || host[0] > '9' || strings.Trim(host, "0123456789abcdefx.") != "") {
			s.finalHost = host
		}
	}
	if len(s.values["hostname"]) == 0 {
		s.values["hostname"] = []string{s.alias}
	}
	s.final = true
}

func (s *resolution) mayFollowCNAME() bool {
	return canFollowCNAME(s.values["canonicalizepermittedcnames"])
}

func canFollowCNAME(values []string) bool {
	return len(values) > 0 && (len(values) != 1 || !strings.EqualFold(values[0], "none"))
}

func (r matchRule) applies(s *resolution) applicability {
	if r.all {
		return applied
	}
	if !r.complete {
		return conditional
	}
	result := applied
	for _, test := range r.tests {
		var name string
		switch test.key {
		case "all", "canonical", "final":
			if (test.key == "all" || s.final) == test.negated {
				return skipped
			}
			continue
		case "originalhost":
			name = asciiLower(s.alias)
		case "host":
			name = asciiLower(s.matchHost())
		case "localuser":
			name = s.localUser
		case "user":
			name = s.localUser
			if values := s.values["user"]; len(values) != 0 {
				name = values[0]
				if len(values) != 1 || strings.Contains(name, "%") || strings.Contains(name, "${") {
					name = ""
				}
			}
		}
		if name == "" {
			result = conditional
		} else if patternListMatches(test.patterns, name) == test.negated {
			return skipped
		}
	}
	return result
}

func (s *resolution) visit(lines []directive, scope applicability) {
	active := scope
	for _, d := range lines {
		switch d.key {
		case "host":
			if scope > s.aliasState && slices.ContainsFunc(d.values, func(value string) bool { return strings.EqualFold(value, s.alias) }) {
				s.aliasState = scope
			}
			name := s.alias
			if s.final {
				name = s.finalHost
			}
			active = scope
			if name == "" {
				active = min(scope, conditional)
			} else if !patternListMatches(d.values, name) {
				active = skipped
			}
		case "match":
			s.wantFinal = s.wantFinal || d.rule.requestsFinal
			active = min(scope, d.rule.applies(s))
		case "include":
			if d.dynamic {
				s.wantFinal = true
				if active == applied {
					s.err = "包含尚不支持导入的 Include 路径变量，请使用固定路径。"
				} else if active == conditional {
					s.err = unsupportedMatch
				}
			} else {
				s.visit(d.included, active)
			}
		default:
			if active == applied {
				if d.key == "identityfile" {
					s.values[d.key] = append(s.values[d.key], d.values...)
				} else if _, found := s.values[d.key]; !found {
					s.values[d.key] = d.values
				}
			} else if active == conditional && s.err == "" && s.canAffectImport(d) {
				s.err = unsupportedMatch
			}
		}
	}
}

func (s *resolution) canAffectImport(d directive) bool {
	switch d.key {
	case "identityfile":
		return slices.ContainsFunc(d.values, func(value string) bool {
			return !strings.EqualFold(value, "none") && !slices.Contains(s.values[d.key], value)
		})
	case "proxyjump":
		return len(s.values["proxyjump"]) == 0 && len(s.values["proxycommand"]) == 0
	case "proxycommand":
		jump := s.values["proxyjump"]
		return len(s.values["proxycommand"]) == 0 && (len(jump) == 0 || strings.EqualFold(jump[0], "none"))
	case "canonicalizepermittedcnames":
		return len(s.values[d.key]) == 0 && canFollowCNAME(d.values)
	default:
		return len(s.values[d.key]) == 0 && (known[d.key] || slices.Contains(importBlockers, d.key))
	}
}

func patternListMatches(patterns []string, name string) bool {
	positive := false
	for _, pattern := range patterns {
		negative := strings.HasPrefix(pattern, "!")
		if wildcardMatch(strings.TrimPrefix(pattern, "!"), name) {
			if negative {
				return false
			}
			positive = true
		}
	}
	return positive
}

func wildcardMatch(pattern, name string) bool {
	var afterStar, retry string
	for len(name) > 0 {
		switch {
		case strings.HasPrefix(pattern, "*"):
			pattern = pattern[1:]
			afterStar, retry = pattern, name
		case len(pattern) > 0 && (pattern[0] == '?' || pattern[0] == name[0]):
			pattern, name = pattern[1:], name[1:]
		case len(retry) > 0:
			retry = retry[1:]
			pattern, name = afterStar, retry
		default:
			return false
		}
	}
	return strings.Trim(pattern, "*") == ""
}

func asciiLower(value string) string {
	return strings.Map(func(char rune) rune {
		if char >= 'A' && char <= 'Z' {
			return char + 'a' - 'A'
		}
		return char
	}, value)
}
