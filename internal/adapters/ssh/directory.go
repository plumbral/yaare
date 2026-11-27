package sshclient

import (
	"bytes"
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

type DirectoryState struct {
	Path  string `json:"path"`
	Ready bool   `json:"ready"`
}

func validDirectory(value string) bool {
	if len(value) == 0 || len(value) > 4096 || value[0] != '/' || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

type directoryDecoder struct {
	token, host string
	state       int
	frame       []byte
	overflow    bool
	current     DirectoryState
	frameEpoch  uint64
}

func (d *directoryDecoder) feed(data []byte, epoch uint64) *DirectoryState {
	var last *DirectoryState
	finish := func() {
		if !d.overflow {
			if report := d.decode(string(d.frame)); report != nil {
				value := *report
				if d.frameEpoch != epoch {
					value.Ready = false
				}
				last = &value
			}
		}
		d.frame = d.frame[:0]
		d.state = 0
		d.overflow = false
	}
	for i := 0; i < len(data); i++ {
		if d.state == 0 {
			next := bytes.IndexByte(data[i:], 27)
			if next < 0 {
				break
			}
			i += next
			d.state = 1
			d.frameEpoch = epoch
			continue
		}
		b := data[i]
		switch d.state {
		case 0:
			if b == 27 {
				d.state = 1
				d.frameEpoch = epoch
			}
		case 1:
			if b == ']' {
				d.state = 2
			} else if b == 27 {
				d.frameEpoch = epoch
			} else if b != 27 {
				d.state = 0
			}
		case 2:
			if b == 7 {
				finish()
			} else if b == 27 {
				d.state = 3
			} else if len(d.frame) < 16384 {
				d.frame = append(d.frame, b)
			} else {
				d.overflow = true
			}
		case 3:
			if b == '\\' {
				finish()
			} else {
				d.frame = d.frame[:0]
				d.overflow = false
				if b == ']' {
					d.state = 2
					d.frameEpoch = epoch
				} else if b == 27 {
					d.state = 1
					d.frameEpoch = epoch
				} else {
					d.state = 0
				}
			}
		}
	}
	return last
}

func (d *directoryDecoder) decode(frame string) *DirectoryState {
	private := "777;yaare;" + d.token + ";"
	if d.token != "" && strings.HasPrefix(frame, private) {
		value, err := url.PathUnescape(strings.TrimPrefix(frame, private))
		if err == nil && validDirectory(value) {
			d.current = DirectoryState{Path: value, Ready: true}
			return &d.current
		}
	}
	if strings.HasPrefix(frame, "7;") {
		u, err := url.Parse(strings.TrimPrefix(frame, "7;"))
		if err == nil && u.Scheme == "file" && u.User == nil && u.Port() == "" && u.RawQuery == "" && u.Fragment == "" && (u.Hostname() == "" || strings.EqualFold(u.Hostname(), d.host)) && validDirectory(u.Path) {
			d.current = DirectoryState{Path: u.Path}
			return &d.current
		}
	}
	return nil
}

func (s *Session) scheduleDirectoryEmitLocked() {
	if s.directorySending || s.directoryClosed {
		return
	}
	s.directorySending = true
	done := make(chan struct{})
	s.directoryDone = done
	go s.drainDirectory(done)
}

func (s *Session) drainDirectory(done chan struct{}) {
	defer close(done)
	for {
		s.mu.Lock()
		if !s.connected || s.directoryClosed || s.ctx.Err() != nil || (s.directoryHasSent && s.directory == s.directorySent) {
			s.directorySending = false
			s.directoryDone = nil
			s.mu.Unlock()
			return
		}
		value := s.directory
		s.directorySent = value
		s.directoryHasSent = true
		s.mu.Unlock()
		if err := s.send(Event{Type: "directory", Directory: &value}); err != nil {
			s.mu.Lock()
			s.directorySending = false
			s.directoryDone = nil
			s.mu.Unlock()
			return
		}
	}
}

func (s *Session) stopDirectoryNotifications() {
	s.mu.Lock()
	s.directoryClosed = true
	done := s.directoryDone
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (s *Session) reportDirectory(value DirectoryState, epoch uint64) {
	s.mu.Lock()
	if epoch != s.inputEpoch {
		value.Ready = false
	}
	if !s.connected || s.directoryClosed || s.ctx.Err() != nil || s.directory == value {
		s.mu.Unlock()
		return
	}
	s.directory = value
	s.scheduleDirectoryEmitLocked()
	s.mu.Unlock()
}

func (s *Session) directoryBusy() {
	s.mu.Lock()
	s.inputEpoch++
	value := s.directory
	changed := value.Ready
	value.Ready = false
	s.directory = value
	if changed && s.connected && !s.directoryClosed && s.ctx.Err() == nil {
		s.scheduleDirectoryEmitLocked()
	}
	s.mu.Unlock()
}

func (s *Session) ChangeDirectory(value string) error {
	if !validDirectory(value) {
		return errors.New("目录路径无效或过长。")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	ready := s.connected && s.directory.Ready && s.stdin != nil && s.terminalTransfer == ""
	stdin := s.stdin
	s.mu.Unlock()
	if !ready || s.ctx.Err() != nil {
		return errors.New("终端正在运行命令、已有输入或尚未就绪，请回到空命令行后重试。")
	}
	s.directoryBusy()
	command := " cd -- '" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'\r"
	if _, err := stdin.Write([]byte(command)); err != nil {
		s.cancel()
		return errors.New("目录切换未能发送，请检查 SSH 连接。")
	}
	return nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func directoryIntegrationAllowed(serverVersion []byte) bool {
	version := strings.ToLower(string(serverVersion))
	return strings.Contains(version, "openssh") && !strings.Contains(version, "windows")
}

func directoryShellCommand(token string) string {
	inner := `__yaare_directory_report() {
  local result=$? value= char index LC_ALL=C
  for ((index=0; index<${#PWD}; index++)); do
    char=${PWD:index:1}
    case "$char" in [a-zA-Z0-9/._~-]) value+=$char;; *) printf -v char "%%%02X" "'$char"; value+=$char;; esac
  done
  printf "\033]777;yaare;TOKEN;%s\007" "$value"
  return "$result"
}
export -f __yaare_directory_report
if [[ -n ${PROMPT_COMMAND:-} ]]; then
  PROMPT_COMMAND+=$'\n__yaare_directory_report'
else
  PROMPT_COMMAND=__yaare_directory_report
fi
export PROMPT_COMMAND
exec "$BASH" -l`
	inner = strings.ReplaceAll(inner, "TOKEN", token)
	script := "case \"${SHELL:-/bin/sh}\" in\n*/bash) exec \"$SHELL\" -c " + shellQuote(inner) + ";;\n*) exec \"${SHELL:-/bin/sh}\" -l;;\nesac"
	return "/bin/sh -c " + shellQuote(script)
}
