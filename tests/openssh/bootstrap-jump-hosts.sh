#!/bin/sh
set -eu
root=/opt/yaare-jump-hosts
mkdir -p "$root" /run/sshd
for port in 22223 22224; do
  key="$root/host_$port"
  if [ ! -f "$key" ]; then ssh-keygen -q -t ed25519 -N '' -f "$key"; fi
  cat > "$root/sshd_$port.conf" <<EOF
Port $port
ListenAddress 127.0.0.1
HostKey $key
PidFile $root/sshd_$port.pid
PasswordAuthentication yes
PubkeyAuthentication yes
KbdInteractiveAuthentication yes
UsePAM yes
PermitRootLogin no
AllowUsers yaaretest
AllowTcpForwarding yes
GatewayPorts no
Subsystem sftp internal-sftp
PrintMotd no
EOF
  /usr/sbin/sshd -t -f "$root/sshd_$port.conf"
  if [ -f "$root/sshd_$port.pid" ] && kill -0 "$(cat "$root/sshd_$port.pid")" 2>/dev/null; then
    kill -HUP "$(cat "$root/sshd_$port.pid")"
  else
    /usr/sbin/sshd -f "$root/sshd_$port.conf"
  fi
done
exit 0
