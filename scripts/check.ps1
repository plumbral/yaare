$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    if ((Get-FileHash -LiteralPath 'LICENSE').Hash -ne (Get-FileHash -LiteralPath 'resources/LICENSE').Hash) {
        throw 'The desktop package license differs from the project license.'
    }
    & bun scripts/build-themes.ts --check
    if ($LASTEXITCODE -ne 0) { throw 'Generated theme is stale. Run bun run theme:generate.' }
    & bun run typecheck
    if ($LASTEXITCODE -ne 0) { throw 'Frontend type checking failed.' }
    & bun test tests/workspace.test.ts tests/workspace-tools.test.ts tests/workbench-refinements.test.ts tests/ssh-transport.test.ts tests/ssh-agent.test.ts tests/saved-workspace.test.ts tests/command-library.test.ts tests/network-config.test.ts tests/remote-text.test.ts tests/file-tree.test.ts tests/terminal-colors.test.ts tests/transfers.test.ts tests/session-close.test.ts tests/terminal-layout.test.ts tests/directory-link.test.ts tests/directory-history.test.ts tests/native-drop.test.ts tests/remember-workspace.test.ts tests/broadcast.test.ts tests/zmodem.test.ts tests/remote-desktop.test.ts tests/element-fullscreen.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Workspace tests failed.' }
    & bun test tests/ssh-reconnect.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'SSH reconnect tests failed.' }
    & bun test tests/terminal-renderer.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Terminal renderer tests failed.' }
    & bun test tests/terminal-updates.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Terminal update tests failed.' }
    & bun test tests/dialog-exit.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Dialog exit tests failed.' }
    & bun test tests/sync-status.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Configuration sync status tests failed.' }
    & bun test tests/host-batch.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Host batch tests failed.' }
    & bun test tests/connection-passwords.test.ts tests/group-creation.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Connection password tests failed.' }
    & bun test tests/host-pins.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Host pin tests failed.' }
    & bun test tests/host-order.test.ts tests/list-motion.test.ts tests/host-context-menu.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Host ordering, list motion or context menu tests failed.' }
    & bun test --isolate tests/workbench-startup.test.tsx tests/workbench-actions.test.tsx tests/connection-editor-retry.test.tsx tests/backup-recovery.test.tsx
    if ($LASTEXITCODE -ne 0) { throw 'Workbench and connection editor interaction tests failed.' }
    & bun test tests/settings-navigation.test.ts tests/appearance-bootstrap.test.ts tests/desktop-preferences.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Settings navigation tests failed.' }
    & bun test --isolate tests/application-updates.test.ts tests/about-updates.test.tsx
    if ($LASTEXITCODE -ne 0) { throw 'Application update tests failed.' }
    & bun test tests/i18n.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Interface translation tests failed.' }
    & bun test tests/fonts.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Font preference tests failed.' }
    & bun test tests/themes.test.ts
    if ($LASTEXITCODE -ne 0) { throw 'Theme palette tests failed.' }
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed.' }
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed.' }
    & go build ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go compilation failed.' }
} finally {
    Pop-Location
}
