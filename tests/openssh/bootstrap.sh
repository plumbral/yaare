#!/bin/bash
set -eu
sh /tmp/yaare-install-packages.sh openssh-server bash vim nano tmux htop procps ncurses-term ca-certificates lrzsz
id yaaretest >/dev/null 2>&1 || useradd -m -s /bin/bash yaaretest
echo 'yaaretest:Yaare-OpenSSH-Test-Only!' | chpasswd
id yaarenteractive >/dev/null 2>&1 || useradd -m -s /bin/bash yaarenteractive
echo 'yaarenteractive:Yaare-OpenSSH-Test-Only!' | chpasswd
mkdir -p /run/sshd /home/yaaretest/files /home/yaaretest/readonly
printf 'Yaare SFTP test\n中文与 Emoji 🦂\n' > /home/yaaretest/files/hello.txt
dd if=/dev/zero of=/home/yaaretest/files/large.bin bs=1M count=16 status=none
chown -R yaaretest:yaaretest /home/yaaretest/files
chmod 555 /home/yaaretest/readonly
printf 'Port 22\nListenAddress 0.0.0.0\nPasswordAuthentication yes\nPubkeyAuthentication yes\nKbdInteractiveAuthentication yes\nUsePAM yes\nX11Forwarding yes\nPermitRootLogin no\nAllowUsers yaaretest yaarenteractive\nSubsystem sftp internal-sftp\nPrintMotd no\nMatch User yaarenteractive\n PasswordAuthentication no\n PubkeyAuthentication no\n KbdInteractiveAuthentication yes\n' > /etc/ssh/sshd_config
ssh-keygen -A
/usr/sbin/sshd -t
if [ -s /run/sshd.pid ] && kill -0 "$(cat /run/sshd.pid)" 2>/dev/null; then
    kill -HUP "$(cat /run/sshd.pid)"
else
    /usr/sbin/sshd
fi
sed -e 's/^Port 22$/Port 2223/' -e 's/^ListenAddress .*/ListenAddress 127.0.0.1/' -e '/^Subsystem sftp /d' /etc/ssh/sshd_config > /run/sshd-shell.conf
if [ -s /run/sshd-shell.pid ] && kill -0 "$(cat /run/sshd-shell.pid)" 2>/dev/null; then
    kill -HUP "$(cat /run/sshd-shell.pid)"
else
    /usr/sbin/sshd -f /run/sshd-shell.conf -o PidFile=/run/sshd-shell.pid
fi
