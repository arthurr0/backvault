CREATE TABLE hosts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 0,
    "user" TEXT NOT NULL DEFAULT '',
    auth TEXT NOT NULL DEFAULT 'key',
    private_key TEXT NOT NULL DEFAULT '',
    key_passphrase TEXT NOT NULL DEFAULT '',
    password TEXT NOT NULL DEFAULT '',
    public_key TEXT NOT NULL DEFAULT '',
    host_key TEXT NOT NULL DEFAULT '',
    sudo INTEGER NOT NULL DEFAULT 0,
    connect_timeout INTEGER NOT NULL DEFAULT 0,
    tags TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_test_at TEXT,
    last_test_ok INTEGER,
    last_test_error TEXT NOT NULL DEFAULT '',
    last_seen_os TEXT NOT NULL DEFAULT '',
    tools TEXT NOT NULL DEFAULT '[]'
);

CREATE UNIQUE INDEX idx_hosts_name ON hosts(name);

ALTER TABLE sources ADD COLUMN host_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_sources_host ON sources(host_id);
