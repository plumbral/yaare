package server

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"yaare/internal/domain/workspace"
)

const systemProbe = `/bin/sh -c 'printf "%s\n" YAARE_OS_V1
system=$(uname -s) || exit 1
printf "%s\n" "$system"
if [ "$system" = Linux ]; then
  if [ -r /etc/os-release ]; then cat /etc/os-release
  elif [ -r /usr/lib/os-release ]; then cat /usr/lib/os-release
  fi
else
  uname -r
fi'`

func DetectSystem(ctx context.Context, open Opener) (workspace.ServerSystem, error) {
	raw, err := Run(ctx, open, systemProbe, 8192)
	if err != nil {
		return workspace.ServerSystem{}, err
	}
	return parseSystem(string(raw))
}

func parseSystem(raw string) (workspace.ServerSystem, error) {
	const marker = "YAARE_OS_V1\n"
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, marker) {
		return workspace.ServerSystem{}, errors.New("unrecognized remote system reply")
	}
	kernel, body, _ := strings.Cut(strings.TrimPrefix(raw, marker), "\n")
	if kernel == "Linux" {
		values := map[string]string{}
		for _, line := range strings.Split(body, "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok && (key == "ID" || key == "VERSION_ID" || key == "NAME") {
				values[key] = osReleaseValue(value)
			}
		}
		id := strings.ToLower(values["ID"])
		if !workspace.ValidSystemPart(id) {
			return workspace.ServerSystem{Family: "linux", Label: "Linux"}, nil
		}
		version := values["VERSION_ID"]
		if !workspace.ValidSystemPart(version) {
			version = ""
		}
		name := values["NAME"]
		if name == "" {
			name = id
		}
		return workspace.ServerSystem{Family: "linux", ID: id, Version: version, Label: systemLabel(name, version)}, nil
	}
	name := kernel
	switch kernel {
	case "FreeBSD", "OpenBSD", "NetBSD", "SunOS":
	case "Darwin":
		name = "macOS"
	default:
		return workspace.ServerSystem{}, errors.New("unrecognized remote system")
	}
	version := strings.TrimSpace(body)
	if !workspace.ValidSystemPart(version) {
		version = ""
	}
	return workspace.ServerSystem{Family: strings.ToLower(kernel), ID: strings.ToLower(kernel), Version: version, Label: systemLabel(name, version)}, nil
}

func osReleaseValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 || (value[0] != '"' && value[0] != '\'') || value[len(value)-1] != value[0] {
		return value
	}
	inner := value[1 : len(value)-1]
	if value[0] == '"' {
		return strings.NewReplacer(`\"`, `"`, `\\\\`, `\`, `\$`, "$", "\\`", "`").Replace(inner)
	}
	return inner
}

func systemLabel(name, version string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, name)
	label := strings.TrimSpace(name + " " + version)
	if runes := []rune(label); len(runes) > 64 {
		label = strings.TrimSpace(string(runes[:64]))
	}
	return label
}
