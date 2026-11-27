#!/bin/sh
set -eu
mkdir -p /run/xrdp /run/dbus
chown xrdp:xrdp /run/xrdp
if [ ! -S /run/dbus/system_bus_socket ]; then dbus-daemon --system --fork; fi
Xvfb :99 -screen 0 1280x720x24 -ac -listen tcp &
export DISPLAY=:99
for attempt in $(seq 1 50); do
    if xdpyinfo >/dev/null 2>&1; then break; fi
    sleep 0.1
done
xdpyinfo >/dev/null
openbox &
xterm -title 'Yaare display test' &
x11vnc -display :99 -forever -shared -nopw -rfbport 5900 &
/usr/sbin/xrdp-sesman --nodaemon &
exec /usr/sbin/xrdp --nodaemon
