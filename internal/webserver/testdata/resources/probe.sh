#!/bin/bash
set -eu -o pipefail
side=$1
state=$2
fail() { echo "$side $state port $port: $*" >&2; exit 1; }
read_hex() { od -An -tx1 -N "$1" <&3 | tr -d '[:space:]'; }
for port in 35121 35122 35123; do
    expected=closed
    if [[ $state == active ]]; then
        case "$side/$port" in
            web/35121|peer/35122|web/35123) expected=open ;;
        esac
    fi
    if ! { exec 3<>"/dev/tcp/127.0.0.1/$port"; } 2>/dev/null; then
        [[ $expected == closed ]] || fail "listener missing"
        continue
    fi
    [[ $expected == open ]] || fail "unexpected listener"
    if [[ $port == 35123 ]]; then
        printf '\x05\x01\x00' >&3
        [[ $(read_hex 2) == 0500 ]] || fail "SOCKS5 negotiation failed"
        printf '\x05\x01\x00\x01\x7f\x00\x00\x01\x00\x16' >&3
        reply=$(read_hex 10)
        [[ ${#reply} == 20 && $reply == 05000001* ]] || fail "SOCKS5 connect failed"
    fi
    IFS= read -r -t 4 banner <&3 || fail "missing SSH banner"
    [[ $banner == SSH-2.0-OpenSSH* ]] || fail "wrong destination"
    exec 3>&-
done
