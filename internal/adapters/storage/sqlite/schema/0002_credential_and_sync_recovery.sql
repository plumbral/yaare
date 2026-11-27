CREATE TABLE credential_key_check (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    cipher BLOB NOT NULL CHECK (length(cipher) BETWEEN 1 AND 65536)
);

ALTER TABLE config_sync ADD COLUMN baseline_lost INTEGER NOT NULL DEFAULT 0 CHECK (baseline_lost IN (0, 1));
