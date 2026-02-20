# split-vpn-webui — Project Progress

> **For agents:** Read this file at the start of every session. Update it before ending every session.
> The implementation plan is in `docs/IMPLEMENTATION_PLAN.md`.
> Project requirements are in `AGENTS.md`.

---

## Current Status

**Active sprint:** Sprint 2 — VPN Provider Abstraction
**Last updated:** 2026-02-18
**Last session summary:** Sprint 1 completed in full. All paths corrected, SQLite database package added, authentication system implemented, `handleWriteConfig` un-stubbed, `handleStartVPN`/`handleStopVPN`/`handleAutostart` partially implemented (legacy script-based, Sprint 3 will replace with systemd), deploy files rewritten. All 10 tests pass. Linux amd64 and arm64 cross-compilation confirmed.

---

## Sprint Status

| Sprint | Status | Notes |
|---|---|---|
| **1** — Foundation Reset | **Complete** | All checklist items done; 10/10 tests pass |
| **2** — VPN Provider Abstraction | Not started | Ready to begin |
| **3** — systemd Manager | Not started | Blocked until Sprint 2 complete |
| **4** — Domain Groups & Routing | Not started | Blocked until Sprint 3 complete |
| **5** — DNS Pre-Warm | Not started | Blocked until Sprint 4 complete |
| **6** — Web UI: VPN Management | Not started | Blocked until Sprint 3 complete |
| **7** — Web UI: Domain Routing | Not started | Blocked until Sprint 4 + 6 complete |
| **8** — Web UI: Pre-Warm, Auth & Settings | Not started | Blocked until Sprint 5 + 6 complete |
| **9** — Install Script & Hardening | Not started | Blocked until Sprint 8 complete |
| **10** — Persistent Stats, Build & CI | Not started | Blocked until Sprint 9 complete |

---

## Known Issues / Blockers

- `handleStartVPN` and `handleStopVPN` still use the legacy script-based approach (`run-vpn.sh`, `wg-quick`, `pkill`). Sprint 3 replaces this with proper systemd `svpn-<name>` unit management.
- `controlsEnabled = false` in `app.js` — UI control buttons still disabled. Sprint 6 enables them.
- Stats history is still in-memory only (Sprint 10 adds SQLite persistence).
- No VPN CRUD API yet (Sprint 2 adds the provider abstraction and manager; Sprint 3 wires up the API endpoints and systemd units).
- **`server.go` is 743 lines** — exceeds the 500-line limit. Must be split as first task of Sprint 2.
- **`config.go` reads wrong vpn.conf keys**: `VPN_TYPE` (should be `VPN_PROVIDER`), `VPN_GATEWAY` (should be `VPN_ENDPOINT_IPV4`). These fields are always empty from real configs. Sprint 2's new VPN manager uses correct keys.
- **`go.mod`**: `golang.org/x/crypto` listed as `// indirect` but is a direct import in `internal/auth`. Fix with `go mod tidy`.
- **`interfaceState()` duplicated** in both `server.go` and `stats.go` — should be unified into `internal/util/`.
- **`install.sh` still uses old paths** (`/mnt/data/`, `$SCRIPT_DIR/bin/`, writes unit directly to `/etc/systemd/system/`). Deferred to Sprint 9 for full rewrite.
- **No restart API endpoint** — Sprint 3 must add `POST /api/vpns/{name}/restart`.

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
