#!/bin/sh
set -eu
if [ "${YAARE_DISPOSABLE_DBUS:-}" != 1 ]; then
    test_root=$(mktemp -d "${TMPDIR:-/tmp}/yaare-keyring-test.XXXXXX")
    trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM
    export XDG_DATA_HOME="$test_root/data" XDG_CONFIG_HOME="$test_root/config" XDG_RUNTIME_DIR="$test_root/runtime"
    export YAARE_DISPOSABLE_DBUS=1 YAARE_TEST_LINUX_KEYRING=1
    mkdir -m 700 -p "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$XDG_RUNTIME_DIR"
    dbus-run-session -- sh "$0"
    exit $?
fi
printf '%s' 'Yaare-Disposable-Keyring-Test!' | gnome-keyring-daemon --unlock --components=secrets >/dev/null
go test -count=1 -v ./internal/adapters/secrets -run '^TestLinuxRealKeyringConcurrentFirstCreation$'
go test -count=1 -v ./internal/desktop/ipc -run '^Test(SavedIdentityProtectionReuseAndReferences|KeyVault.*|Backup.*|LinuxEncryptedBackupUsesRealKeyring)$'
go test -count=1 -v ./internal/desktop/ipc -run '^TestLinuxLostKeyCannotReenrollWorkspace$'
if [ "${YAARE_OPENSSH_TEST:-}" = 1 ]; then
    go test -count=1 -v ./internal/desktop/ipc -run '^TestLinuxSavedIdentityRealSSHAndSFTP$'
fi
go test -count=1 -v ./internal/adapters/secrets -run '^TestLinuxRealKeyringProtectionAndLock$'
