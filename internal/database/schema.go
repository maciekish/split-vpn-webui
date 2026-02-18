package database

// schema contains all table definitions. Each statement is idempotent (CREATE IF NOT EXISTS).
const schema = `
CREATE TABLE IF NOT EXISTS stats_history (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    interface TEXT    NOT NULL,
    timestamp INTEGER NOT NULL,
    rx_bytes  INTEGER NOT NULL DEFAULT 0,
    tx_bytes  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_stats_history_iface_ts
    ON stats_history (interface, timestamp);

CREATE TABLE IF NOT EXISTS domain_groups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    egress_vpn TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TABLE IF NOT EXISTS domain_entries (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES domain_groups(id) ON DELETE CASCADE,
    domain   TEXT    NOT NULL,
    UNIQUE(group_id, domain)
);
CREATE INDEX IF NOT EXISTS idx_domain_entries_group
    ON domain_entries (group_id);

CREATE TABLE IF NOT EXISTS prewarm_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at       INTEGER NOT NULL,
    finished_at      INTEGER,
    duration_ms      INTEGER,
    domains_total    INTEGER NOT NULL DEFAULT 0,
    domains_done     INTEGER NOT NULL DEFAULT 0,
    ips_inserted     INTEGER NOT NULL DEFAULT 0,
    error            TEXT
);
`
