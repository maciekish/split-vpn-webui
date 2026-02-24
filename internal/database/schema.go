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

CREATE TABLE IF NOT EXISTS routing_rules (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES domain_groups(id) ON DELETE CASCADE,
    name     TEXT    NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    exclude_multicast INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_routing_rules_group
    ON routing_rules (group_id, position);

CREATE TABLE IF NOT EXISTS routing_rule_source_cidrs (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    cidr    TEXT    NOT NULL,
    UNIQUE(rule_id, cidr)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_source_cidrs_rule
    ON routing_rule_source_cidrs (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_excluded_source_cidrs (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    cidr    TEXT    NOT NULL,
    UNIQUE(rule_id, cidr)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_excluded_source_cidrs_rule
    ON routing_rule_excluded_source_cidrs (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_source_interfaces (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    iface   TEXT    NOT NULL,
    UNIQUE(rule_id, iface)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_source_interfaces_rule
    ON routing_rule_source_interfaces (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_source_macs (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    mac     TEXT    NOT NULL,
    UNIQUE(rule_id, mac)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_source_macs_rule
    ON routing_rule_source_macs (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_destination_cidrs (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    cidr    TEXT    NOT NULL,
    UNIQUE(rule_id, cidr)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_destination_cidrs_rule
    ON routing_rule_destination_cidrs (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_excluded_destination_cidrs (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    cidr    TEXT    NOT NULL,
    UNIQUE(rule_id, cidr)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_excluded_destination_cidrs_rule
    ON routing_rule_excluded_destination_cidrs (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_ports (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id    INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    protocol   TEXT    NOT NULL,
    start_port INTEGER NOT NULL,
    end_port   INTEGER NOT NULL,
    UNIQUE(rule_id, protocol, start_port, end_port)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_ports_rule
    ON routing_rule_ports (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_excluded_ports (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id    INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    protocol   TEXT    NOT NULL,
    start_port INTEGER NOT NULL,
    end_port   INTEGER NOT NULL,
    UNIQUE(rule_id, protocol, start_port, end_port)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_excluded_ports_rule
    ON routing_rule_excluded_ports (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_asns (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    asn     TEXT    NOT NULL,
    UNIQUE(rule_id, asn)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_asns_rule
    ON routing_rule_asns (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_excluded_asns (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    asn     TEXT    NOT NULL,
    UNIQUE(rule_id, asn)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_excluded_asns_rule
    ON routing_rule_excluded_asns (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_domains (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id     INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    domain      TEXT    NOT NULL,
    is_wildcard INTEGER NOT NULL DEFAULT 0,
    UNIQUE(rule_id, domain, is_wildcard)
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_domains_rule
    ON routing_rule_domains (rule_id);

CREATE TABLE IF NOT EXISTS routing_rule_selector_lines (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id  INTEGER NOT NULL REFERENCES routing_rules(id) ON DELETE CASCADE,
    selector TEXT    NOT NULL,
    line     TEXT    NOT NULL,
    position INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_routing_rule_selector_lines_rule
    ON routing_rule_selector_lines (rule_id, selector, position, id);

CREATE TABLE IF NOT EXISTS resolver_cache (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    selector_type TEXT    NOT NULL,
    selector_key  TEXT    NOT NULL,
    family        TEXT    NOT NULL,
    cidr          TEXT    NOT NULL,
    updated_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(selector_type, selector_key, family, cidr)
);
CREATE INDEX IF NOT EXISTS idx_resolver_cache_selector
    ON resolver_cache (selector_type, selector_key, family);

CREATE TABLE IF NOT EXISTS resolver_runs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at        INTEGER NOT NULL,
    finished_at       INTEGER,
    duration_ms       INTEGER,
    selectors_total   INTEGER NOT NULL DEFAULT 0,
    selectors_done    INTEGER NOT NULL DEFAULT 0,
    prefixes_resolved INTEGER NOT NULL DEFAULT 0,
    error             TEXT
);

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

CREATE TABLE IF NOT EXISTS prewarm_cache (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    set_name   TEXT    NOT NULL,
    family     TEXT    NOT NULL,
    cidr       TEXT    NOT NULL,
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(set_name, family, cidr)
);
CREATE INDEX IF NOT EXISTS idx_prewarm_cache_set
    ON prewarm_cache (set_name, family);
`
