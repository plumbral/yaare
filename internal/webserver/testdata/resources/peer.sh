#!/bin/sh
set -eu
user=yaaretest
client=/home/$user/client
socket=/run/yaare-agent/socket
useradd -m -u 10001 -s /bin/sh "$user"
passwd -d "$user" >/dev/null
install -d -m 0755 /run/sshd
install -d -m 0700 -o "$user" -g "$user" /run/yaare-agent
install -d -m 0700 /run/yaare-ssh
ssh-keygen -q -t ed25519 -N '' -f /run/yaare-ssh/host
ssh-keygen -q -t ed25519 -N '' -f /run/yaare-ssh/ca
runuser -u "$user" -- ssh-keygen -q -t ed25519 -N '' -f "$client"
runuser -u "$user" -- ssh-agent -a "$socket" >/dev/null
runuser -u "$user" -- env SSH_AUTH_SOCK="$socket" ssh-add "$client"
ssh-keygen -q -s /run/yaare-ssh/ca -I yaare-fixture -n "$user" -V -1m:+1h "$client.pub"

start_sshd() {
    port=$1
    shift
    /usr/sbin/sshd -f /dev/null -p "$port" -h /run/yaare-ssh/host \
        -o "PidFile=/run/yaare-ssh/sshd-$port.pid" -o "AllowUsers=$user" \
        -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no "$@"
}
start_sshd 22 -o "AuthorizedKeysFile=$client.pub"
start_sshd 2223 -o AuthorizedKeysFile=none -o TrustedUserCAKeys=/run/yaare-ssh/ca.pub
