#!/bin/sh
set -eu
test -n "${YAARE_SSH_IP:-}"
sh /tmp/yaare-install-packages.sh squid dante-server apache2-utils
id yaareproxy >/dev/null 2>&1 || useradd -M -s /usr/sbin/nologin yaareproxy
echo 'yaareproxy:Yaare-Proxy-Test-Only!' | chpasswd
mkdir -p /opt/yaare-proxy /run/squid
htpasswd -bc /opt/yaare-proxy/passwd yaareproxy 'Yaare-Proxy-Test-Only!'
chmod 644 /opt/yaare-proxy/passwd
for mode in plain auth; do
  if [ "$mode" = plain ]; then port=3128; socksport=1080; method=none; else port=3129; socksport=1081; method=username; fi
  config=/opt/yaare-proxy/squid-$mode.conf
  cat > "$config" <<EOF
http_port 0.0.0.0:$port
visible_hostname yaare-proxy-$mode
pid_filename /opt/yaare-proxy/squid-$mode.pid
access_log none
cache_log /tmp/yaare-squid-$mode.log
cache_store_log none
cache deny all
cache_mem 0 MB
acl ssh_target dst $YAARE_SSH_IP/32
acl ssh_port port 22
http_access deny !ssh_target
http_access deny !ssh_port
EOF
  if [ "$mode" = auth ]; then
    cat >> "$config" <<EOF
auth_param basic program /usr/lib/squid/basic_ncsa_auth /opt/yaare-proxy/passwd
auth_param basic realm Yaare-test
acl credentials proxy_auth REQUIRED
http_access allow credentials
EOF
  else echo 'http_access allow all' >> "$config"; fi
  echo 'http_access deny all' >> "$config"
  squid -k parse -f "$config"
  if [ -f /opt/yaare-proxy/squid-$mode.pid ] && kill -0 "$(cat /opt/yaare-proxy/squid-$mode.pid)" 2>/dev/null; then
    squid -k reconfigure -f "$config"
  else squid -f "$config"; fi
  config=/opt/yaare-proxy/dante-$mode.conf
  cat > "$config" <<EOF
logoutput: /tmp/yaare-dante-$mode.log
internal: 0.0.0.0 port = $socksport
external: eth0
clientmethod: none
socksmethod: $method
user.privileged: root
user.unprivileged: nobody
client pass {
  from: 0.0.0.0/0 to: 0.0.0.0/0
}
socks pass {
  from: 0.0.0.0/0 to: $YAARE_SSH_IP/32 port = 22
  command: connect
  socksmethod: $method
  protocol: tcp
}
EOF
  if [ -f /opt/yaare-proxy/dante-$mode.pid ] && kill -0 "$(cat /opt/yaare-proxy/dante-$mode.pid)" 2>/dev/null; then
    kill -HUP "$(cat /opt/yaare-proxy/dante-$mode.pid)"
  else danted -D -f "$config" -p /opt/yaare-proxy/dante-$mode.pid; fi
done
exit 0
