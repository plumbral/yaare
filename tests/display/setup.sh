#!/bin/sh
set -eu
sh /tmp/yaare-install-packages.sh xvfb x11vnc xterm x11-utils xauth fonts-dejavu-core openbox xrdp xorgxrdp dbus dbus-x11
if ! id yaaredisplay >/dev/null 2>&1; then
    useradd -m -s /bin/bash yaaredisplay
fi
printf 'yaaredisplay:%s\n' "$YAARE_DESKTOP_TEST_PASSWORD" | chpasswd
printf '#!/bin/sh\nexec dbus-run-session -- openbox-session\n' > /home/yaaredisplay/.xsession
chown yaaredisplay:yaaredisplay /home/yaaredisplay/.xsession
chmod 700 /home/yaaredisplay/.xsession
