# Yaare development guide

Yaare is an SSH workbench. The desktop app runs in the system WebView through MyGo. The self-hosted edition runs in a browser. Both share a React interface and Go services. Each runtime handles local file access, credential protection, and session authorization.

## Code comments

Keep source code, tests, scripts, configuration, and output from project-owned generators free of explanatory comments. Put design explanations in project documentation or commit bodies. Preserve directives interpreted by a language or tool, such as Go build constraints, resource embeds, and script shebangs. Keep framework-generated annotations and required third-party license notices intact.

## Git commit messages

Every commit must have a title in the form `type(scope): summary`, followed by a blank line and a descriptive body.

- Choose the type that matches the change, such as `feat` for a new feature, `fix` for a bug fix, or `docs` for documentation. Use a short scope that names the affected area, such as `terminal`, `sftp`, or `sync`.
- Start the summary with an imperative verb and name the specific change.
- In the body, explain what changed and why. Include user-visible effects, compatibility details, or verification steps when they help the reader understand the change.

```text
feat(sync): preview changes before restoring a backup

Show which hosts and commands the selected backup would add, update,
or remove. This lets users review the impact on their current workspace
before they confirm the restore.
```

## Design and UI

- Use **Astryx Neutral** and the existing workbench components as design references. Look for a similar interaction before adding a control. Run `bun run astryx component Button` to inspect a component's API and `bun run astryx docs tokens` to look up colors, spacing, and corner radii. Both commands use the project's installed version.
- Hosts, identities, commands, and settings occupy the main work area. Use [EditorDialog](frontend/src/shared/ui/EditorDialog.tsx) for create and edit forms so the header and actions stay visible while the body scrolls. Use [ConfirmDelete](frontend/src/shared/ui/ConfirmDelete.tsx) for deletion. Give icon buttons a translatable action label. When changing a dialog, check keyboard focus, focus restoration on close, and shortcut handling during input method composition.
- Define shared component styles in the [workbench theme](frontend/src/shared/theme/workbenchTheme.ts), interface colors in [palettes.ts](frontend/src/shared/theme/palettes.ts), and terminal ANSI colors in the [terminal presets](frontend/src/shared/theme/terminal-presets.json). Use semantic tokens such as `--color-text-primary` in components. After editing theme sources, run `bun run theme:generate` and check light mode, dark mode, and keyboard focus.
- Each SSH session owns its terminal, SFTP directory, and sidebar state. When changing tabs or split panes, switch between two active sessions and verify that keystrokes reach the intended terminal, output appears in the correct pane, and each session retains its directory. See the [frontend feature guide](frontend/src/features/README.md).

## Architecture and data boundaries

- [Workbench](frontend/src/app/Workbench.tsx) assembles the interface. Keep feature-specific interactions in [features](frontend/src/features/), reusable controls in [shared/ui](frontend/src/shared/ui/), and frontend calls to Go services in [bridge](frontend/src/shared/bridge/). The desktop app starts in [cmd/yaare](cmd/yaare/) and the browser server starts in [cmd/yaare-web](cmd/yaare-web/).
- Keep domain models and merge rules in `internal/domain`, use cases in `internal/app`, and repository interfaces in `internal/ports`. SQLite, SSH/SFTP, and platform credential protection belong in `internal/adapters`. Constructors and binding generation must work offline without opening a user workspace. Initialize storage and start background workers during application startup. See the [backend boundaries](internal/README.md).
- When changing a public API, update the Go service first and run `bun run generate` to refresh the frontend bindings. For methods exposed to the browser, check the [browser method allowlist](internal/desktop/ipc/browser_methods.go) and [WebSocket RPC handler](internal/webserver/rpc.go). Check the caller's authorization for file, port forwarding, and session operations.
- Handle passwords, private keys, and temporary authentication input through the existing credential interfaces. Keep credentials and remote file contents out of frontend storage and diagnostic logs. For each new persisted field, decide whether it belongs to the device or travels with the workspace. Verify that older configurations still load. See [sync and backup](docs/Sync_and_Backup.md) and [encrypted exports](docs/Local_Encrypted_Config.md).

## Localization, bundled assets, and testing

- Use `tr(...)` for interface text. Register the message key in [sources.json](frontend/src/shared/i18n/sources.json) and update the English, Simplified Chinese, Traditional Chinese, and Japanese [message catalogs](locales/). Keep parameter placeholders consistent across all four, then run `bun run generate`. Preserve terminal output, file contents, and user-defined names verbatim.
- When bundling a font, icon, or dependency, include its source, license, and required attribution in the [third-party notices](resources/licenses/). Preserve the icon IDs used by saved connections when replacing provider artwork.
- Install dependencies with `bun install --frozen-lockfile`. Start with tests for the affected feature, such as `bun test tests/themes.test.ts` for themes or `bun test tests/i18n.test.ts` for translations. Run `.\scripts\check.ps1` for the full Windows checks. Use loopback fixtures for SSH tests and an isolated `--yaare-data-dir` when exercising storage. See the [testing guide](tests/README.md).
