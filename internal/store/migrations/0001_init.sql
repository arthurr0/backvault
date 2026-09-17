CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'viewer',
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_login_at TEXT
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE api_tokens (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    hash TEXT NOT NULL UNIQUE,
    scopes TEXT NOT NULL DEFAULT '[]',
    job_slugs TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    last_used_at TEXT,
    expires_at TEXT
);

CREATE TABLE sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    config TEXT NOT NULL DEFAULT '{}',
    tags TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_test_at TEXT,
    last_test_ok INTEGER,
    last_test_error TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX idx_sources_name ON sources(name);

CREATE TABLE destinations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    config TEXT NOT NULL DEFAULT '{}',
    tags TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_test_at TEXT,
    last_test_ok INTEGER,
    last_test_error TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX idx_destinations_name ON destinations(name);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source_id TEXT NOT NULL REFERENCES sources(id) ON DELETE RESTRICT,
    destination_ids TEXT NOT NULL DEFAULT '[]',
    schedule TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    compression TEXT NOT NULL DEFAULT 'none',
    compression_level INTEGER NOT NULL DEFAULT 0,
    encryption TEXT NOT NULL DEFAULT 'none',
    encryption_passphrase TEXT NOT NULL DEFAULT '',
    retention TEXT NOT NULL DEFAULT '{}',
    notification_channel_ids TEXT NOT NULL DEFAULT '[]',
    notify_on TEXT NOT NULL DEFAULT '[]',
    timeout_minutes INTEGER NOT NULL DEFAULT 0,
    retries INTEGER NOT NULL DEFAULT 0,
    retry_delay_seconds INTEGER NOT NULL DEFAULT 0,
    pre_command TEXT NOT NULL DEFAULT '',
    post_command TEXT NOT NULL DEFAULT '',
    verify_after_upload INTEGER NOT NULL DEFAULT 0,
    expected_interval_minutes INTEGER NOT NULL DEFAULT 0,
    tags TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_jobs_source ON jobs(source_id);

CREATE TABLE job_state (
    job_id TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    last_run TEXT,
    last_success_at TEXT,
    last_duration_ms INTEGER NOT NULL DEFAULT 0,
    next_run_at TEXT,
    overdue INTEGER NOT NULL DEFAULT 0,
    overdue_notified_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL DEFAULT '',
    job_slug TEXT NOT NULL DEFAULT '',
    job_name TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    trigger_kind TEXT NOT NULL,
    status TEXT NOT NULL,
    queued_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    bytes INTEGER NOT NULL DEFAULT 0,
    raw_bytes INTEGER NOT NULL DEFAULT 0,
    sha256 TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    stages TEXT NOT NULL DEFAULT '[]',
    artifact_ids TEXT NOT NULL DEFAULT '[]',
    attempt INTEGER NOT NULL DEFAULT 0,
    meta TEXT NOT NULL DEFAULT '{}',
    created_by TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_runs_job ON runs(job_id, queued_at DESC);
CREATE INDEX idx_runs_queued ON runs(queued_at DESC);
CREATE INDEX idx_runs_status ON runs(status);

CREATE TABLE run_logs (
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    chunk TEXT NOT NULL,
    PRIMARY KEY (run_id, seq)
);

CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL DEFAULT '',
    job_slug TEXT NOT NULL DEFAULT '',
    job_name TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    destination_id TEXT NOT NULL DEFAULT '',
    destination_name TEXT NOT NULL DEFAULT '',
    destination_kind TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL,
    filename TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    sha256 TEXT NOT NULL DEFAULT '',
    compression TEXT NOT NULL DEFAULT 'none',
    encryption TEXT NOT NULL DEFAULT 'none',
    source_kind TEXT NOT NULL DEFAULT '',
    extension TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'present',
    created_at TEXT NOT NULL,
    verified_at TEXT,
    deleted_at TEXT,
    meta TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_artifacts_job ON artifacts(job_id, created_at DESC);
CREATE INDEX idx_artifacts_dest ON artifacts(destination_id);
CREATE INDEX idx_artifacts_run ON artifacts(run_id);
CREATE INDEX idx_artifacts_status ON artifacts(status);

CREATE TABLE notification_channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    config TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER NOT NULL DEFAULT 1,
    events TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_sent_at TEXT,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    data TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE audit_log (
    id TEXT PRIMARY KEY,
    time TEXT NOT NULL,
    actor_id TEXT NOT NULL DEFAULT '',
    actor_label TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    object_type TEXT NOT NULL DEFAULT '',
    object_id TEXT NOT NULL DEFAULT '',
    object_name TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT '{}',
    ip TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_audit_time ON audit_log(time DESC);
CREATE INDEX idx_audit_action ON audit_log(action);
