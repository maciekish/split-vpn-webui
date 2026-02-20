# split-vpn-webui — Implementation Plan

> Before starting any sprint, read `docs/PROGRESS.md` to confirm which sprint is active and check for notes left by previous sessions.
> After completing a sprint, update `docs/PROGRESS.md` before ending the session.

---

## Codebase Baseline (as of plan creation)

The following is **already fully implemented** and must not regress:

| Component | Location | Notes |
|---|---|---|
| Stats collection | `internal/stats/stats.go` | `/sys/class/net` polling, WAN correction, rolling history |
| Latency monitoring | `internal/latency/latency.go` | System ping, ref-counted activation |
| Config discovery | `internal/config/config.go` | Recursive `vpn.conf` parsing, autostart markers |
| Settings persistence | `internal/settings/settings.go` | JSON file, atomic writes |
| Network utilities | `internal/util/network.go` | WAN detection, gateway resolution |
| HTTP server + SSE | `internal/server/server.go` | chi router, SSE broadcaster, partial handlers |
| Web UI (monitoring) | `ui/web/` | Bootstrap 5, Chart.js, SSE client; control buttons disabled |
| Entry point | `cmd/splitvpnwebui/main.go` | Flags, graceful shutdown |

**Known issues to fix in Sprint 1:**
- All paths still reference `/mnt/data/split-vpn` (legacy). Must change to `/data/split-vpn-webui`.
- `deploy/split-vpn-webui.service` and `install.sh` use old paths and wrong install pattern.
- `handleWriteConfig`, `handleStartVPN`, `handleStopVPN`, `handleAutostart` all return 501.
- `controlsEnabled = false` in `app.js` disables all VPN control UI.
- No authentication.
- No SQLite — stats history is in-memory and lost on restart.

---

## Architecture Decisions (locked, do not revisit)

- **Language:** Go 1.24+, single binary, `embed.FS` for all UI assets.
- **HTTP router:** `github.com/go-chi/chi/v5`.
- **SQLite driver:** `modernc.org/sqlite` (pure Go, no CGo, no external `.so` required — essential for cross-compilation and embedded deployment).
- **VPN provider pattern:** `internal/vpn/` defines a `Provider` interface; `wireguard.go` and `openvpn.go` implement it.
- **All persistent state:** `/data/split-vpn-webui/` only. Nothing written outside this tree except transient symlinks in `/etc/systemd/system/` and the dnsmasq drop-in in `/run/dnsmasq.d/`.
- **Systemd unit files:** Canonical copies in `/data/split-vpn-webui/units/`; symlinked to `/etc/systemd/system/` by the boot hook on every boot.
- **Boot hook:** `/data/on_boot.d/10-split-vpn-webui.sh` — re-creates all symlinks, runs `daemon-reload`, starts the service. This is the persistence mechanism across firmware updates.
- **Namespace prefixes:** systemd units `svpn-<name>.service`; ipsets `svpn_<group>_v4/v6`; data dir `split-vpn-webui`.
- **No shell interpolation:** All `exec.Command` calls use explicit argument slices.
- **File permissions:** VPN private keys and credentials stored `0600`; directories `0700`.
- **Testing:** Unit tests in every package using mocks/interfaces for all kernel interactions. Integration tests tagged `//go:build integration`.

---

## Sprint Overview

| Sprint | Focus | Key Deliverable |
|---|---|---|
| **1** | Foundation reset | Correct paths, SQLite DB, authentication |
| **2** | VPN provider abstraction | Split server.go, parse/write WireGuard & OpenVPN configs, resource allocation, name validation |
| **3** | systemd manager | Write units, manage symlinks, start/stop/restart via API |
| **4** | Domain groups & routing | ipset/dnsmasq/iptables CRUD, REST API |
| **5** | DNS pre-warm | DoH worker, goroutine pool, scheduling, SSE progress |
| **6** | Web UI — VPN management | Full VPN CRUD + lifecycle controls in browser |
| **7** | Web UI — Domain routing | Domain group management in browser |
| **8** | Web UI — Pre-warm, auth & settings | Pre-warm dashboard, settings page, password/token management |
| **9** | Install script & hardening | `install.sh`, boot hook, input validation, file permissions |
| **10** | Persistent stats, build & CI | SQLite stats history, cross-compile, GitHub Actions |

---

## Sprint 1 — Foundation Reset

**Goal:** Fix all wrong paths, add SQLite schema, add authentication. After this sprint the app starts correctly on a real UniFi gateway, reads/writes to the right location, and requires a password to access.

### Files to modify

| File | Change |
|---|---|
| `cmd/splitvpnwebui/main.go` | Change `-split-vpn-dir` default to `/data/split-vpn-webui`; add `-db` flag defaulting to `/data/split-vpn-webui/stats.db`; add `-data-dir` flag; wire up new DB and auth packages |
| `internal/settings/settings.go` | Update default path to `/data/split-vpn-webui/settings.json`; add `AuthPasswordHash`, `AuthToken` fields to `Settings` struct |
| `internal/config/config.go` | Update base path default; no logic changes |
| `internal/server/server.go` | Add auth middleware; inject DB and auth manager; fix `handleWriteConfig` to call `config.WriteConfigFile` properly (un-stub it) |
| `deploy/split-vpn-webui.service` | Fix `ExecStart` to `/data/split-vpn-webui/split-vpn-webui`; remove `--split-vpn-dir`; add correct working dir |
| `deploy/split-vpn-webui.sh` | Rewrite as the boot hook pattern (symlink unit, daemon-reload, start service) |

### Files to create

| File | Purpose |
|---|---|
| `internal/database/database.go` | Open/create SQLite DB; run migrations; expose `*sql.DB` |
| `internal/database/schema.go` | SQL schema constants (tables: `stats_history`, `domain_groups`, `domain_entries`, `prewarm_runs`) |
| `internal/auth/auth.go` | `Manager` struct; `CheckPassword(plain string) bool`; `GenerateToken() string`; `ValidateToken(t string) bool`; `SetPassword(plain string) error` |
| `internal/auth/middleware.go` | chi middleware: checks session cookie OR `Authorization: Bearer <token>` header; redirects unauthenticated browser requests to `/login`; returns 401 for API requests |
| `internal/auth/login.go` | `handleLogin` (GET serves login page, POST validates and sets cookie) |
| `ui/web/templates/login.html` | Simple Bootstrap login form (username field omitted — single-admin, password only) |
| `internal/database/database_test.go` | Tests: schema creation, idempotent migration |
| `internal/auth/auth_test.go` | Tests: password hash/check, token generation/validation |

### Deliverables / Definition of Done

- [ ] App starts with `./split-vpn-webui` and creates `/data/split-vpn-webui/` tree if absent.
- [ ] SQLite DB created at `/data/split-vpn-webui/stats.db` with all tables on first start.
- [ ] All browser requests (except `/login`) redirect to login page if unauthenticated.
- [ ] Default password `split-vpn` works on first login; sets a session cookie.
- [ ] API requests with `Authorization: Bearer <token>` header are accepted without cookie.
- [ ] `PUT /api/configs/{name}/file` now saves the file (no longer 501).
- [ ] All tests pass: `go test ./...`.
- [ ] `deploy/split-vpn-webui.service` references `/data/split-vpn-webui/split-vpn-webui`.

---

## Sprint 2 — VPN Provider Abstraction

**Goal:** A clean, testable layer for reading, writing, and validating WireGuard and OpenVPN config files. Route table and fwmark allocator that guarantees no collision with UniFi or peacey/split-vpn. Also: split the oversized `server.go` (743 lines, violates the 500-line limit) before adding new handlers, fix the `go.mod` indirect markers, and unify duplicated helpers.

### Pre-requisite: server.go split

`server.go` is already 743 lines — well above the 500-line limit. Before adding any new handlers, split it into:

| New file | Content moved from `server.go` |
|---|---|
| `internal/server/server.go` | Core `Server` struct, `New()`, `Router()`, `StartBackground()`, SSE stream handler, watcher management, `broadcastUpdate`, `createPayload` |
| `internal/server/handlers_auth.go` | `handleLoginGet`, `handleLoginPost`, `handleLogout` |
| `internal/server/handlers_config.go` | `handleListConfigs`, `handleReadConfig`, `handleWriteConfig`, `handleStartVPN`, `handleStopVPN`, `handleAutostart`, `handleReload` |
| `internal/server/handlers_settings.go` | `handleGetSettings`, `handleSaveSettings`, `scheduleRestart` |
| `internal/server/handlers_vpn.go` | New Sprint 2 VPN CRUD handlers: create, list, get, update, delete |
| `internal/server/helpers.go` | `interfaceState`, `fileExists`, `dominantKey`, `writeJSON` — also unify `interfaceState` with the duplicate in `stats.go` by moving it to `internal/util/` |
| `internal/server/state.go` | `refreshState`, `resolveGateway`, `statsWAN`, `collectConfigStatuses`, `startVPN`, `stopVPN`, `applyAutostart`, `runStartStopCommand`, `runCommand` |

### Housekeeping

- Run `go mod tidy` to fix `golang.org/x/crypto` listed as `// indirect` when it is a direct import in `internal/auth`.
- Move the duplicate `interfaceState()` / `readInterfaceState()` logic (present in both `server.go` and `stats.go`) into `internal/util/network.go` and have both packages call it.

### Files to create

| File | Purpose |
|---|---|
| `internal/vpn/provider.go` | `Provider` interface: `Type() string`, `ValidateConfig(raw string) error`, `ParseConfig(raw string) (*VPNProfile, error)`, `GenerateUnit(profile *VPNProfile, dataDir string) string` |
| `internal/vpn/profile.go` | `VPNProfile` struct: `Name`, `Type`, `RawConfig`, `RouteTable`, `FWMark`, `InterfaceName`, `Gateway`, `BoundInterface`; `VPNMeta` (vpn.conf key-value map) |
| `internal/vpn/wireguard.go` | `WireGuardProvider` implementing `Provider`; `ParseWGConfig` (INI-style parser for `[Interface]`/`[Peer]` blocks); `ValidateWGConfig`; `GenerateUnit` wrapping `wg-quick` |
| `internal/vpn/openvpn.go` | `OpenVPNProvider` implementing `Provider`; `ValidateOVPNConfig` (check for required directives: `client`, `remote`, `dev`); `GenerateUnit` wrapping `openvpn --config` |
| `internal/vpn/manager.go` | `Manager`: CRUD for VPN profiles in `/data/split-vpn-webui/vpns/<name>/`; writes `vpn.conf`, the raw VPN config file, and calls allocator. **Validates VPN names** (see validation rules below). |
| `internal/vpn/allocator.go` | `Allocator`: reads `/etc/iproute2/rt_tables` + `ip rule` output; maintains an in-memory set of used table IDs and marks; `AllocateTable() (int, error)`, `AllocateMark() (uint32, error)`, `Release(table int, mark uint32)` — starts allocation from 200. **On startup, scans all persisted `vpn.conf` files** to rebuild the in-memory allocation set (prevents duplicate allocation after reboot). |
| `internal/vpn/validate.go` | `ValidateName(name string) error` — shared name validation: must match `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`; rejects path traversal (`..`, `/`, `\`); rejects systemd-reserved chars (`@`); rejects collision with existing VPN names. Also: `ValidateDomain(domain string) error` for later use. |
| `internal/vpn/wireguard_test.go` | Table-driven tests: parse valid config, parse invalid config, validate all required fields, handle whitespace-tolerant `Address` parsing (e.g. `10.49.1.2 ,2001:db8:a161::2`) |
| `internal/vpn/openvpn_test.go` | Table-driven tests: validate required directives, reject invalid configs, handle inline `<ca>`/`<cert>`/`<key>` blocks |
| `internal/vpn/allocator_test.go` | Tests: allocation starts at 200, no duplicates, collision with mocked existing tables, reboot recovery from persisted vpn.conf files |
| `internal/vpn/manager_test.go` | Tests: create/read/update/delete profile using temp dir, name validation enforcement |
| `internal/vpn/validate_test.go` | Tests: valid names, path traversal rejection, systemd-reserved chars rejection, length limits |

### Files to modify

| File | Change |
|---|---|
| `internal/server/server.go` | **Split into multiple files** (see table above). Add `vpn.Manager` injection to `Server` struct. |
| `internal/server/handlers_vpn.go` | New file: REST handlers: `POST /api/vpns` (create), `GET /api/vpns` (list), `GET /api/vpns/{name}` (get), `PUT /api/vpns/{name}` (update), `DELETE /api/vpns/{name}` (delete) |
| `internal/util/network.go` | Add `InterfaceOperState(name string) (up bool, state string, err error)` — unified from `server.go` and `stats.go` duplicates |
| `go.mod` | `go mod tidy` to fix indirect markers |

### VPN name validation rules (enforced in Sprint 2, not deferred to Sprint 9)

VPN names become directory names **and** systemd unit suffixes (`svpn-<name>.service`). Validation must be enforced at creation time:
- Pattern: `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`
- Rejected: path separators (`/`, `\`), parent traversal (`..`), systemd-reserved chars (`@`), whitespace, control characters
- Max length: 64 characters (keeps systemd unit name under 256-char limit including `svpn-` prefix and `.service` suffix)
- Uniqueness: reject if a VPN with the same name already exists on disk

### WireGuard parser edge cases

- **`Address` field**: Must handle whitespace-tolerant comma-separated CIDR list with mixed IPv4 and IPv6 (reference: `10.49.1.2 ,2001:db8:a161::2` — note the space before the comma).
- **`Table` directive**: The user-supplied `Table` value in the WG config should be preserved if present. The allocator must register it (mark as used) rather than override it. If `Table` is absent, the allocator assigns one and the parser injects it.
- **`PostUp`/`PreDown`/`PostDown`**: Preserve user-supplied hook commands. Do NOT inject peacey/split-vpn `updown.sh` references — this app manages routing independently. If importing a config that references peacey scripts, strip those specific lines and warn in the API response.
- **`PresharedKey`**: Must be stored with `0600` permissions alongside the config.
- **Multiple `[Peer]` sections**: Parser must handle configs with more than one peer.

### OpenVPN parser edge cases

- **Inline blocks**: Handle `<ca>`, `<cert>`, `<key>`, `<tls-crypt>`, `<tls-auth>` inline sections (multi-line content between XML-like tags).
- **Separate credential files**: If the `.ovpn` references external files (`ca`, `cert`, `key` directives without inline blocks), those files must be uploaded separately and stored alongside.
- **`dev` directive**: Extract the interface name (e.g. `dev tun` vs `dev tun0` — if just `dev tun`, a specific name like `tun0` must be assigned to avoid conflicts).

### Allocator reboot recovery

On startup, the allocator **must scan all persisted `vpn.conf` files** (in `/data/split-vpn-webui/vpns/*/vpn.conf`) to rebuild its in-memory used-set. Without this, a reboot followed by a new VPN creation could allocate a table ID or fwmark that collides with an existing VPN that simply hasn't been started yet.

### Deliverables / Definition of Done

- [x] `server.go` split into ≤500-line files; no file exceeds the limit.
- [x] `go mod tidy` run; `golang.org/x/crypto` marked as direct dependency.
- [x] Duplicate `interfaceState` unified into `internal/util/`.
- [x] `wireguard.ParseWGConfig` correctly parses the sample `wg0.conf` from the reference repo, including whitespace-tolerant `Address` and `Table` extraction.
- [x] `openvpn.ValidateOVPNConfig` correctly validates the sample `DreamMachine.ovpn`, including inline `<ca>`/`<cert>`/`<key>` blocks.
- [x] VPN name validation rejects path traversal, systemd-reserved chars, and overlong names.
- [x] Allocator never issues table ID or fwmark below 200.
- [x] Allocator reads live system tables and avoids conflicts.
- [x] Allocator recovers persisted allocations from existing `vpn.conf` files on startup.
- [x] Full CRUD API for VPN profiles: create, list, get, update, delete — all returning JSON.
- [x] Files stored with correct permissions: `vpn.conf` 0644, private key material 0600, VPN directories 0700.
- [x] All tests pass: `go test ./...` (not just `./internal/vpn/...`).

---

## Sprint 3 — systemd Manager

**Goal:** The app can create, symlink, start, stop, restart, and remove systemd units for managed VPNs. The boot hook script is generated and written. Stubbed start/stop API endpoints are fully implemented. Legacy `runStartStopCommand` code is removed.

### Generated systemd unit design

**WireGuard unit** (`svpn-<name>.service`):
```ini
[Unit]
Description=split-vpn-webui WireGuard tunnel (<name>)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=WG_ENDPOINT_RESOLUTION_RETRIES=infinity
ExecStart=/usr/bin/wg-quick up /data/split-vpn-webui/vpns/<name>/<file>.wg
ExecStop=/usr/bin/wg-quick down /data/split-vpn-webui/vpns/<name>/<file>.wg
TimeoutStartSec=2min
TimeoutStopSec=1min
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW

[Install]
WantedBy=multi-user.target
```

Key differences from reference:
- Uses **absolute paths** to config files (not relative + `WorkingDirectory`).
- Does **not** use `EnvironmentFile` to load `vpn.conf` — this app manages routing independently.
- Does **not** reference peacey/split-vpn `updown.sh` — no `PostUp`/`PreDown` hooks injected by the app (user-supplied hooks in the WG config are preserved).
- No `ExecStartPre=/bin/sleep 30` by default (configurable delay can be added later).

**OpenVPN unit** (`svpn-<name>.service`):
```ini
[Unit]
Description=split-vpn-webui OpenVPN tunnel (<name>)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/sbin/openvpn --config /data/split-vpn-webui/vpns/<name>/<file>.ovpn --dev <iface> --route-noexec --script-security 1
Restart=on-failure
RestartSec=5s
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW

[Install]
WantedBy=multi-user.target
```

Key differences from reference:
- `--route-noexec` prevents OpenVPN from adding its own routes (this app manages routing).
- `--script-security 1` prevents OpenVPN from running external scripts (no `--up`/`--down` hooks to peacey scripts).
- `--dev <iface>` uses the interface name from the VPN profile (assigned by allocator or user).
- Does **not** use `--redirect-gateway def1` — routing is managed by this app's iptables/ipset rules.

### Files to create

| File | Purpose |
|---|---|
| `internal/systemd/manager.go` | `Manager`: `WriteUnit(name, content string) error` (writes to `units/`, creates symlink in `/etc/systemd/system/`, runs daemon-reload); `RemoveUnit(name string) error`; `Start/Stop/Restart/Enable/Disable/Status(name string)` — all call `exec.Command("systemctl", ...)` with explicit args. Defined behind an interface for mockability. |
| `internal/systemd/bootscript.go` | `WriteBootHook(dataDir string) error` — generates and writes `/data/on_boot.d/10-split-vpn-webui.sh` with correct symlink logic for this app's unit and all `svpn-*.service` units; sets executable bit |
| `internal/systemd/mock.go` | `MockManager` implementing same interface as `Manager` for tests |
| `internal/systemd/manager_test.go` | Tests using temp dirs: unit file written correctly, symlink created, boot script content correct |

### Files to modify

| File | Change |
|---|---|
| `internal/server/state.go` | Remove legacy `runStartStopCommand` and `runCommand`. Replace `startVPN`/`stopVPN` to call `systemd.Start("svpn-<name>")` / `systemd.Stop("svpn-<name>")`. |
| `internal/server/handlers_config.go` | Implement `handleStartVPN` via systemd; implement `handleStopVPN` via systemd; implement `handleAutostart` (calls `systemd.Enable`/`systemd.Disable` + updates marker file); add `POST /api/vpns/{name}/restart` handler |
| `internal/vpn/manager.go` | On `Create`: call `systemd.WriteUnit` with generated unit content. On `Delete`: call `systemd.RemoveUnit`. |
| `cmd/splitvpnwebui/main.go` | Instantiate `systemd.Manager`; call `systemd.WriteBootHook` on startup if hook is missing or outdated |

### Deliverables / Definition of Done

- [ ] Creating a VPN via API writes the systemd unit to `/data/split-vpn-webui/units/svpn-<name>.service` and creates the symlink in `/etc/systemd/system/`.
- [ ] `POST /api/configs/{name}/start` starts the VPN via `systemctl start svpn-<name>`.
- [ ] `POST /api/configs/{name}/stop` stops it.
- [ ] `POST /api/vpns/{name}/restart` restarts it.
- [ ] `POST /api/configs/{name}/autostart` enables/disables the systemd unit.
- [ ] Deleting a VPN removes the unit file, removes the symlink, and runs `daemon-reload`.
- [ ] Boot hook written to `/data/on_boot.d/10-split-vpn-webui.sh` with correct content and `chmod +x`.
- [ ] Legacy `runStartStopCommand` / `runCommand` code is deleted — no fallback to `wg-quick` or `pkill`.
- [ ] No shell string interpolation anywhere — all `exec.Command` calls use string slices.
- [ ] All tests pass.

---

## Sprint 4 — Domain Groups & Routing

**Goal:** Users can create named domain groups, assign domains to them, and assign an egress VPN. The app applies the corresponding ipset, dnsmasq, and iptables/ip6tables rules atomically. Full IPv4 and IPv6 support.

### dnsmasq config path

The reference `20-ipset.sh` writes to `/run/dnsmasq.dhcp.conf.d/20-ipset.conf`. On different UniFi firmware versions the dnsmasq drop-in directory may be either `/run/dnsmasq.d/` or `/run/dnsmasq.dhcp.conf.d/`. The implementation must **probe both paths at startup** and use whichever exists. If neither exists, attempt to create `/run/dnsmasq.d/` (standard path). Log a warning if dnsmasq reload fails so the user can troubleshoot.

### Routing rules — full pattern

For each domain group with an assigned egress VPN (which has fwmark `MARK` and route table `ROUTE_TABLE`):

```
# IPv4
iptables  -t mangle -A PREROUTING -m set --match-set svpn_<group>_v4 dst -j MARK --set-mark <MARK> -m comment --comment svpn
iptables  -t nat    -A POSTROUTING -m mark --mark <MARK> -o <vpn_dev> -j MASQUERADE -m comment --comment svpn
ip rule add fwmark <MARK> table <ROUTE_TABLE> priority 100

# IPv6
ip6tables -t mangle -A PREROUTING -m set --match-set svpn_<group>_v6 dst -j MARK --set-mark <MARK> -m comment --comment svpn
ip6tables -t nat    -A POSTROUTING -m mark --mark <MARK> -o <vpn_dev> -j MASQUERADE -m comment --comment svpn
ip -6 rule add fwmark <MARK> table <ROUTE_TABLE> priority 100
```

**Critical: MASQUERADE rule is required.** Without NAT in POSTROUTING, packets exiting the VPN tunnel retain the LAN source IP, which the VPN endpoint will drop. The reference `add-vpn-iptables-rules.sh` includes this.

**Critical: `ip rule` deduplication.** Running `ip rule add` multiple times creates duplicate rules. The `Apply()` method must follow a **flush-and-readd pattern**: (1) delete all rules with `svpn` comment/matching fwmarks, (2) re-add from the current configuration. The iptables flush uses `-D` in a loop or a custom chain that can be flushed atomically.

### Custom iptables chain pattern (preferred over flush-by-comment)

Instead of adding rules directly to PREROUTING/POSTROUTING (which risks ordering issues and makes flushing fragile), create dedicated chains:

```
iptables  -t mangle -N SVPN_MARK 2>/dev/null || iptables  -t mangle -F SVPN_MARK
iptables  -t mangle -C PREROUTING -j SVPN_MARK 2>/dev/null || iptables  -t mangle -A PREROUTING -j SVPN_MARK
# ...add rules to SVPN_MARK...

iptables  -t nat -N SVPN_NAT 2>/dev/null || iptables  -t nat -F SVPN_NAT
iptables  -t nat -C POSTROUTING -j SVPN_NAT 2>/dev/null || iptables  -t nat -A POSTROUTING -j SVPN_NAT
# ...add MASQUERADE rules to SVPN_NAT...
```

Same for `ip6tables`. This makes `Apply()` idempotent: flush the custom chains, re-populate them, done.

### Files to create

| File | Purpose |
|---|---|
| `internal/routing/model.go` | `DomainGroup` struct: `ID`, `Name`, `EgressVPN`, `Domains []string`; persistence via SQLite (uses `internal/database`) |
| `internal/routing/store.go` | `Store`: `Create/Update/Delete/List/Get` for `DomainGroup` in SQLite |
| `internal/routing/ipset.go` | `IPSetManager`: `EnsureSet(name, family string) error`; `AddIP(set, ip string, timeoutSec int) error`; `FlushSet(name string) error`; `DestroySet(name string) error`; all calls use `exec.Command("ipset", ...)` |
| `internal/routing/dnsmasq.go` | `DetectDnsmasqConfDir() string`; `GenerateDnsmasqConf(groups []DomainGroup) string`; `WriteDnsmasqConf(content string) error`; `ReloadDnsmasq() error` (try `kill -HUP $(pidof dnsmasq)` first, fall back to `systemctl restart dnsmasq`) |
| `internal/routing/iptables.go` | `RuleManager`: `ApplyRules(groups []DomainGroup, vpns []VPNProfile) error` — creates/flushes custom chains `SVPN_MARK` (mangle) and `SVPN_NAT` (nat), adds jump rules, populates with per-group mark + MASQUERADE rules; manages `ip rule` / `ip -6 rule`; `FlushRules() error` — removes all this app's rules. **Both `iptables` and `ip6tables` for full IPv6 support.** |
| `internal/routing/manager.go` | `Manager`: orchestrates store, ipset, dnsmasq, iptables; `Apply() error` — called after any change: re-reads all groups and VPNs, calls ipset/dnsmasq/iptables atomically |
| `internal/routing/mock_ipset.go` | Mock for tests |
| `internal/routing/mock_exec.go` | Mock command executor for iptables/ip rule tests |
| `internal/routing/store_test.go` | Tests: CRUD, SQLite isolation using in-memory DB |
| `internal/routing/dnsmasq_test.go` | Tests: config generation for known groups, path detection logic |
| `internal/routing/iptables_test.go` | Tests: rule generation with mock executor, IPv4 and IPv6 rule parity, idempotency (apply twice → same result) |

### Files to modify

| File | Change |
|---|---|
| `internal/server/server.go` | Add `routing.Manager` injection |
| `internal/server/handlers_routing.go` | New file: REST handlers: `GET /api/groups`, `POST /api/groups`, `GET /api/groups/{id}`, `PUT /api/groups/{id}`, `DELETE /api/groups/{id}`; each mutating call triggers `routing.Manager.Apply()` |
| `cmd/splitvpnwebui/main.go` | Instantiate `routing.Manager`; call `Apply()` on startup to restore rules after reboot |

### Deliverables / Definition of Done

- [ ] `POST /api/groups` creates a group with domains and egress VPN; `Apply()` runs.
- [ ] After `Apply()`, ipsets `svpn_<group>_v4` and `svpn_<group>_v6` exist in the kernel.
- [ ] After `Apply()`, dnsmasq config exists at the detected path with correct `ipset=` lines.
- [ ] After `Apply()`, iptables **and** ip6tables rules route matched packets to the correct VPN fwmark.
- [ ] After `Apply()`, MASQUERADE/SNAT rules exist in the nat POSTROUTING chain for each VPN.
- [ ] After `Apply()`, `ip rule` and `ip -6 rule` entries route fwmark traffic to the correct table.
- [ ] `Apply()` is idempotent — running it twice does not duplicate rules, sets, or ip rules.
- [ ] On startup, `Apply()` restores all rules (ipsets, iptables chains, and ip rules are recreated after reboot).
- [ ] dnsmasq is reloaded via `kill -HUP` (graceful, no DNS downtime) with `systemctl restart` as fallback.
- [ ] All group CRUD endpoints return correct JSON.
- [ ] All tests pass (using mocks for ipset/iptables/dnsmasq system calls).

---

## Sprint 5 — DNS Pre-Warm

**Goal:** A background worker that pre-fetches DNS for all configured domains via each VPN interface's DoH endpoint, populates ipsets, and reports live progress via SSE.

### Behavior note: per-interface vs per-egress

The reference `90-ipset-prewarm.sh` queries each domain through **ALL** VPN interfaces (not just the domain group's assigned egress VPN). This is intentional — it pre-warms DNS caches on every VPN interface because different VPN endpoints may resolve domains to different CDN nodes. This app should replicate this behavior: for each domain, query DoH through every active VPN interface, collecting all unique IPs.

### `SO_BINDTODEVICE` and platform considerations

Binding HTTP requests to specific interfaces via `SO_BINDTODEVICE` requires:
- `CAP_NET_RAW` capability (already in the systemd unit).
- Linux only — does not work on macOS/Windows.
- The DoH client must use a `net.Dialer` with a `Control` function that calls `syscall.SetsockoptString(fd, syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)`.

For development/testing on macOS, the DoH client interface binding must be behind an interface/mock. Tests must use `httptest.Server` and mock the dialer, never attempting real `SO_BINDTODEVICE` calls.

### Files to create

| File | Purpose |
|---|---|
| `internal/prewarm/worker.go` | `Worker`: goroutine pool (configurable size); `Run(ctx context.Context) error` — iterates all domain groups, queries DoH for each domain via **every active VPN interface** (not just the egress VPN), inserts all resolved IPs into the group's ipsets; emits `Progress` events |
| `internal/prewarm/doh.go` | `DoHClient` interface + `CloudflareDoHClient` implementation: `QueryA(ctx, domain, iface string) ([]string, error)`; `QueryAAAA(ctx, domain, iface string) ([]string, error)`; `QueryCNAME(ctx, domain, iface string) ([]string, error)`; binds HTTP client to a specific network interface using `net.Dialer` with `Control` func setting `SO_BINDTODEVICE` (Linux only; no-op on other platforms for dev) |
| `internal/prewarm/progress.go` | `Progress` struct: `TotalDomains`, `ProcessedDomains`, `TotalIPs`, `PerVPN map[string]VPNProgress`; serialisable to JSON for SSE |
| `internal/prewarm/scheduler.go` | `Scheduler`: wraps `Worker`; runs on configurable interval (duration stored in settings); `Start()`, `Stop()`, `TriggerNow() error`; stores last run metadata to SQLite (`prewarm_runs` table) |
| `internal/prewarm/store.go` | SQLite persistence: `SaveRun(run PrewarmRun) error`; `LastRun() (*PrewarmRun, error)` |
| `internal/prewarm/doh_test.go` | Tests with `httptest.Server` mocking Cloudflare DoH responses: A records, AAAA records, CNAME chaining (one level), failure cases, timeout handling |
| `internal/prewarm/worker_test.go` | Tests with mock DoH client and mock ipset manager |

### Files to modify

| File | Change |
|---|---|
| `internal/server/server.go` | Inject `prewarm.Scheduler` |
| `internal/server/handlers_prewarm.go` | New file: `POST /api/prewarm/run` (trigger immediately); `GET /api/prewarm/status` (returns last run + current progress); push prewarm progress events into SSE stream |
| `cmd/splitvpnwebui/main.go` | Instantiate and start `prewarm.Scheduler` |

**Note:** `prewarm_runs` table already exists in `internal/database/schema.go` (created in Sprint 1).

### Deliverables / Definition of Done

- [ ] `POST /api/prewarm/run` triggers a pre-warm and returns immediately (runs in background).
- [ ] `GET /api/prewarm/status` returns `{ "running": bool, "lastRun": {...}, "progress": {...} }`.
- [ ] SSE stream emits `event: prewarm` messages with `Progress` JSON during a run.
- [ ] CNAME chaining: if a DoH response contains type=5 (CNAME) answers, the CNAME target is queried one level deep for A/AAAA before collecting IPs.
- [ ] IPs are inserted with `ipset add svpn_<group>_v4 <ip> timeout 43200 -exist` (both v4 and v6).
- [ ] Each domain is queried through every active VPN interface (matching reference behavior), not just the egress VPN.
- [ ] DoH interface binding uses `SO_BINDTODEVICE` on Linux, no-op on non-Linux for development.
- [ ] Worker respects context cancellation (stops cleanly when app shuts down).
- [ ] All tests pass (using mock DoH client — no real network calls in tests).

---

## Sprint 6 — Web UI: VPN Management

**Goal:** Replace the read-only VPN table with a full management UI. Users can add, edit, delete, start, stop, and restart VPNs entirely in the browser.

### Files to modify

| File | Change |
|---|---|
| `ui/web/static/js/app.js` | Set `controlsEnabled = true`; implement `openAddVPNModal()`, `openEditVPNModal(name)`, `deleteVPN(name)`, `startVPN(name)`, `stopVPN(name)`, `restartVPN(name)`; wire up file upload → populate textarea; wire all buttons |
| `ui/web/templates/layout.html` | Add "Add VPN" button to VPN table header; add Add/Edit VPN modal with: VPN type selector (WireGuard / OpenVPN), name field, file upload input, large textarea (pre-filled on edit), save/cancel buttons; add delete confirmation modal; enable Start/Stop/Restart buttons in VPN rows |
| `ui/web/static/css/app.css` | Style the VPN editor modal (monospace textarea, full-width, min-height 300px) |

### Deliverables / Definition of Done

- [ ] "Add VPN" button opens modal; user can paste or upload a `.wg` or `.ovpn` file.
- [ ] File upload populates the textarea; user can further edit the content before saving.
- [ ] Saving a new VPN calls `POST /api/vpns` and shows success/error notification.
- [ ] Edit VPN opens modal pre-filled with current config content; saving calls `PUT /api/vpns/{name}`.
- [ ] Delete VPN shows confirmation dialog; confirmed deletion calls `DELETE /api/vpns/{name}`.
- [ ] Start/Stop/Restart buttons call the correct API endpoints and update status in real-time via SSE.
- [ ] Autostart toggle is functional (calls `/api/configs/{name}/autostart`).
- [ ] All actions show appropriate success/error notifications.
- [ ] No regressions in monitoring display (charts, stats, latency all still work).

---

## Sprint 7 — Web UI: Domain Routing

**Goal:** Full domain group management in the browser. Users can create groups, add domains, and assign egress VPNs without any SSH.

### Files to modify

| File | Change |
|---|---|
| `ui/web/templates/layout.html` | Add "Domain Routing" section/tab; domain groups list card; "Add Group" button; per-group card showing: group name, egress VPN badge, domain count, Edit/Delete buttons; Add/Edit group modal: name field, egress VPN dropdown, domains textarea (one per line) |
| `ui/web/static/js/app.js` | `loadDomainGroups()`, `renderDomainGroups(groups)`, `openAddGroupModal()`, `openEditGroupModal(id)`, `deleteGroup(id)`, `saveGroup()` — all wired to REST API |
| `ui/web/static/css/app.css` | Domain group card styles; domain badge count styling |

### Deliverables / Definition of Done

- [ ] Domain groups section visible in UI, loads existing groups on page load.
- [ ] "Add Group" modal: name, egress VPN (populated from live VPN list), domains textarea.
- [ ] Saving a group calls `POST /api/groups` and the group appears in the list immediately.
- [ ] Edit group pre-populates modal, saving calls `PUT /api/groups/{id}`.
- [ ] Delete shows confirmation, calls `DELETE /api/groups/{id}`.
- [ ] After any save/delete, routing rules are re-applied on the backend (confirmed via routing manager).
- [ ] Egress VPN dropdown is populated from the live VPN list.

---

## Sprint 8 — Web UI: Pre-Warm, Auth & Settings

**Goal:** Pre-warm control panel with live progress, and a complete settings page with password change and API token management.

**Note:** `login.html` already exists (created in Sprint 1). Login flow is already functional.

### Files to modify

| File | Change |
|---|---|
| `ui/web/templates/layout.html` | Add "Pre-Warm" section: last run info (timestamp, duration, domains processed, IPs inserted), "Run Now" button, live progress bar (appears during run), schedule configuration input; add "Auth" section to settings modal: "Change Password" field + button, "API Token" display with "Regenerate" button |
| `ui/web/static/js/app.js` | `loadPrewarmStatus()`, `triggerPrewarm()`, `renderPrewarmProgress(event)` (handles `event: prewarm` SSE messages); settings modal: `changePassword()`, `regenerateToken()`, `copyToken()` |
| `internal/server/handlers_auth.go` | Add `POST /api/auth/password` (change password — requires current password + new password); add `POST /api/auth/token` (regenerate token); add `GET /api/auth/token` (get current token for display in settings) |
| `internal/server/server.go` | Wire up the new auth API routes in `Router()` |

### Deliverables / Definition of Done

- [ ] "Run Now" triggers pre-warm; live progress bar updates via SSE `prewarm` events.
- [ ] After run completes, last run stats (timestamp, duration, IPs inserted) are displayed.
- [ ] Schedule field allows setting run interval; saved to settings.
- [ ] Settings modal "Change Password" works: requires current password, sets new one.
- [ ] Settings modal shows API token with a "Copy" button and a "Regenerate" button.
- [ ] `POST /api/auth/password` validates current password before accepting the change (prevents CSRF-style resets).
- [ ] `GET /api/auth/token` only returns the token to authenticated users (already behind auth middleware).

---

## Sprint 9 — Install Script & Hardening

**Goal:** Production-ready `install.sh` following the correct UniFi persistence pattern. Full input validation and file permission enforcement throughout.

### Files to modify/create

| File | Change |
|---|---|
| `install.sh` | Complete rewrite: (1) check `udm-boot` active; (2) detect arch; (3) download binary from GitHub Releases; (4) create `/data/split-vpn-webui/{logs,vpns,units}/`; (5) write binary to `/data/split-vpn-webui/split-vpn-webui`; (6) write `units/split-vpn-webui.service`; (7) write and chmod `/data/on_boot.d/10-split-vpn-webui.sh`; (8) run the boot hook immediately; (9) print URL |
| `deploy/split-vpn-webui.service` | Final correct unit: `ExecStart=/data/split-vpn-webui/split-vpn-webui`, no legacy paths |
| `deploy/split-vpn-webui.sh` | Rename to `deploy/on_boot_hook.sh`; contains the boot hook template (symlink logic + daemon-reload + service start) |
| `internal/server/server.go` | Audit all handlers: validate every URL parameter (name, id) against `^[a-zA-Z0-9_-]+$`; validate domain names in group requests; sanitise all file path construction (use `filepath.Join` + verify result stays within expected prefix) |
| `internal/vpn/manager.go` | Enforce `0600` on private key files; enforce `0700` on VPN subdirectories |
| All `exec.Command` callsites | Audit for any string concatenation or interpolation; confirm all use `[]string{...}` arg form |

### New tests

| File | Purpose |
|---|---|
| `internal/server/server_test.go` | Input validation tests: reject names with path traversal, reject overlong inputs, reject invalid domain names |
| `integration/integration_test.go` | `//go:build integration`; end-to-end test: start server, create VPN, start it, check systemctl status |

### Deliverables / Definition of Done

- [ ] `install.sh` runs successfully on a UDM SE with `udm-boot-2x` installed.
- [ ] After running `install.sh` and rebooting, the service comes back up automatically.
- [ ] After a simulated firmware update (wipe `/etc/systemd/system/`), the boot hook restores the service.
- [ ] No input accepted by any API endpoint can cause path traversal.
- [ ] All VPN private key files created with permission `0600`.
- [ ] All `exec.Command` calls verified to use slice args (grep audit passes).
- [ ] All tests pass: `go test ./...`.

---

## Sprint 10 — Persistent Stats, Build & CI

**Goal:** Stats history survives restarts (SQLite), cross-compilation for amd64 and arm64, GitHub Actions pipeline.

**Note:** The `stats_history` table already exists in `internal/database/schema.go` (created in Sprint 1). This sprint adds the Go code to read/write it and the retention cleanup.

### Files to modify

| File | Change |
|---|---|
| `internal/stats/stats.go` | Add `Persist(db *sql.DB) error` and `LoadHistory(db *sql.DB) error` — write/read `stats_history` table; called on shutdown and startup respectively |
| `internal/database/database.go` | Add `Cleanup(db *sql.DB) error` — deletes `stats_history` rows older than 7 days; called on startup |
| `cmd/splitvpnwebui/main.go` | On startup: call `stats.LoadHistory(db)` and `database.Cleanup(db)`. On graceful shutdown: call `stats.Persist(db)`. |
| `.github/workflows/build.yml` | Create CI workflow: `go test ./...`, then `GOOS=linux GOARCH=amd64 go build`, `GOOS=linux GOARCH=arm64 go build`; on tag push: create GitHub Release with both binaries |
| `Makefile` | `make build-amd64`, `make build-arm64`, `make test`, `make install` targets |

### Deliverables / Definition of Done

- [ ] Restart the app; stats charts continue from where they left off (no blank restart).
- [ ] `GOOS=linux GOARCH=amd64 go build ./cmd/splitvpnwebui` succeeds (pure Go — `modernc.org/sqlite` has no CGo).
- [ ] `GOOS=linux GOARCH=arm64 go build ./cmd/splitvpnwebui` succeeds.
- [ ] `go test ./...` passes in CI.
- [ ] GitHub Actions workflow builds and uploads release binaries on tag push.
- [ ] `install.sh` downloads the correct binary for the detected architecture.
- [ ] Stats history table does not grow unbounded (rows older than 7 days are pruned on startup).

---

## Cross-Cutting Concerns & Audit Notes

> Added 2026-02-20 after a comprehensive codebase audit against CLAUDE.md and the reference implementation.

### Legacy config.go field mapping issues

`internal/config/config.go` reads the wrong keys from `vpn.conf`:
- Line 169: `cfg.VPNType = values["VPN_TYPE"]` — but reference vpn.conf files use `VPN_PROVIDER` (values: `"external"` for WireGuard, `"openvpn"` for OpenVPN). `VPN_TYPE` is never set.
- Line 170: `cfg.Gateway = values["VPN_GATEWAY"]` — but reference vpn.conf files use `VPN_ENDPOINT_IPV4`. `VPN_GATEWAY` is never set.

**Resolution:** Sprint 2's new `internal/vpn/` package replaces this with correct parsing. The old `config.Manager` will remain for legacy discovery but will eventually be replaced entirely. The new VPN manager reads `VPN_PROVIDER` and `VPN_ENDPOINT_IPV4`/`VPN_ENDPOINT_IPV6`.

### Concurrency safety

- The `vpn.Manager` allocator must use a mutex to prevent two concurrent `POST /api/vpns` requests from both allocating the same table ID or fwmark.
- The `routing.Manager.Apply()` must hold a mutex to prevent concurrent apply calls from interleaving ipset/iptables operations.
- The `prewarm.Scheduler` must prevent overlapping runs (if a manual trigger arrives while a scheduled run is in progress, reject with "already running").

### Atomic writes everywhere

All file writes must follow the atomic temp-file + rename pattern (already used by `settings.Save` and `config.WriteConfigFile`). This includes:
- VPN config files (`vpn.conf`, `.wg`, `.ovpn`)
- Systemd unit files
- dnsmasq config
- Boot hook script

### IPv6 parity checklist (CLAUDE.md: "full IPv4 and IPv6 support")

Every feature that operates on IPv4 must also handle IPv6:
- [ ] WireGuard `Address` field: parse both IPv4 and IPv6 CIDRs
- [ ] VPN endpoint: store and display both `VPN_ENDPOINT_IPV4` and `VPN_ENDPOINT_IPV6`
- [ ] Routing rules: `iptables` AND `ip6tables`; `ip rule` AND `ip -6 rule`
- [ ] ipsets: `hash:ip family inet` AND `hash:ip6 family inet6`
- [ ] DNS pre-warm: query both A (type 1) and AAAA (type 28) records
- [ ] Latency monitoring: handle both IPv4 and IPv6 ping targets (ping already handles both on most systems)
- [ ] UI: display both IPv4 and IPv6 addresses where applicable
