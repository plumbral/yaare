package sshclient

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

const bashKeyFile = ".yaare-bash-key"
const bashStartMarker = ": YAARE_BASH_INTEGRATION_BEGIN_V1"
const bashEndMarker = ": YAARE_BASH_INTEGRATION_END_V1"

const readBashKey = `[ -n "${SSH_CONNECTION-}" ] &&
  [ -f "$HOME/.yaare-bash-key" ] && [ ! -L "$HOME/.yaare-bash-key" ] &&
  [ "$(command stat -c '%a:%u' -- "$HOME/.yaare-bash-key")" = "600:$(command id -u)" ] &&
  IFS=' ' read -r marker profile created secret < "$HOME/.yaare-bash-key" &&
  [ "$marker" = yaare-bash-v1 ] && [ "${#secret}" = 64 ]`

const bashConnectionLifetime = `__yaare_bash_lifetime_v1() (
  set -f
  IFS=' '
  p=$$
  depth=0
  while [ "$p" -gt 1 ] && [ "$depth" -lt 24 ]; do
    IFS= read -r row < "/proc/$p/stat" || exit 1
    set -- ${row##*\) }
    case "$row" in "$p (sshd) "*|"$p (sshd-session) "*)
      IFS= read -r boot < /proc/sys/kernel/random/boot_id || exit 1
      printf '%s:%s:%s' "$boot" "$p" "${20}"
      exit 0;;
    esac
    p=$2
    depth=$((depth+1))
  done
  exit 1
)`

const hashBashToken = `printf '%s\n%s\n%s\n%s\n' "$secret" "$SSH_CONNECTION" "$lifetime" "$nonce" | command sha256sum`

var bashNoncePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var bashTokenPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var errBashIntegration = errors.New("无法安全修改 Bash 启动配置。请检查远程文件权限、类型及 Yaare 管理的内容。")
var errBashChanged = errors.New("Bash 集成配置已变化，请重新预览。")
var errBashRequirements = errors.New("自动加载需要 Linux OpenSSH、Bash、SFTP、flock、sha256sum 和 GNU stat。")
var errBashLock = errors.New("无法锁定远程账户的主目录，请稍后重试。")

func bashProfileBlock() string {
	setup := bashConnectionLifetime + `
__yaare_bash_setup_v1() {
  local marker profile created secret nonce digest lifetime
  ` + readBashKey + ` || return
  [[ $secret =~ ^[0-9a-f]{64}$ ]] || return
  lifetime=$(__yaare_bash_lifetime_v1) || return
  nonce=$(command od -An -N16 -tx1 /dev/urandom | command tr -d ' \n') || return
  [[ $nonce =~ ^[0-9a-f]{32}$ ]] || return
  digest=$(` + hashBashToken + `) || return
  __yaare_bash_tag_v1=${digest%% *}
  [[ $__yaare_bash_tag_v1 =~ ^[0-9a-f]{64}$ ]] || return
  __yaare_bash_nonce_v1=$nonce
  if (( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1) )); then
    PROMPT_COMMAND+=(__yaare_bash_report_v1)
  else
    PROMPT_COMMAND=${PROMPT_COMMAND:+$PROMPT_COMMAND$'\n'}__yaare_bash_report_v1
  fi
}
__yaare_bash_report_v1() {
  local result=$? value= char index LC_ALL=C
  for ((index=0; index<${#PWD}; index++)); do
    char=${PWD:index:1}
    case "$char" in [a-zA-Z0-9/._~-]) value+=$char;; *) printf -v char '%%%02X' "'$char"; value+=$char;; esac
  done
  printf '\033]777;yaare;bash;%s;%s;%s\007' "$__yaare_bash_nonce_v1" "$__yaare_bash_tag_v1" "$value"
  return "$result"
}
__yaare_bash_setup_v1 || :
unset -f __yaare_bash_setup_v1 __yaare_bash_lifetime_v1`
	return "\n" + bashStartMarker + "\n" +
		"if [ -n \"${BASH_VERSION-}\" ] && [ -n \"${SSH_CONNECTION-}\" ] && [ -z \"${__yaare_bash_nonce_v1-}\" ]; then\n" +
		"  case $- in *x*) ;; *i*) eval \"$(command cat <<'YAARE_BASH_V1'\n" + setup + "\nYAARE_BASH_V1\n)\" ;; esac\nfi\n" +
		bashEndMarker + "\n"
}

func (s *Session) bashCommandOutput(ctx context.Context, script string, limit int64) (string, error) {
	command, err := s.OpenCommand(ctx, "/bin/sh -c "+shellQuote(script))
	if err != nil {
		return "", err
	}
	defer command.Close()
	_ = command.Input.Close()
	data, err := io.ReadAll(io.LimitReader(command.Output, limit+1))
	if err != nil || int64(len(data)) > limit {
		return "", errBashRequirements
	}
	if err = command.Wait(); err != nil {
		return "", errBashRequirements
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

func (s *Session) persistentDirectoryToken(ctx context.Context, nonce string) (string, error) {
	if !bashNoncePattern.MatchString(nonce) {
		return "", errBashIntegration
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	script := "set +x\n" + bashConnectionLifetime + "\nlifetime=$(__yaare_bash_lifetime_v1) || exit 1\nnonce=" + nonce + "; " + readBashKey + " || exit 1\n" +
		"digest=$(" + hashBashToken + ") || exit 1\nprintf '%s' \"${digest%% *}\""
	tag, err := s.bashCommandOutput(ctx, script, 64)
	if err != nil || !bashTokenPattern.MatchString(tag) {
		return "", errBashIntegration
	}
	return tag, nil
}
