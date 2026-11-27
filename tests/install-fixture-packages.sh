#!/bin/sh
set -eu
install_packages() {
    retries=$1
    shift
    apt-get -o APT::Update::Error-Mode=any -o Acquire::Retries="$retries" -o Acquire::http::Timeout=20 update -qq &&
        apt-get -o Acquire::Retries="$retries" -o Acquire::http::Timeout=20 install -y -qq --no-install-recommends "$@"
}
for package in "$@"; do
    if [ "$(dpkg-query -W -f='${db:Status-Status}' "$package" 2>/dev/null)" != installed ]; then
        export DEBIAN_FRONTEND=noninteractive
        if install_packages 0 "$@"; then
            exit 0
        fi
        unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY
        install_packages 3 "$@"
        exit 0
    fi
done
