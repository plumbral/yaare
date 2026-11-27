# Backend boundaries

- `app` hosts use cases. `app/sync` handles portable workspace conversion, baseline decoding, merge publication and background scheduling. `app/transfers` handles queue priorities and drives resumable copies. `app/vault` validates and protects key material.
- `domain` holds workspace, preference and sync models. `domain/syncmodel` owns portable entries, canonicalization, ordering and conflict resolution.
- `ports` declares the repository operations the application requires. SQLite implements these ports. Credentials stay behind the backend interfaces.
- `desktop` adapts MyGo. The IPC services own caller validation, page-scoped grants and preview lifetimes, and map backend operations to the generated API.
- `adapters` implements real SSH, SFTP, configuration storage, platform protection and the packed frontend. `testutil` provides independent loopback fixtures kept out of the production entry point.

Constructors and binding generation perform no user-data or network I/O. Storage initialization and background workers start in the desktop lifecycle.

## Workspace database upgrades

Keep each published SQL file in `adapters/storage/sqlite/schema/` unchanged. Append a numbered script to the migration list in `migrations.go`. The same scripts create new databases and upgrade existing ones. Before upgrading a workspace, the store preserves a verified SQLite snapshot. It applies the scripts, updates the version and validates the result in one transaction. Older clients refuse writes to a newer database.

When changing a persisted field, update backup verification for the relevant schema versions and decide whether the field belongs in portable exports. Local recovery reads the original snapshot with its recorded schema signature and upgrades the restored copy. Keep a snapshot produced by the previous release in `adapters/storage/sqlite/testdata/`; exercise both direct upgrade and recovery from that snapshot.
