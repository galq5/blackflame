-- +goose Up
CREATE TABLE IF NOT EXISTS boards (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    is_public   INTEGER NOT NULL DEFAULT 1,
    theme_json  TEXT,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS sections (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    board_id    INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    is_public   INTEGER NOT NULL DEFAULT 1,
    is_pinned   INTEGER NOT NULL DEFAULT 0,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (board_id, name)
);

CREATE TABLE IF NOT EXISTS items (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    board_id      INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    section_id    INTEGER REFERENCES sections(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL DEFAULT 'app',
    name          TEXT NOT NULL,
    url           TEXT NOT NULL DEFAULT '',
    icon          TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    is_public     INTEGER NOT NULL DEFAULT 1,
    is_pinned     INTEGER NOT NULL DEFAULT 0,
    is_hidden     INTEGER NOT NULL DEFAULT 0,
    check_enabled INTEGER NOT NULL DEFAULT 1,
    source        TEXT NOT NULL DEFAULT 'manual',
    external_id   TEXT,
    group_name    TEXT NOT NULL DEFAULT '',
    is_override   INTEGER NOT NULL DEFAULT 0,
    config_json   TEXT,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS items_source_external
    ON items(source, external_id)
    WHERE external_id IS NOT NULL AND external_id <> '';

CREATE INDEX IF NOT EXISTS items_board ON items(board_id);
CREATE INDEX IF NOT EXISTS items_section ON items(section_id);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    csrf       TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'admin',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS status_latest (
    item_key    TEXT PRIMARY KEY,
    up          INTEGER NOT NULL,
    status_code INTEGER,
    latency_ms  INTEGER,
    cert_expiry TEXT,
    error       TEXT,
    checked_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS icons (
    name         TEXT PRIMARY KEY,
    content_type TEXT NOT NULL,
    bytes        BLOB NOT NULL,
    source_url   TEXT,
    etag         TEXT,
    fetched_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE IF EXISTS icons;
DROP TABLE IF EXISTS status_latest;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS sections;
DROP TABLE IF EXISTS boards;
