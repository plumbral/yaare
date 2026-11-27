FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive LANG=C.UTF-8 TERM=xterm-256color
RUN apt-get update && apt-get install -y --no-install-recommends openssh-server bash vim nano tmux htop procps ncurses-term ca-certificates lrzsz && rm -rf /var/lib/apt/lists/* \
 && useradd -m -s /bin/bash yaaretest && echo 'yaaretest:Yaare-OpenSSH-Test-Only!' | chpasswd \
 && useradd -m -s /bin/bash yaarenteractive && echo 'yaarenteractive:Yaare-OpenSSH-Test-Only!' | chpasswd \
 && mkdir -p /run/sshd /home/yaaretest/files /home/yaaretest/readonly \
 && printf 'Yaare SFTP test\n中文与 Emoji 🦂\n' > /home/yaaretest/files/hello.txt \
 && dd if=/dev/zero of=/home/yaaretest/files/large.bin bs=1M count=16 \
 && chown -R yaaretest:yaaretest /home/yaaretest/files && chmod 555 /home/yaaretest/readonly
RUN printf 'Port 22\nListenAddress 0.0.0.0\nPasswordAuthentication yes\nPubkeyAuthentication yes\nKbdInteractiveAuthentication yes\nUsePAM yes\nPermitRootLogin no\nAllowUsers yaaretest yaarenteractive\nSubsystem sftp internal-sftp\nPrintMotd no\nMatch User yaarenteractive\n PasswordAuthentication no\n PubkeyAuthentication no\n KbdInteractiveAuthentication yes\n' > /etc/ssh/sshd_config
EXPOSE 22
CMD ["/bin/sh", "-c", "ssh-keygen -A && exec /usr/sbin/sshd -D -e"]
