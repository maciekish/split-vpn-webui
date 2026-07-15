# Tech Stack & Implemented Components

## Tech Stack (locked architecture decisions)

| Layer | Technology |
|---|---|
| Language | Go 1.24+, single binary |
| HTTP router | `github.com/go-chi/chi/v5` |
| SQLite driver | `modernc.org/sqlite` (pure Go, no CGo — essential for cross-compilation) |
| Frontend | Bootstrap 5, Chart.js, Bootstrap Icons, vanilla JS |
| Asset delivery | Go `embed.FS` (all static assets compiled into the binary) |
| VPN runtime | systemd units managed by this app |
| App data | JSON config files + SQLite under `/data/split-vpn-webui/` |
| Stats database | SQLite at `/data/split-vpn-webui/stats.db` |
| Logs | `/data/split-vpn-webui/logs/` |
| Live updates | Server-Sent Events (SSE) at `/api/stream` |

No runtime dependencies beyond the binary. No other databases, container runtimes, or system daemons.

## Currently Implemented Components

| Component | Location |
|---|---|
| Stats collection | `internal/stats/` — `/sys/class/net` polling, WAN correction, rolling history, SQLite persistence |
| Latency monitoring | `internal/latency/` — system ping, ref-counted activation |
| Config discovery | `internal/config/` — recursive `vpn.conf` parsing, autostart markers |
| Settings persistence | `internal/settings/` — JSON file, atomic writes |
| Authentication | `internal/auth/` — bcrypt password, API token, chi middleware |
| VPN provider abstraction | `internal/vpn/` — WireGuard + OpenVPN providers, allocator, name validation |
| systemd manager | `internal/systemd/` — unit write/symlink/daemon-reload, boot hook generation, self-healing |
| Routing engine | `internal/routing/` — ipset/iptables/dnsmasq/ip-rule CRUD, rule-based groups, atomic apply |
| Resolver | `internal/routing/resolver*.go` — domain/ASN/wildcard resolution, scheduling, 24h additive cache |
| DNS pre-warm | `internal/prewarm/` — DoH goroutine pool, per-interface binding, wildcard discovery, ECS profiles |
| Backup/restore | `internal/backup/` — versioned JSON export/import with rollback |
| Update manager | `internal/update/` — GitHub release check, checksum verify, self-update runner |
| Flow inspector | `internal/server/flow_inspector*.go` — conntrack-based per-VPN flow visibility |
| Speed test | `internal/speedtest/` — pure-Go Ookla client, interface-bound, live SSE streaming (`/api/speedtest/stream`) |
| Interface binding | `internal/netbind/` — shared `SO_BINDTODEVICE` dialer control (used by prewarm + speedtest) |
| Network utilities | `internal/util/network.go` — WAN/LAN detection, gateway resolution, interface state |
| Database | `internal/database/` — SQLite open/migrate/cleanup |
| HTTP server + SSE | `internal/server/` — chi router, SSE broadcaster, all REST handlers |
| Web UI | `ui/web/` — Bootstrap 5, Chart.js, SSE client; full VPN/routing/prewarm/settings management |
| Entry point | `cmd/splitvpnwebui/main.go` — flags, graceful shutdown, LAN auto-bind |
| Diagnostics logging | `internal/diaglog/` — runtime-configurable file logger |

## Key Architectural Invariants

- VPN provider pattern: `internal/vpn/` defines a `Provider` interface; `wireguard.go` and `openvpn.go` implement it.
- All persistent state: `/data/split-vpn-webui/` only. Nothing written outside this tree except transient symlinks in `/etc/systemd/system/` and dnsmasq drop-in in `/run/dnsmasq.d/`.
- Systemd unit files: canonical copies in `/data/split-vpn-webui/units/`; symlinked by the boot hook.
- WireGuard interface names are `wg-sv-<sanitized-name>` (15-char kernel limit, hash-suffix collision reduction).
- Route table IDs and fwmarks allocated from 200 upward; never below 200; collision-checked against live system.
- No file exceeds ~500 lines; split into subpackages before hitting the limit.
