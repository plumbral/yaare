package sshclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type DirectoryState struct {
	Path       string `json:"path"`
	Ready      bool   `json:"ready"`
	Integrated bool   `json:"integrated,omitempty"`
}

type shellIntegrationAttempt struct {
	ack      chan struct{}
	deadline time.Time
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
	persistent  *persistentDirectoryReport
}

type persistentDirectoryReport struct {
	nonce, tag, path   string
	epoch              uint64
	checking, verified bool
}

func (d *directoryDecoder) persistentFrame(frame string) *DirectoryState {
	nonce, rest, ok := strings.Cut(frame, ";")
	if !ok || !bashNoncePattern.MatchString(nonce) {
		return nil
	}
	tag, raw, ok := strings.Cut(rest, ";")
	if !ok || !bashTokenPattern.MatchString(tag) {
		return nil
	}
	path, err := url.PathUnescape(raw)
	if err != nil || !validDirectory(path) {
		return nil
	}
	if d.persistent == nil {
		d.persistent = &persistentDirectoryReport{nonce: nonce, tag: tag}
	}
	p := d.persistent
	if p.nonce != nonce || p.tag != tag {
		return nil
	}
	if !p.verified {
		p.path, p.epoch = path, d.frameEpoch
		return nil
	}
	d.current = DirectoryState{Path: path, Ready: true, Integrated: true}
	return &d.current
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
	if raw, ok := strings.CutPrefix(frame, "777;yaare;bash;"); ok {
		return d.persistentFrame(raw)
	}
	private := "777;yaare;" + d.token + ";"
	if d.token != "" && strings.HasPrefix(frame, private) {
		value, err := url.PathUnescape(strings.TrimPrefix(frame, private))
		if err == nil && validDirectory(value) {
			if d.persistent != nil {
				d.persistent.path = ""
			}
			d.current = DirectoryState{Path: value, Ready: true, Integrated: true}
			return &d.current
		}
	}
	if strings.HasPrefix(frame, "7;") {
		u, err := url.Parse(strings.TrimPrefix(frame, "7;"))
		if err == nil && u.Scheme == "file" && u.User == nil && u.Port() == "" && u.RawQuery == "" && u.Fragment == "" && (u.Hostname() == "" || strings.EqualFold(u.Hostname(), d.host)) && validDirectory(u.Path) {
			if d.persistent != nil {
				d.persistent.path = ""
			}
			d.current = DirectoryState{Path: u.Path, Integrated: d.current.Integrated}
			return &d.current
		}
	}
	return nil
}

func (s *Session) verifyPersistentDirectory(p *persistentDirectoryReport) {
	tag, err := s.persistentDirectoryToken(s.ctx, p.nonce)
	if err != nil || tag != p.tag {
		return
	}
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	p.verified = true
	value := s.directoryDecoder.current
	value.Integrated = true
	if p.path != "" {
		value.Path, value.Ready = p.path, true
	}
	s.directoryDecoder.current = value
	if s.ctx.Err() == nil && s.flushLocked() == nil {
		s.reportDirectory(value, p.epoch)
	}
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
	value.Integrated = value.Integrated || s.directory.Integrated
	if epoch != s.inputEpoch {
		value.Ready = false
	}
	if !s.connected || s.directoryClosed || s.ctx.Err() != nil || s.directory == value {
		s.mu.Unlock()
		return
	}
	s.directory = value
	if value.Integrated && s.integration != nil {
		close(s.integration.ack)
		s.integration = nil
	}
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

func directoryHookCommand(token string) string {
	name := "__yaare_directory_" + token
	script := `if ! declare -F NAME >/dev/null; then ` +
		`NAME() { local result=$? value= char index LC_ALL=C; ` +
		`for ((index=0; index<${#PWD}; index++)); do char=${PWD:index:1}; ` +
		`case "$char" in [a-zA-Z0-9/._~-]) value+=$char;; *) printf -v char '%%%02X' "'$char"; value+=$char;; esac; done; ` +
		`printf '\033]777;yaare;TOKEN;%s\007' "$value"; return "$result"; }; ` +
		`if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) =~ ^declare\ -[^[:space:]]*a ]]; then PROMPT_COMMAND+=(NAME); ` +
		`else PROMPT_COMMAND+=$'\nNAME'; fi; fi`
	script = strings.NewReplacer("NAME", name, "TOKEN", token).Replace(script)
	return ` if [ -n "${BASH_VERSION-}" ]; then eval ` + shellQuote(script) + "; fi\r"
}

func (s *Session) EnableShellIntegration(ctx context.Context) error {
	s.writeMu.Lock()
	s.mu.Lock()
	if !s.connected || s.stdin == nil || s.terminalTransfer != "" || s.ctx.Err() != nil || ctx.Err() != nil || s.directoryDecoder.token == "" {
		s.mu.Unlock()
		s.writeMu.Unlock()
		return errors.New("SSH 会话已结束或不可用。")
	}
	if s.directory.Integrated {
		s.mu.Unlock()
		s.writeMu.Unlock()
		return nil
	}
	attempt := s.integration
	install := attempt == nil || !time.Now().Before(attempt.deadline)
	if install {
		attempt = &shellIntegrationAttempt{ack: make(chan struct{}), deadline: time.Now().Add(5 * time.Second)}
		s.integration = attempt
	}
	stdin, token := s.stdin, s.directoryDecoder.token
	s.mu.Unlock()
	if install {
		s.directoryBusy()
		command := directoryHookCommand(token)
		n, err := io.WriteString(stdin, command)
		if err != nil || n != len(command) {
			s.cancel()
			s.writeMu.Unlock()
			return errors.New("SSH 会话已结束或不可用。")
		}
	}
	s.writeMu.Unlock()
	timer := time.NewTimer(time.Until(attempt.deadline))
	defer timer.Stop()
	select {
	case <-attempt.ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return errors.New("SSH 会话已结束或不可用。")
	case <-timer.C:
		return errors.New("未收到 Bash 提示符确认，请在空命令行重试。")
	}
}
