CREATE TABLE schema_info (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    checksum TEXT NOT NULL
);

CREATE TABLE meta (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    revision INTEGER NOT NULL CHECK (revision >= 0)
);

CREATE TABLE groups (
    name TEXT PRIMARY KEY,
    position INTEGER NOT NULL
);

CREATE TABLE keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    format TEXT NOT NULL,
    path TEXT NOT NULL,
    cipher BLOB CHECK (cipher IS NULL OR length(cipher) BETWEEN 1 AND 2097152)
);

CREATE TABLE identities (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    username TEXT NOT NULL,
    authentication TEXT NOT NULL CHECK (authentication IN ('password', 'key')),
    key_id TEXT REFERENCES keys(id),
    version INTEGER NOT NULL,
    cipher BLOB NOT NULL,
    use_agent INTEGER NOT NULL DEFAULT 0 CHECK (use_agent IN (0, 1))
);

CREATE TABLE hosts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    username TEXT NOT NULL,
    group_name TEXT NOT NULL REFERENCES groups(name),
    system TEXT NOT NULL,
    icon TEXT NOT NULL,
    authentication TEXT NOT NULL CHECK (authentication IN ('none', 'password', 'key')),
    key_id TEXT REFERENCES keys(id),
    position INTEGER NOT NULL,
    identity_id TEXT REFERENCES identities(id),
    notes TEXT NOT NULL DEFAULT '',
    pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
    last_used INTEGER NOT NULL DEFAULT 0 CHECK (last_used >= 0),
    jump_hosts TEXT NOT NULL DEFAULT '[]',
    forward_rules TEXT NOT NULL DEFAULT '[]',
    timeout_seconds INTEGER NOT NULL DEFAULT 0 CHECK (timeout_seconds = 0 OR timeout_seconds BETWEEN 3 AND 120),
    proxy_config TEXT NOT NULL DEFAULT '{}',
    use_agent INTEGER NOT NULL DEFAULT 0 CHECK (use_agent IN (0, 1))
);

CREATE TABLE host_credentials (
    host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (length(scope) = 64),
    cipher BLOB NOT NULL CHECK (length(cipher) BETWEEN 1 AND 65536)
);

CREATE TABLE proxy_credentials (
    host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (length(scope) = 64),
    cipher BLOB NOT NULL CHECK (length(cipher) BETWEEN 1 AND 65536)
);

CREATE TABLE host_order (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    pins TEXT NOT NULL CHECK (length(pins) <= 1048576)
);

CREATE TABLE command_groups (
    name TEXT PRIMARY KEY,
    position INTEGER NOT NULL
);

CREATE TABLE commands (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    command TEXT NOT NULL,
    position INTEGER NOT NULL,
    group_name TEXT NOT NULL REFERENCES command_groups(name) ON UPDATE CASCADE,
    icon TEXT NOT NULL DEFAULT 'linux'
);

CREATE TABLE session_layout (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    revision INTEGER NOT NULL CHECK (revision > 0),
    payload TEXT NOT NULL CHECK (length(payload) <= 262144)
);

CREATE TABLE config_sync (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    version INTEGER NOT NULL CHECK (version > 0),
    settings BLOB NOT NULL CHECK (length(settings) BETWEEN 1 AND 65536),
    state_hash TEXT NOT NULL DEFAULT '' CHECK (state_hash = '' OR length(state_hash) = 64),
    paused INTEGER NOT NULL DEFAULT 0 CHECK (paused IN (0, 1))
);

CREATE TABLE desktop_preferences (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    version INTEGER NOT NULL CHECK (version > 0),
    revision INTEGER NOT NULL CHECK (revision > 0),
    portable BLOB CHECK (length(portable) BETWEEN 2 AND 4096),
    device BLOB CHECK (length(device) BETWEEN 2 AND 8192)
);

CREATE TABLE portable_extensions (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    data TEXT NOT NULL
);

CREATE INDEX commands_group ON commands(group_name);
CREATE INDEX commands_position ON commands(position, id);
CREATE INDEX hosts_group ON hosts(group_name);
CREATE INDEX hosts_position ON hosts(position, id);
CREATE INDEX hosts_key ON hosts(key_id) WHERE key_id IS NOT NULL;
CREATE INDEX hosts_identity ON hosts(identity_id) WHERE identity_id IS NOT NULL;
CREATE INDEX identities_key ON identities(key_id) WHERE key_id IS NOT NULL;

INSERT INTO meta (id, revision) VALUES (1, 0);
INSERT INTO host_order (id, pins) VALUES (1, '[]');
