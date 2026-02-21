# split-vpn-webui — Project Progress

> **For agents:** Read this file at the start of every session. Update it before ending every session.
> The implementation plan is in `docs/IMPLEMENTATION_PLAN.md`.
> Project requirements are in `AGENTS.md`.

---

## Current Status

**Active sprint:** Sprint 9 — Install Script & Hardening
**Last updated:** 2026-02-20
**Last session summary:** Sprint 8 completed end-to-end: added DNS pre-warm control panel with Run Now, live SSE progress, last-run stats, and schedule persistence; added authenticated auth APIs for password change and token management; and extended settings modal with change-password and API token copy/regenerate controls. Full test suite passing.

---

## Sprint Status

| Sprint | Status | Notes |
|---|---|---|
| **1** — Foundation Reset | **Complete** | All checklist items done; 10/10 tests pass |
| **2** — VPN Provider Abstraction | **Complete** | All Sprint 2 deliverables implemented and validated |
| **3** — systemd Manager | **Complete** | All Sprint 3 deliverables implemented and validated |
| **4** — Domain Groups & Routing | **Complete** | All Sprint 4 deliverables implemented and validated |
| **5** — DNS Pre-Warm | **Complete** | All Sprint 5 deliverables implemented and validated |
| **6** — Web UI: VPN Management | **Complete** | All Sprint 6 deliverables implemented and validated |
| **7** — Web UI: Domain Routing | **Complete** | All Sprint 7 deliverables implemented and validated |
| **8** — Web UI: Pre-Warm, Auth & Settings | **Complete** | All Sprint 8 deliverables implemented and validated |
| **9** — Install Script & Hardening | Not started | Active sprint |
| **10** — Persistent Stats, Build & CI | Not started | Blocked until Sprint 9 complete |

---

## Known Issues / Blockers

- Stats history is still in-memory only (Sprint 10 adds SQLite persistence).
- **`install.sh` still uses old paths** (`/mnt/data/`, `$SCRIPT_DIR/bin/`, writes unit directly to `/etc/systemd/system/`). Deferred to Sprint 9 for full rewrite.

---

## Sprint 1 — Completed Changes

### New files created
- `internal/database/database.go` — SQLite open + WAL + migrations
- `internal/database/schema.go` — Tables: `stats_history`, `domain_groups`, `domain_entries`, `prewarm_runs`
- `internal/database/database_test.go` — 3 tests (schema creation, idempotent migration, FK cascade)
- `internal/auth/auth.go` — Auth manager: password hash (bcrypt), API token, EnsureDefaults
- `internal/auth/middleware.go` — chi middleware: cookie + Bearer token validation, public path exemptions
- `internal/auth/auth_test.go` — 7 tests (defaults, idempotency, password check, token lifecycle)
- `ui/web/templates/login.html` — Bootstrap 5 dark theme login page

### Modified files
- `internal/settings/settings.go` — Added `AuthPasswordHash`, `AuthToken` fields; `NewManager` now takes full file path (not basePath)
- `internal/server/server.go` — Added `*auth.Manager`; restructured router with protected group; added login/logout handlers; fixed `handleWriteConfig` (no longer 501); `handleStartVPN`/`handleStopVPN`/`handleAutostart` now functional (legacy); settings API scrubs auth fields; SSE WriteTimeout set to 0; added `X-Accel-Buffering: no` header
- `cmd/splitvpnwebui/main.go` — Default data dir now `/data/split-vpn-webui`; creates data dir tree on startup; opens SQLite DB; initialises auth; config manager now scans `vpns/` subdirectory; `WriteTimeout: 0` for SSE
- `deploy/split-vpn-webui.service` — `ExecStart=/data/split-vpn-webui/split-vpn-webui --systemd`; correct capabilities; log redirect to `/data/split-vpn-webui/logs/`
- `deploy/split-vpn-webui.sh` — Rewritten as boot hook: symlinks unit files from `units/`, daemon-reload, enable + restart service

### Dependencies added (go.mod)
- `modernc.org/sqlite v1.46.1` (pure Go SQLite — no CGo, cross-compiles cleanly)
- `golang.org/x/crypto v0.48.0` (bcrypt for password hashing)

---

## Session Notes

### 2026-02-20 — Sprint 8 completion session
- Backend auth API expansion in `internal/server/handlers_auth.go`:
  - Added `POST /api/auth/password` with strict current-password validation before password change.
  - Added `GET /api/auth/token` for authenticated token retrieval.
  - Added `POST /api/auth/token` for token regeneration (with updated session cookie to keep UI logged in).
- Router integration in `internal/server/server.go`:
  - Added authenticated routes:
    - `GET /api/auth/token`
    - `POST /api/auth/token`
    - `POST /api/auth/password`
- UI additions in `ui/web/templates/layout.html`:
  - New DNS Pre-Warm section with:
    - last-run metadata panel
    - Run Now button
    - progress bar area
    - schedule (minutes) input + save button
  - Settings modal auth section:
    - current/new password fields + change button
    - API token display + copy/regenerate buttons
- New frontend module `ui/web/static/js/prewarm-auth.js`:
  - `loadPrewarmStatus()`, `triggerPrewarm()`, `renderPrewarmProgress()` via SSE `event: prewarm`.
  - Schedule persistence through `/api/settings` (`prewarmIntervalSeconds`).
  - Auth actions:
    - password change via `/api/auth/password`
    - token fetch via `/api/auth/token`
    - token regenerate via `/api/auth/token` (POST)
    - token copy helper.
- Styling updates in `ui/web/static/css/app.css` for prewarm progress and token display.
- Validation run:
  - `node --check ui/web/static/js/app.js`
  - `node --check ui/web/static/js/domain-routing.js`
  - `node --check ui/web/static/js/prewarm-auth.js`
  - `go test ./...`
  - All passed.

### 2026-02-20 — Sprint 7 completion session
- Added Domain Routing UI in `ui/web/templates/layout.html`:
  - New "Domain Routing" section with card-based group list.
  - "Add Group" button and add/edit modal containing:
    - Group name input
    - Egress VPN select
    - Domains textarea (one domain per line)
  - Delete confirmation modal for group removal.
- Added dedicated frontend module `ui/web/static/js/domain-routing.js`:
  - `loadDomainGroups()` via `GET /api/groups`
  - `loadVPNs()` via `GET /api/vpns` for egress dropdown population
  - Add/edit flow using:
    - `POST /api/groups`
    - `PUT /api/groups/{id}`
  - Delete flow using:
    - `DELETE /api/groups/{id}`
  - Inline section-level status messages for success/error outcomes.
  - Refresh integration: reloads groups + VPN list when "Reload" is clicked.
- Added styling in `ui/web/static/css/app.css` for domain group cards and domain badges.
- Wired new script in template:
  - Added `<script src="/static/js/domain-routing.js"></script>` after `app.js`.
- Deliverable alignment:
  - UI now exposes full group CRUD; backend `routing.Manager.Apply()` already runs on every save/delete from Sprint 4, so routing re-apply behavior is preserved.
- Validation run:
  - `node --check ui/web/static/js/domain-routing.js`
  - `node --check ui/web/static/js/app.js`
  - `go test ./...`
  - All passed.

### 2026-02-20 — Sprint 6 completion session
- Updated VPN management UI in `ui/web/templates/layout.html`:
  - Added "Add VPN" button in the VPN card header.
  - Replaced old read-only config modal with full Add/Edit VPN modal:
    - Type selector (WireGuard/OpenVPN)
    - Name field
    - Config file upload
    - Large editable config textarea
  - Added delete confirmation modal.
- Updated frontend behavior in `ui/web/static/js/app.js`:
  - Enabled controls and wired full action handlers:
    - `openAddVPNModal()`
    - `openEditVPNModal(name)` via `GET /api/vpns/{name}`
    - `deleteVPN(name)` via `DELETE /api/vpns/{name}`
    - `startVPN(name)` via `POST /api/configs/{name}/start`
    - `stopVPN(name)` via `POST /api/configs/{name}/stop`
    - `restartVPN(name)` via `POST /api/vpns/{name}/restart`
  - Added file upload → textarea population flow (with simple type auto-detection).
  - Updated table action buttons to Start/Stop/Restart/Edit/Delete and left autostart toggle active.
  - Improved API error handling in `fetchJSON()` to extract `{ "error": "..." }` envelopes.
  - Improved status messaging styling (success/error tone) and reduced conflicts with periodic SSE error refreshes.
  - Preserved prewarm settings fields during settings save to avoid accidental resets.
- Updated styles in `ui/web/static/css/app.css` for new VPN editor controls.
- Reference validation during this sprint rechecked local implementation references in:
  - `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/`
  - (for unit/service control semantics used by UI actions) existing systemd/start-stop paths already aligned with prior Sprint 3 implementation.
- Validation run: `node --check ui/web/static/js/app.js` and `go test ./...` passed.

### 2026-02-20 — Sprint 5 completion session
- Added new package `internal/prewarm/`:
  - `doh.go` + `bind_linux.go` + `bind_other.go` — DoH client with Cloudflare DNS JSON API support, Linux interface binding via `SO_BINDTODEVICE`, and non-Linux no-op binding for local development.
  - `worker.go` — cancellable goroutine worker pool that:
    - Reads domain groups.
    - Resolves via CNAME (one level), then A/AAAA.
    - Queries through **every active VPN interface**.
    - Inserts unique IPs into `svpn_*_v4`/`svpn_*_v6` ipsets with 12-hour timeout.
    - Emits live progress updates.
  - `scheduler.go` — periodic + on-demand run control (`Start`, `Stop`, `TriggerNow`, `Status`) with persisted run metadata and live progress callbacks.
  - `store.go` — SQLite persistence for `prewarm_runs` run history.
  - `progress.go` — run and per-interface progress models.
  - Tests:
    - `doh_test.go` — A/AAAA/CNAME parsing and timeout behavior using `httptest.Server`.
    - `worker_test.go` — active-interface multi-query behavior, CNAME handling, IP insertion, and cancellation.
- Settings model extended for prewarm runtime controls:
  - `prewarmParallelism`
  - `prewarmDoHTimeoutSeconds`
  - `prewarmIntervalSeconds`
- Server integration:
  - Added prewarm API handlers in `internal/server/handlers_prewarm.go`:
    - `POST /api/prewarm/run`
    - `GET /api/prewarm/status`
  - Updated SSE plumbing in `internal/server/stream.go` to support named events and emit `event: prewarm` messages.
  - Wired scheduler injection and route registration in `internal/server/server.go`.
- App startup integration:
  - `cmd/splitvpnwebui/main.go` now initializes, starts, and gracefully stops the prewarm scheduler.
- Reference validation completed against:
  - `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/on_boot.d/90-ipset-prewarm.sh`
  - `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/on_boot.d/91-ipset-prewarm-cron.sh`
  - `peacey/split-vpn` and `unifios-utilities` behavior notes already reflected in `docs/IMPLEMENTATION_PLAN.md`.
- Validation run: `go test ./...` passed.

### 2026-02-20 — Sprint 4 completion session
- Added new package `internal/routing/`:
  - `model.go` — domain group model, validation, deterministic ipset naming.
  - `store.go` — SQLite CRUD for `domain_groups` and `domain_entries`.
  - `ipset.go` — ipset command wrapper with ensure/add/flush/destroy/list.
  - `dnsmasq.go` — dnsmasq drop-in dir detection, config rendering/writing, graceful reload (`kill -HUP`) with systemctl fallback.
  - `iptables.go` — IPv4+IPv6 custom chains (`SVPN_MARK`, `SVPN_NAT`), MASQUERADE rules, and `ip rule`/`ip -6 rule` refresh+flush handling.
  - `executor.go`, `mock_exec.go`, `mock_ipset.go` — command and ipset mocks for tests.
  - `manager.go` — orchestration layer (`Apply`, `CreateGroup`, `UpdateGroup`, `DeleteGroup`) with apply mutex and startup-safe reconciliation.
  - Tests: `store_test.go`, `dnsmasq_test.go`, `iptables_test.go`, `manager_test.go`.
- Added routing HTTP handlers:
  - `internal/server/handlers_routing.go` — CRUD endpoints:
    - `GET /api/groups`
    - `POST /api/groups`
    - `GET /api/groups/{id}`
    - `PUT /api/groups/{id}`
    - `DELETE /api/groups/{id}`
  - Structured JSON error responses preserved as `{ "error": "..." }`.
- Wired routing manager through app:
  - `internal/server/server.go` now injects `*routing.Manager` and registers `/api/groups` routes.
  - `cmd/splitvpnwebui/main.go` now initializes routing manager and calls `Apply()` on startup to restore runtime state.
- Edge-case hardening completed:
  - `Manager.Apply()` now serializes concurrent calls with a mutex.
  - Reordered cleanup so stale ipsets are destroyed **after** rules are applied/flushed, avoiding “set in use” failures.
  - `CreateGroup`/`UpdateGroup` now validate the referenced egress VPN before persisting.
  - Added concurrency/order tests covering these paths.
- Validation run: `go test ./...` passed.

### 2026-02-20 — Sprint 3 completion session
- Added new package `internal/systemd/`:
  - `manager.go` — canonical unit write/remove, symlink management, `daemon-reload`, `Start/Stop/Restart/Enable/Disable/Status`.
  - `bootscript.go` — generated on-boot script writer for `/data/on_boot.d/10-split-vpn-webui.sh`.
  - `mock.go` — mock implementation for tests.
  - `manager_test.go` — tests for unit writes/removals, symlink behavior, boot hook generation, and command routing.
- Wired systemd manager into app startup:
  - `cmd/splitvpnwebui/main.go` now instantiates `systemd.NewManager(*dataDir)` and calls `WriteBootHook()` on startup.
- Wired systemd manager into VPN manager:
  - `internal/vpn/manager.go` now accepts `UnitManager` and writes/removes `svpn-<name>.service` units on create/update/delete.
  - Unit content is generated by provider-specific `GenerateUnit()` and stored in `/data/split-vpn-webui/units/`.
- Replaced legacy script-based runtime control in server:
  - `internal/server/state.go` no longer contains `runStartStopCommand` / `runCommand`.
  - `startVPN`/`stopVPN` now call systemd manager methods.
  - Added `restartVPN` helper.
- API updates:
  - Added route and handler for `POST /api/vpns/{name}/restart`.
  - Updated `POST /api/configs/{name}/autostart` to call `systemctl enable/disable` and persist marker state.
- Updated Sprint 3 checklist in `docs/IMPLEMENTATION_PLAN.md` to all completed.
- Validation run: `go test ./...` passed.

### 2026-02-20 — Sprint 2 completion session
- Completed remaining Sprint 2 deliverables.
- Added allocator implementation:
  - `internal/vpn/allocator.go`
  - Loads used table IDs from `/etc/iproute2/rt_tables`
  - Loads used fwmarks/tables from `ip rule` + `ip -6 rule`
  - Rebuilds allocation state from persisted `/vpns/*/vpn.conf`
  - Allocates route tables/fwmarks from `>= 200` and prevents collisions
- Added manager implementation split into focused files:
  - `internal/vpn/manager.go`
  - `internal/vpn/manager_prepare.go`
  - `internal/vpn/manager_storage.go`
  - `internal/vpn/manager_helpers.go`
  - `internal/vpn/manager_wireguard.go`
- Manager now supports full profile CRUD on disk:
  - `Create`, `List`, `Get`, `Update`, `Delete`
  - Name validation and type validation
  - WireGuard legacy peacey `updown.sh` hook stripping with warnings
  - Route `Table` preservation/allocation behavior
  - Permission enforcement: vpn dir `0700`, VPN config `0600`, `vpn.conf` `0644`
- Added VPN API handlers and routes:
  - `internal/server/handlers_vpn.go`
  - `GET /api/vpns`
  - `POST /api/vpns`
  - `GET /api/vpns/{name}`
  - `PUT /api/vpns/{name}`
  - `DELETE /api/vpns/{name}`
- Wired manager into runtime:
  - `cmd/splitvpnwebui/main.go` now initializes `vpn.Manager`
  - `internal/server/server.go` now injects and routes through `vpn.Manager`
- Fixed legacy vpn.conf key mapping in discovery:
  - `internal/config/config.go` now reads `VPN_PROVIDER` and `VPN_ENDPOINT_IPV4`/`VPN_ENDPOINT_IPV6` (with legacy fallback).
- Added/expanded tests:
  - `internal/vpn/allocator_test.go`
  - `internal/vpn/manager_test.go`
  - `internal/vpn/wireguard_test.go` now validates real reference sample from `internal/vpn/testdata/wg0.reference.conf`
  - `internal/vpn/openvpn_test.go` now validates real reference sample from `internal/vpn/testdata/dreammachine.reference.ovpn`
- Added reference fixtures:
  - `internal/vpn/testdata/wg0.reference.conf`
  - `internal/vpn/testdata/dreammachine.reference.ovpn`
- Compliance checks:
  - Confirmed no Go source file exceeds 500 lines.
  - Validation run: `go test ./...` passed.

### 2026-02-20 — Sprint 2 provider foundation session
- Researched local reference configs/scripts in `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/`:
  - `split-vpn/sgp.swic.name/wg0.conf`
  - `split-vpn/web.appulize.com/DreamMachine.ovpn`
  - `split-vpn/*/vpn.conf`
  - `on_boot.d/20-ipset.sh` and `on_boot.d/90-ipset-prewarm.sh`
- Added `internal/vpn/` package:
  - `provider.go`: `Provider` strategy interface
  - `profile.go`: `VPNProfile`, `WireGuardConfig`, `OpenVPNConfig` models
  - `validate.go`: `ValidateName` + `ValidateDomain`
  - `wireguard.go`: parser/validator + unit generator (`ParseWGConfig`, `ValidateWGConfig`)
  - `openvpn.go`: parser/validator + unit generator (`ValidateOVPNConfig`)
- Added tests:
  - `internal/vpn/wireguard_test.go`
  - `internal/vpn/openvpn_test.go`
  - `internal/vpn/validate_test.go`
- WireGuard parser now handles:
  - Whitespace-tolerant comma-separated `Address` values with mixed IPv4/IPv6
  - Multiple `[Peer]` sections
  - `Table` extraction into `VPNProfile.RouteTable`
  - Hook directives (`PostUp`, `PreDown`, `PostDown`) preservation
- OpenVPN parser now handles:
  - Required directive validation (`client`, `remote`, `dev`)
  - Inline block parsing (`<ca>`, `<cert>`, `<key>`, `<tls-crypt>`, etc.)
  - `dev tun` normalization to `tun0`
- Validation run: `go test ./...` passed.

### 2026-02-20 — Sprint 2 pre-requisite session
- Renamed local working branch from `calude-code` to `claude-code` to match AGENTS.md requirement.
- Split `internal/server/server.go` into:
  - `internal/server/server.go` (core types + router + background loop)
  - `internal/server/handlers_auth.go`
  - `internal/server/handlers_page.go`
  - `internal/server/handlers_config.go`
  - `internal/server/handlers_settings.go`
  - `internal/server/stream.go`
  - `internal/server/state.go`
  - `internal/server/helpers.go`
- Verified file-size limit compliance: all `internal/server/*.go` files are now <= 215 lines.
- Unified interface operstate probing:
  - Added `util.InterfaceOperState()` in `internal/util/network.go`
  - Updated `internal/server/state.go` and `internal/stats/stats.go` to use it
  - Removed duplicate `readInterfaceState()` implementation from `internal/stats/stats.go`
- Ran `go mod tidy`; `go.mod` now lists `golang.org/x/crypto` as a direct dependency.
- Validation run: `go test ./...` passed after all refactors.

### 2026-02-20 — Comprehensive audit session
- Full codebase audit against CLAUDE.md, reference implementation, and implementation plan.
- No code changes made — documentation only.
- Identified 29 issues across 5 categories (code↔spec sync, plan gaps, structural issues, edge cases, IPv6 parity).
- Updated `docs/IMPLEMENTATION_PLAN.md` with significant additions:
  - Sprint 2: Added `server.go` split plan (pre-requisite), VPN name validation (moved from Sprint 9), allocator reboot recovery, WireGuard parser edge cases (Address whitespace, Table directive, PostUp/PreDown handling, multiple peers), OpenVPN parser edge cases (inline blocks, separate credential files, dev directive), `go mod tidy` housekeeping, `interfaceState` dedup.
  - Sprint 3: Added detailed generated systemd unit templates for WireGuard and OpenVPN, documented key differences from reference (no peacey updown.sh, absolute paths, no sleep 30, --route-noexec for OVPN), added legacy code removal (runStartStopCommand), added restart endpoint.
  - Sprint 4: Added dnsmasq path detection (both `/run/dnsmasq.d/` and `/run/dnsmasq.dhcp.conf.d/`), MASQUERADE/SNAT rule requirement, `ip rule` deduplication via flush-and-readd pattern, custom iptables chain pattern (SVPN_MARK/SVPN_NAT), full IPv6 parity (ip6tables + ip -6 rule), graceful dnsmasq reload via `kill -HUP`.
  - Sprint 5: Added per-interface vs per-egress behavior note (reference queries ALL interfaces), SO_BINDTODEVICE platform considerations and mock strategy.
  - Sprint 8: Removed duplicate login.html creation (already exists from Sprint 1), added current-password requirement for password change endpoint.
  - Sprint 10: Noted stats_history table already exists, added Cleanup function reference.
  - Added new "Cross-Cutting Concerns" section: legacy config.go mapping issues, concurrency safety requirements, atomic writes pattern, IPv6 parity checklist.
- Updated `docs/PROGRESS.md` Known Issues with all newly identified issues.

### 2026-02-18 — Planning session
- Conducted full codebase audit.
- Researched unifios-utilities boot persistence for modern (2.x) firmware.
- Wrote AGENTS.md with all corrected paths and coexistence rules.
- Wrote `docs/IMPLEMENTATION_PLAN.md` with 10 sprints.
- No code changes made this session.

### 2026-02-18 — Sprint 1 session
- Implemented all Sprint 1 deliverables.
- Key decisions made during implementation:
  - `settings.NewManager` signature changed from `(basePath string)` to `(settingsPath string)` — takes full file path now, caller constructs it.
  - Session cookie value = API token (stateless — no in-memory session store needed; token regeneration invalidates all sessions).
  - Auth fields are scrubbed before returning settings via `/api/settings` — hash and token never sent to browser.
  - `WriteTimeout: 0` on HTTP server — required for SSE long-lived connections; the old 15s timeout would silently drop streams.
  - Config manager now scans `vpns/` subdirectory (not data root) to avoid scanning logs, units, etc.
  - `refreshState()` treats a missing/empty vpns/ dir as non-fatal (logs warning, continues with empty config list).
  - `handleSaveSettings` decodes only `listenInterface` and `wanInterface` — auth fields cannot be overwritten via settings API.
  - AGENTS.md updated with hard requirement: "Always consider any and all relevant edge cases".

---

## How to Resume

1. Read `AGENTS.md` for full project context and requirements.
2. Read `docs/IMPLEMENTATION_PLAN.md` for detailed sprint breakdown.
3. Check the **Sprint Status** table above for the active sprint.
4. Read the **Session Notes** for the most recent session to understand where things were left.
5. Run `go test ./...` to confirm baseline before starting work.
6. Start the active sprint. Work through its checklist in `IMPLEMENTATION_PLAN.md`.
7. Update this file before ending the session: update sprint status, add session notes.
