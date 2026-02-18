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
| **2** | VPN provider abstraction | Parse/write WireGuard & OpenVPN configs; resource allocation |
| **3** | systemd manager | Write units, manage symlinks, start/stop/restart via API |
| **4** | Domain groups & routing | ipset/dnsmasq/iptables CRUD, REST API |
| **5** | DNS pre-warm | DoH worker, goroutine pool, scheduling, SSE progress |
| **6** | Web UI — VPN management | Full VPN CRUD + lifecycle controls in browser |
| **7** | Web UI — Domain routing | Domain group management in browser |
| **8** | Web UI — Pre-warm, auth & settings | Pre-warm dashboard, login page, settings page |
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

**Goal:** A clean, testable layer for reading, writing, and validating WireGuard and OpenVPN config files. Route table and fwmark allocator that guarantees no collision with UniFi or peacey/split-vpn.

### Files to create

| File | Purpose |
|---|---|
| `internal/vpn/provider.go` | `Provider` interface: `Type() string`, `ValidateConfig(raw string) error`, `ParseConfig(raw string) (*VPNProfile, error)`, `GenerateUnit(profile *VPNProfile, dataDir string) string` |
| `internal/vpn/profile.go` | `VPNProfile` struct: `Name`, `Type`, `RawConfig`, `RouteTable`, `FWMark`, `InterfaceName`, `Gateway`, `BoundInterface`; `VPNMeta` (vpn.conf key-value map) |
| `internal/vpn/wireguard.go` | `WireGuardProvider` implementing `Provider`; `ParseWGConfig` (INI-style parser for `[Interface]`/`[Peer]` blocks); `ValidateWGConfig`; `GenerateUnit` wrapping `wg-quick` |
| `internal/vpn/openvpn.go` | `OpenVPNProvider` implementing `Provider`; `ValidateOVPNConfig` (check for required directives: `client`, `remote`, `dev`); `GenerateUnit` wrapping `openvpn --config` |
| `internal/vpn/manager.go` | `Manager`: CRUD for VPN profiles in `/data/split-vpn-webui/vpns/<name>/`; writes `vpn.conf`, the raw VPN config file, and calls allocator |
| `internal/vpn/allocator.go` | `Allocator`: reads `/etc/iproute2/rt_tables` + `ip rule` output; maintains an in-memory set of used table IDs and marks; `AllocateTable() (int, error)`, `AllocateMark() (uint32, error)`, `Release(table int, mark uint32)` — starts allocation from 200 |
| `internal/vpn/wireguard_test.go` | Table-driven tests: parse valid config, parse invalid config, validate all required fields |
| `internal/vpn/openvpn_test.go` | Table-driven tests: validate required directives, reject invalid configs |
| `internal/vpn/allocator_test.go` | Tests: allocation starts at 200, no duplicates, collision with mocked existing tables |
| `internal/vpn/manager_test.go` | Tests: create/read/update/delete profile using temp dir |

### Files to modify

| File | Change |
|---|---|
| `internal/server/server.go` | Add `vpn.Manager` injection; add REST handlers: `POST /api/vpns` (create), `GET /api/vpns` (list), `GET /api/vpns/{name}` (get), `PUT /api/vpns/{name}` (update), `DELETE /api/vpns/{name}` (delete) |

### Deliverables / Definition of Done

- [ ] `wireguard.ParseWGConfig` correctly parses the sample `wg0.conf` from the reference repo.
- [ ] `openvpn.ValidateOVPNConfig` correctly validates the sample `DreamMachine.ovpn`.
- [ ] Allocator never issues table ID or fwmark below 200.
- [ ] Allocator reads live system tables and avoids conflicts.
- [ ] Full CRUD API for VPN profiles: create, list, get, update, delete — all returning JSON.
- [ ] Files stored with correct permissions: `vpn.conf` 0644, private key material 0600.
- [ ] All tests pass: `go test ./internal/vpn/...`.

---

## Sprint 3 — systemd Manager

**Goal:** The app can create, symlink, start, stop, restart, and remove systemd units for managed VPNs. The boot hook script is generated and written. Stubbed start/stop API endpoints are fully implemented.

### Files to create

| File | Purpose |
|---|---|
| `internal/systemd/manager.go` | `Manager`: `WriteUnit(name, content string) error` (writes to `units/`, creates symlink in `/etc/systemd/system/`, runs daemon-reload); `RemoveUnit(name string) error`; `Start/Stop/Restart/Enable/Status(name string)` — all call `exec.Command("systemctl", ...)` with explicit args |
| `internal/systemd/bootscript.go` | `WriteBootHook(dataDir string) error` — generates and writes `/data/on_boot.d/10-split-vpn-webui.sh` with correct symlink logic for this app's unit and all `svpn-*.service` units; sets executable bit |
| `internal/systemd/mock.go` | `MockManager` implementing same interface as `Manager` for tests |
| `internal/systemd/manager_test.go` | Tests using temp dirs: unit file written correctly, symlink created, boot script content correct |

### Files to modify

| File | Change |
|---|---|
| `internal/server/server.go` | Inject `systemd.Manager`; implement `handleStartVPN` (calls `systemd.Start("svpn-<name>")`); implement `handleStopVPN`; implement `handleAutostart` (enables/disables systemd unit + updates marker file); add `POST /api/vpns/{name}/restart` |
| `internal/vpn/manager.go` | On `Create`: call `systemd.WriteUnit` with generated unit content. On `Delete`: call `systemd.RemoveUnit`. |
| `cmd/splitvpnwebui/main.go` | Instantiate `systemd.Manager`; call `systemd.WriteBootHook` on startup if hook is missing or outdated |

### Deliverables / Definition of Done

- [ ] Creating a VPN via API writes the systemd unit to `/data/split-vpn-webui/units/svpn-<name>.service` and creates the symlink in `/etc/systemd/system/`.
- [ ] `POST /api/configs/{name}/start` starts the VPN via `systemctl start svpn-<name>`.
- [ ] `POST /api/configs/{name}/stop` stops it.
- [ ] `POST /api/configs/{name}/autostart` enables/disables the systemd unit.
- [ ] Deleting a VPN removes the unit file, removes the symlink, and runs `daemon-reload`.
- [ ] Boot hook written to `/data/on_boot.d/10-split-vpn-webui.sh` with correct content and `chmod +x`.
- [ ] No shell string interpolation anywhere — all `exec.Command` calls use string slices.
- [ ] All tests pass.

---

## Sprint 4 — Domain Groups & Routing

**Goal:** Users can create named domain groups, assign domains to them, and assign an egress VPN. The app applies the corresponding ipset, dnsmasq, and iptables rules atomically.

### Files to create

| File | Purpose |
|---|---|
| `internal/routing/model.go` | `DomainGroup` struct: `ID`, `Name`, `EgressVPN`, `Domains []string`; persistence via SQLite (uses `internal/database`) |
| `internal/routing/store.go` | `Store`: `Create/Update/Delete/List/Get` for `DomainGroup` in SQLite |
| `internal/routing/ipset.go` | `IPSetManager`: `EnsureSet(name, family string) error`; `AddIP(set, ip string, timeoutSec int) error`; `FlushSet(name string) error`; `DestroySet(name string) error`; all calls use `exec.Command("ipset", ...)` |
| `internal/routing/dnsmasq.go` | `GenerateDnsmasqConf(groups []DomainGroup) string`; `WriteDnsmasqConf(content string) error` (writes to `/run/dnsmasq.d/split-vpn-webui.conf`); `ReloadDnsmasq() error` |
| `internal/routing/iptables.go` | `RuleManager`: `ApplyRules(groups []DomainGroup, vpns []VPNProfile) error` — idempotent: flushes this app's rules (matched by comment `--comment svpn`) and re-applies; `FlushRules() error` |
| `internal/routing/manager.go` | `Manager`: orchestrates store, ipset, dnsmasq, iptables; `Apply() error` — called after any change: re-reads all groups and VPNs, calls ipset/dnsmasq/iptables atomically |
| `internal/routing/mock_ipset.go` | Mock for tests |
| `internal/routing/store_test.go` | Tests: CRUD, SQLite isolation using in-memory DB |
| `internal/routing/dnsmasq_test.go` | Tests: config generation for known groups |
| `internal/routing/iptables_test.go` | Tests: rule generation with mock executor |

### Files to modify

| File | Change |
|---|---|
| `internal/server/server.go` | Add `routing.Manager` injection; add REST handlers: `GET /api/groups`, `POST /api/groups`, `GET /api/groups/{id}`, `PUT /api/groups/{id}`, `DELETE /api/groups/{id}`; each mutating call triggers `routing.Manager.Apply()` |
| `cmd/splitvpnwebui/main.go` | Instantiate `routing.Manager`; call `Apply()` on startup to restore rules after reboot |

### Deliverables / Definition of Done

- [ ] `POST /api/groups` creates a group with domains and egress VPN; `Apply()` runs.
- [ ] After `Apply()`, ipsets `svpn_<group>_v4` and `svpn_<group>_v6` exist in the kernel.
- [ ] After `Apply()`, `/run/dnsmasq.d/split-vpn-webui.conf` exists with correct `ipset=` lines.
- [ ] After `Apply()`, iptables rules route matched packets to the correct VPN fwmark.
- [ ] `Apply()` is idempotent — running it twice does not duplicate rules or sets.
- [ ] On startup, `Apply()` restores all rules (ipsets and iptables rules are recreated after reboot).
- [ ] All group CRUD endpoints return correct JSON.
- [ ] All tests pass (using mocks for ipset/iptables/dnsmasq system calls).

---

## Sprint 5 — DNS Pre-Warm

**Goal:** A background worker that pre-fetches DNS for all configured domains via each VPN interface's DoH endpoint, populates ipsets, and reports live progress via SSE.

### Files to create

| File | Purpose |
|---|---|
| `internal/prewarm/worker.go` | `Worker`: goroutine pool (configurable size); `Run(ctx context.Context) error` — iterates all domain groups, queries DoH for each domain via the egress VPN's interface, inserts IPs into ipsets; emits `Progress` events |
| `internal/prewarm/doh.go` | `DoHClient`: `QueryA(ctx, domain, iface string) ([]string, error)`; `QueryAAAA(ctx, domain, iface string) ([]string, error)`; `QueryCNAME(ctx, domain, iface string) ([]string, error)`; binds HTTP client to a specific network interface using `net.Dialer` with `Control` func setting `SO_BINDTODEVICE` |
| `internal/prewarm/progress.go` | `Progress` struct: `TotalDomains`, `ProcessedDomains`, `TotalIPs`, `PerVPN map[string]VPNProgress`; serialisable to JSON for SSE |
| `internal/prewarm/scheduler.go` | `Scheduler`: wraps `Worker`; runs on configurable interval (duration stored in settings); `Start()`, `Stop()`, `TriggerNow() error`; stores last run metadata to SQLite (`prewarm_runs` table) |
| `internal/prewarm/store.go` | SQLite persistence: `SaveRun(run PrewarmRun) error`; `LastRun() (*PrewarmRun, error)` |
| `internal/prewarm/doh_test.go` | Tests with `httptest.Server` mocking Cloudflare DoH responses: A records, AAAA records, CNAME chaining, failure cases |
| `internal/prewarm/worker_test.go` | Tests with mock DoH client and mock ipset manager |

### Files to modify

| File | Change |
|---|---|
| `internal/server/server.go` | Inject `prewarm.Scheduler`; add `POST /api/prewarm/run` (trigger immediately); add `GET /api/prewarm/status` (returns last run + current progress); push prewarm progress events into SSE stream |
| `internal/database/schema.go` | Add `prewarm_runs` table to schema |
| `cmd/splitvpnwebui/main.go` | Instantiate and start `prewarm.Scheduler` |

### Deliverables / Definition of Done

- [ ] `POST /api/prewarm/run` triggers a pre-warm and returns immediately (runs in background).
- [ ] `GET /api/prewarm/status` returns `{ "running": bool, "lastRun": {...}, "progress": {...} }`.
- [ ] SSE stream emits `event: prewarm` messages with `Progress` JSON during a run.
- [ ] CNAME chaining: if a DoH A-record response has CNAME answers, the CNAME target is queried next.
- [ ] IPs are inserted with `ipset add svpn_<group>_v4 <ip> timeout 43200 -exist`.
- [ ] DoH queries use the correct VPN interface (verified in tests via mock dialer).
- [ ] Worker respects context cancellation (stops cleanly when app shuts down).
- [ ] All tests pass.

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

**Goal:** Pre-warm control panel with live progress, login page, and a complete settings page.

### Files to create

| File | Purpose |
|---|---|
| `ui/web/templates/login.html` | Bootstrap login page: password field, submit button, error message area. Submits to `POST /login`. |

### Files to modify

| File | Change |
|---|---|
| `ui/web/templates/layout.html` | Add "Pre-Warm" section: last run info (timestamp, duration, domains processed, IPs inserted), "Run Now" button, live progress bar (appears during run), schedule configuration input; add "Auth" section to settings modal: "Change Password" field + button, "API Token" display with "Regenerate" button |
| `ui/web/static/js/app.js` | `loadPrewarmStatus()`, `triggerPrewarm()`, `renderPrewarmProgress(event)` (handles `event: prewarm` SSE messages); settings modal: `changePassword()`, `regenerateToken()`, `copyToken()` |
| `internal/server/server.go` | Add `POST /api/auth/password` (change password); add `POST /api/auth/token` (regenerate token); add `GET /api/auth/token` (get current token, for display) |

### Deliverables / Definition of Done

- [ ] Unauthenticated users see the login page; successful login sets session cookie and redirects to dashboard.
- [ ] "Run Now" triggers pre-warm; live progress bar updates via SSE `prewarm` events.
- [ ] After run completes, last run stats (timestamp, duration, IPs inserted) are displayed.
- [ ] Schedule field allows setting run interval; saved to settings.
- [ ] Settings modal "Change Password" works: requires current password, sets new one.
- [ ] Settings modal shows API token with a "Copy" button and a "Regenerate" button.

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

### Files to modify

| File | Change |
|---|---|
| `internal/stats/stats.go` | Add `Persist(db *sql.DB) error` and `LoadHistory(db *sql.DB) error` — write/read `stats_history` table; called on shutdown and startup respectively |
| `internal/database/schema.go` | Add `stats_history` table: `(interface TEXT, timestamp INTEGER, rx_bytes INTEGER, tx_bytes INTEGER)` with index on `interface, timestamp`; add retention cleanup (delete rows older than 7 days) |
| `cmd/splitvpnwebui/main.go` | On startup: call `stats.LoadHistory(db)`. On graceful shutdown: call `stats.Persist(db)`. |
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
