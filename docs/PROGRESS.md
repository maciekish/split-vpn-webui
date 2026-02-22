# split-vpn-webui — Project Progress

> **For agents:** Read this file at the start of every session. Update it before ending every session.
> The implementation plan is in `docs/IMPLEMENTATION_PLAN.md`.
> Project requirements are in `AGENTS.md`.

---

## Current Status

**Active sprint:** None (all planned sprints complete)
**Last updated:** 2026-02-22
**Last session summary:** Unified Throughput Share/WAN/VPN dashboard cards into one packed grid (no wasted space) and preserved user-entered selector/domain order across save+reload by removing sort-on-save/read behavior.
**Default working branch:** `main` (unless explicitly instructed otherwise)

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
| **9** — Install Script & Hardening | **Complete** | Installer + hardening + tests implemented |
| **10** — Persistent Stats, Build & CI | **Complete** | Stats persistence + cross-build + CI workflow implemented |
| **11** — Policy Routing Expansion | **Complete** | Rule-based selectors + resolver scheduler + UI/API + tests implemented |
| **12** — Interactive Uninstall Script | **Complete** | Interactive full-wipe + category uninstall flow implemented |
| **13** — Versioning & Update Management | **Complete** | Version metadata + release checksums + installer update prompts + webUI self-update orchestration |

---

## Known Issues / Blockers

- No known code blockers. A final on-device UDM acceptance run is still recommended for installer/boot-persistence behavior under real firmware update conditions.

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

### 2026-02-22 — Dashboard grid packing + selector order preservation
- Dashboard card layout now uses a single shared grid for throughput + interfaces:
  - `ui/web/templates/layout.html`: Throughput Share card moved into `#interface-grid`.
  - `ui/web/static/js/app-chart-helpers.js`: interface cards now use the same column class as Throughput Share and are ordered after it.
  - Effective order is now: Throughput Share first, then WAN, then VPN cards alphabetically (existing interface sort behavior).
  - This removes the previously wasted blank area under Throughput Share.
- Routing rule selector persistence now preserves user-entered order (instead of alphabetizing):
  - `internal/routing/model_normalize.go`: normalization keeps first-seen order for CIDRs/interfaces/MACs/ports/ASNs/domains while still deduping.
  - `internal/routing/model.go`: legacy-domain projection now preserves first-seen rule/domain order.
  - `internal/routing/store_rules_read.go`: selector/domain reads are ordered by insertion id.
  - `internal/routing/store_legacy.go`: legacy domain reads now use insertion id order (`id ASC`) instead of `domain ASC`.
  - Updated assertions in `internal/routing/model_test.go` and `internal/routing/store_test.go` for order-preserving behavior.
- Validation run:
  - `go test ./internal/routing -count=1`
  - `go test ./... -count=1`
  - `node --check ui/web/static/js/app-chart-helpers.js ui/web/static/js/app.js ui/web/static/js/domain-routing.js`

### 2026-02-22 — Hitless routing cutover hardening
- Implemented staged, low-blip routing refresh for resolver and policy applies:
  - Added atomic ipset set updates using staged sets + `ipset swap` in `internal/routing/manager_sets.go`.
  - `internal/routing/manager.go` now builds desired set state first, applies staged set swaps, then applies rule/dnsmasq updates.
  - Added resolver fast-path: `Manager.ReplaceResolverSnapshot` updates resolver cache and destination ipsets only, without rebuilding dnsmasq or iptables chains.
- Reworked iptables apply logic for safer cutover:
  - `internal/routing/iptables.go` now uses generation chains (`SVPN_MARK_A/B`, `SVPN_NAT_A/B`) and switches root jumps instead of flushing active chains.
  - Added legacy-chain migration handling to clear old root-chain inline rules once before switching to generation-chain mode.
  - Added additive-first managed `ip rule` reconciliation (add missing first, remove stale after) in `internal/routing/iptables_iprules.go`.
- Extended ipset interface for atomic swaps:
  - `SwapSets(setA, setB)` added to `internal/routing/ipset.go` and test mocks.
- Updated regression tests for new behavior:
  - Resolver run no longer requires a full rule re-apply (`internal/routing/resolver_test.go`).
  - Updated chain expectations for staged generation chains (`internal/routing/iptables_test.go`).
- Validation run:
  - `go test ./...`

### 2026-02-22 — Domain/wildcard overlap fix
- Fixed a routing persistence edge case where combining exact and wildcard selectors that collapse to the same legacy domain key (for example `domain.com` + `*.domain.com`) could trigger a SQLite unique-constraint error during save.
- `internal/routing/store.go`:
  - `replaceLegacyDomainsTx` now deduplicates canonicalized legacy domain rows after trimming wildcard prefixes.
  - This preserves modern rule semantics while keeping legacy mirror table writes collision-free.
- Added regression coverage:
  - `internal/routing/store_test.go` now verifies one rule can persist and reload both exact domains and wildcard domains together (including `asdf.domain.com` + `*.domain.com`).
- Validation run:
  - `go test ./internal/routing ./internal/server`
  - `go test ./...`

### 2026-02-22 — Repository secret/placeholder hygiene sweep
- Completed a full repository scan for obvious secret indicators and private-domain placeholders.
- Replaced legacy private placeholder domains with generic `contoso.com` examples across:
  - `AGENTS.md`
  - `docs/PROGRESS.md`
  - `ui/web/templates/layout.html`
  - `internal/server/server_test.go`
  - `internal/prewarm/worker_test.go`
  - `internal/vpn/manager_test.go`
  - `internal/vpn/wireguard_test.go`
  - `internal/vpn/testdata/wg0.reference.conf`
- Sanitized OpenVPN reference fixture/test content to remove realistic inline certificate/key/static-key material:
  - `internal/vpn/testdata/dreammachine.reference.ovpn`
  - `internal/vpn/openvpn_test.go`
- Validation run:
  - `go test ./...` (pass)

### 2026-02-22 — Branch policy update
- Updated branch policy to make `main` the default working branch unless explicitly directed otherwise.
- Updated files:
  - `AGENTS.md`
  - `docs/IMPLEMENTATION_PLAN.md`
  - `docs/PROGRESS.md`

### 2026-02-22 — Wildcard domain warning hardening
- Strengthened wildcard-rule warning copy in the policy rule editor:
  - `ui/web/static/js/domain-routing.js` now explicitly warns that broad top domains such as `*.microsoft.com` / `microsoft.com` can expand into very large discovered domain sets and create massive IPv4/IPv6 ipsets.
  - Warning style changed from `text-warning` to `text-danger` for higher visibility.
- Validation run:
  - `node --check ui/web/static/js/domain-routing.js`

### 2026-02-22 — Config backup/restore feature (post-sprint enhancement)
- Added new backup subsystem:
  - `internal/backup/` with versioned snapshot schema and manager orchestration.
  - Export captures source-style data for forward-compatible restore:
    - settings (including auth hash/token)
    - VPN source payloads (`name/type/config/configFile/interface/boundInterface`) plus supporting files as base64
    - autostart flags
    - routing groups/rules
    - resolver cache snapshot
  - Explicitly excludes traffic statistics/history tables.
- Added API endpoints:
  - `GET /api/backup/export` returns downloadable monolithic JSON backup file.
  - `POST /api/backup/import` accepts JSON body or multipart file upload (`file` field).
  - Import flow pauses resolver/prewarm schedulers, restores data, then resumes schedulers.
- Restore behavior:
  - Recreates VPNs through manager create/delete flows (API-style/source-data restore), not raw file replay.
  - Clears then restores routing state in controlled phases.
  - Restores autostart markers and settings.
  - Includes best-effort rollback to pre-import snapshot on failure.
- Routing manager/store enhancements for restore orchestration:
  - `routing.Manager.LoadResolverSnapshot`
  - `routing.Manager.ReplaceState`
  - `routing.Store.ReplaceAll` (atomic group + resolver cache replacement)
- Web UI additions (Settings modal):
  - "Download Full Backup" button.
  - Restore file picker + "Restore Backup" button with warning text.
  - Restore triggers reload to handle auth/session changes safely.
- Tests added:
  - `internal/backup/manager_test.go` (export fidelity, validation failure, import recreation flow).
- Validation run (all passed):
  - `go test ./...`

### 2026-02-22 — Wildcard prewarm + routing size visibility polish
- Wildcard prewarm behavior aligned with intended UX:
  - `internal/prewarm/worker.go` now treats wildcard tasks as discovery-enabled and resolves discovered subdomains before prewarming A/AAAA records.
  - Added dedicated wildcard discovery client for prewarm:
    - `internal/prewarm/wildcard.go` (`crt.sh` based discovery, normalization, dedupe, validation).
  - `internal/prewarm/scheduler.go` now injects wildcard resolver into worker runs.
  - Refactored task building utilities into `internal/prewarm/tasks.go` to keep worker file size under limit.
  - Added regression coverage:
    - `internal/prewarm/worker_test.go` `TestWorkerWildcardDiscoveryPrewarmsDiscoveredSubdomains`.
- Per-VPN routing size visibility added:
  - `internal/server/routing_sizes.go` computes current IP set entry counts from `ipset list` and aggregates per VPN (IPv4/IPv6 separately).
  - `internal/server/state.go` injects these counts into config status payloads.
  - `internal/server/server.go` `ConfigStatus` now includes `routingV4Size` and `routingV6Size`.
  - `ui/web/templates/layout.html` VPN table now includes `Routing Sets (v4/v6)` column.
  - `ui/web/static/js/app-vpn-helpers.js` renders per-VPN `v4 / v6` sizes.
  - Added parser tests:
    - `internal/server/routing_sizes_test.go`.
- Policy UI guidance text improved:
  - `ui/web/static/js/domain-routing.js` now shows a concise note:
    - normal domain entries route subdomains via dnsmasq matching but prewarm only runs on explicitly entered domains.
    - wildcard entries discover and prewarm known subdomains and include a caution about potentially large entry counts.
- Validation run (all passed):
  - `go test ./...`
  - `node --check ui/web/static/js/domain-routing.js ui/web/static/js/app-vpn-helpers.js ui/web/static/js/domain-routing-utils.js`

### 2026-02-22 — FORCED_SOURCE*/FORCED_DESTINATION parity completion
- Implemented remaining forced-selector coverage from peacey split-rule semantics (limited to forced source/destination scope):
  - Added routing rule selectors:
    - `sourceInterfaces` (maps to FORCED_SOURCE_INTERFACE behavior)
    - `sourceMacs` (maps to FORCED_SOURCE_MAC behavior)
  - Extended destination-port selector protocol support to include `both` (in addition to `tcp`/`udp`) to cover FORCED_SOURCE_*_PORT semantics.
  - Existing selectors already covered:
    - `sourceCidrs` (FORCED_SOURCE_IPV4/FORCED_SOURCE_IPV6)
    - `destinationCidrs` (FORCED_DESTINATIONS_IPV4/FORCED_DESTINATIONS_IPV6)
- Backend/runtime updates:
  - `internal/routing/model.go` now validates/normalizes source interface and source MAC selectors and accepts `both` protocol ports.
  - `internal/database/schema.go` adds selector persistence tables:
    - `routing_rule_source_interfaces`
    - `routing_rule_source_macs`
  - `internal/routing/store.go` persists/loads source interface and source MAC selectors.
  - `internal/routing/manager.go` carries new selectors into runtime bindings.
  - `internal/routing/iptables.go` now applies `-i <iface>` and `-m mac --mac-source <mac>` matches and expands `both` ports into tcp+udp rules for IPv4 and IPv6 chains.
- API/UI updates:
  - `internal/server/handlers_routing.go` accepts and returns `sourceInterfaces` + `sourceMacs`.
  - `ui/web/static/js/domain-routing.js` adds rule-editor fields for source interfaces/MACs and allows `both:<port>` syntax.
- Test coverage added/updated:
  - `internal/routing/model_test.go` (normalization/validation for new selectors)
  - `internal/routing/store_test.go` (selector persistence round-trip)
  - `internal/routing/manager_test.go` (binding propagation for interface/mac selectors)
  - `internal/routing/iptables_test.go` (source interface/mac command generation + `both` protocol expansion)
  - `internal/server/server_test.go` (group payload parsing for new selectors)
  - `internal/database/database_test.go` (new table existence checks)
- Validation run (all passed):
  - `go test ./...`
  - `node --check ui/web/static/js/domain-routing.js`
  - `bash -n install.sh uninstall.sh deploy/dev-deploy.sh deploy/dev-cleanup.sh deploy/dev-uninstall.sh deploy/on_boot_hook.sh`
- Release:
  - committed as `e088f51` (`Add forced source MAC/interface routing selectors and both-protocol ports`)
  - pushed to `main` and `claude-code`
  - tagged and published `v1.0.1` with release assets:
    - `split-vpn-webui-linux-amd64`
    - `split-vpn-webui-linux-arm64`
    - `SHA256SUMS`

### 2026-02-22 — CI verification after user-reported workflow failure
- Re-verified root cause and fix path:
  - `internal/prewarm/doh_test.go` parser/timeout tests now use empty interface binding (`""`) and no longer depend on a fake interface (`wg-a`) that fails on Linux with `SO_BINDTODEVICE`.
- Validation:
  - local `go test ./...` passed (including `internal/prewarm`).
  - GitHub Actions API shows latest tag-triggered `Build` run `22282694147` as `completed/success` for tag `v1.0.0` at commit `7c9890f714a9628112ad2416038ba3119a15f484`.

### 2026-02-22 — Release CI hotfix (prewarm DoH test interface binding)
- Root cause identified for failed tag workflow test stage:
  - `internal/prewarm/doh_test.go` used fake interface `wg-a`.
  - On Linux CI, `SO_BINDTODEVICE` was enforced and returned `no such device`.
- Fix applied:
  - parser and timeout tests now query with empty interface binding (`""`) so tests validate DoH parsing/timeout behavior without requiring a specific local interface.
- Validation:
  - `go test ./... -count=1` passed locally.

### 2026-02-22 — Release/publish follow-up (tag-only workflow trigger)
- Performed release branch operations:
  - fast-forward merged `claude-code` into `main`.
  - pushed `main` and `claude-code` over SSH remote transport.
  - created/pushed `v1.0.0` tag and validated remote refs point to expected commit.
- Investigated duplicate workflow fan-out on branch + tag pushes and applied trigger fix:
  - updated `.github/workflows/build.yml` to run only on `push.tags: v*`.
  - removed `pull_request` and `push.branches` triggers from that workflow so build/release pipeline does not execute on `main`/`claude-code` pushes.
  - committed fix (`Run build/release workflow on tags only`) and pushed to `main` and `claude-code`.
- Validation:
  - GitHub Actions run list shows no new branch-triggered runs after the trigger-only-tag commit.

### 2026-02-22 — Sprint 13 completion (version/update management)
- Added build metadata subsystem:
  - `internal/version/version.go` with runtime metadata accessors and JSON/human output.
  - `cmd/splitvpnwebui/main.go` now supports `--version` and `--version-json`.
- Added updater subsystem:
  - new `internal/update/` package with:
    - GitHub release metadata lookup (`latest`/tag),
    - binary/checksum asset selection,
    - SHA256 verification,
    - staged update job persistence,
    - updater status persistence (`update-status.json`) with file locking,
    - dedicated self-update runner (`--self-update-run`) for binary swap + restart + rollback attempt.
  - Added updater tests:
    - `internal/update/manager_test.go`
    - `internal/update/semver_test.go`
    - `internal/update/github_test.go`
- Added web API support:
  - `internal/server/handlers_update.go`:
    - `GET /api/update/status`
    - `POST /api/update/check`
    - `POST /api/update/apply`
  - `internal/server/server.go` route wiring + updater injection.
  - Added handler coverage in `internal/server/server_test.go` for update payload parsing/status-unavailable path.
- Added web UI update controls:
  - `ui/web/templates/layout.html` now includes software update section in settings modal.
  - `ui/web/static/js/app-updates.js` implements status rendering and check/apply actions.
  - `ui/web/static/js/app.js` integrates update controller in settings load flow.
- Installer improvements (`install.sh`):
  - now fetches release metadata first, resolves release tag, and detects existing installation.
  - prompts user before update/reinstall (with non-interactive fallback and `ASSUME_YES=1` override).
  - enforces checksum verification via release checksum asset before binary install.
  - prints installed target release tag.
- Release automation improvements:
  - `.github/workflows/build.yml` now embeds build metadata via ldflags,
    generates `SHA256SUMS`,
    uploads checksum with binaries,
    enables generated release notes,
    and attempts AI summary generation with safe fallback.
  - `.github/release.yml` added for generated-release-note categorization.
- Cleanup/uninstall coverage:
  - `uninstall.sh` now removes updater unit/symlink, updater status/job, and staged update directory.
  - `deploy/dev-uninstall.sh` iterative/complete modes now also remove updater unit artifacts.
- Validation run (all passed):
  - `go test ./...`
  - `go test ./internal/server ./internal/update`
  - `node --check ui/web/static/js/app-updates.js ui/web/static/js/app.js`
  - `bash -n install.sh uninstall.sh deploy/dev-uninstall.sh`

### 2026-02-22 — Version/update management research (tentative plan, no code changes)
- Audited current repository state:
  - `.github/workflows/build.yml` already builds on tag push (`v*`) and publishes Linux `amd64`/`arm64` binaries to GitHub Releases.
  - `install.sh` already resolves release assets from GitHub (`latest` by default, `VERSION=<tag>` override) and installs per-arch binaries.
- Collected current GitHub capability references for:
  - tag-triggered Actions workflows (`on.push.tags`),
  - release creation and auto-generated release notes (`generate_release_notes` / `gh release create --generate-notes`),
  - release-note customization (`.github/release.yml` categories/exclusions),
  - optional AI-generated release-note synthesis via GitHub Models from workflows (`permissions: models: read` with `GITHUB_TOKEN`).
- Prepared a phased tentative plan (pending approval) to add:
  - explicit semantic version metadata in binary/UI,
  - release-channel semantics (`latest` vs pinned tag),
  - in-webUI update checks + one-click update trigger using installer logic,
  - checksum/signature validation and optional attestations,
  - AI-assisted changelog draft generation with deterministic fallback to GitHub auto-notes.

### 2026-02-22 — Prewarm interface-alignment hardening (`wg-sv-*` support)
- Fixed false-negative VPN activity detection for prewarm and status paths:
  - `internal/util/network.go` now treats `operstate=unknown|dormant` as active when the interface has `IFF_UP`, which is common for WireGuard/tunnel interfaces.
  - `InterfaceOperState` now falls back to interface flags when `operstate` cannot be read, instead of returning an error-only path that suppressed interfaces in callers.
- Added prewarm fallback discovery path for managed interfaces:
  - `internal/prewarm/worker.go` `WorkerOptions` now supports `InterfaceList` injection and defaults to live interface listing.
  - when no active interfaces are found from VPN profiles, prewarm now falls back to active live interfaces matching the managed `wg-sv-*` namespace.
  - this allows prewarm to proceed when profile interface metadata is stale but managed WireGuard interfaces are actually up.
- Added regression tests:
  - `internal/prewarm/worker_test.go` adds `TestWorkerFallsBackToActiveManagedWireGuardInterfaces`.
  - `internal/util/network_test.go` adds `TestInterfaceStateConnected` for `up/unknown/dormant/down` behavior.
- Validation run (all passed):
  - `go test ./internal/prewarm ./internal/util -count=1`
  - `go test ./internal/server ./internal/vpn ./internal/config -count=1`
  - `go test ./... -count=1`
  - `go vet ./...`

### 2026-02-22 — systemd unit lifecycle hardening (autostart + self-heal)
- Root-cause fix for `Unit svpn-<name>.service not loaded` after autostart toggles:
  - `internal/server/handlers_config.go` `handleAutostart` now updates only the app autostart marker file and no longer calls `systemctl enable/disable` for VPN units.
  - This avoids disable-time removal of linked `/etc/systemd/system/svpn-*.service` unit links.
- Added self-healing of managed unit links before all service actions:
  - `internal/systemd/manager.go` now calls `ensureLinkedUnit` from `runSystemctl` before `start/stop/restart/enable/disable`.
  - If canonical unit file exists under `units/` but `/etc/systemd/system/<unit>.service` link is missing or stale, it re-creates the symlink and runs `systemctl daemon-reload` automatically.
  - This makes runtime actions resilient to link drift or partial cleanup.
- Added regression coverage:
  - `internal/systemd/manager_test.go` adds `TestSystemctlSelfHealsMissingSymlink`.
  - `internal/server/server_test.go` adds `TestHandleAutostartDoesNotCallSystemdEnableDisable`.
- Validation run (all passed):
  - `go test ./internal/systemd ./internal/server -count=1`
  - `go test ./internal/config ./internal/vpn -count=1`
  - `go test ./... -count=1`
  - `go vet ./...`

### 2026-02-22 — WireGuard interface alignment + stop-action usability
- Implemented managed WireGuard interface naming by profile name instead of config filename:
  - `internal/vpn/manager_helpers.go` now generates WireGuard interfaces in `wg-sv-*` format from the sanitized VPN profile name, with 15-char kernel limit enforcement and hash-suffix collision reduction for long names.
  - `internal/vpn/manager_helpers.go` now canonicalizes WireGuard config filenames to `<interface>.conf` so `wg-quick` always creates the intended managed interface regardless of uploaded filename.
  - `internal/vpn/manager_prepare.go` now resolves interface first, then WireGuard config filename from that interface, ensuring `DEV` and `CONFIG_FILE` stay in lockstep.
  - `internal/vpn/manager_storage.go` and `internal/config/config.go` now trust persisted `DEV` as the source of interface identity (no runtime inference from arbitrary config filenames).
- Fixed UI control behavior:
  - `ui/web/static/js/app-vpn-helpers.js` stop button is no longer disabled when UI thinks a VPN is disconnected; stop can always be issued.
- Added/updated regression tests:
  - `internal/vpn/manager_test.go` now validates WireGuard interface naming and canonical filename behavior (`wg-sv-*` and `<interface>.conf`) even when user-supplied config file names differ.
  - WireGuard interface-conflict tests now assert conflicts against the managed `wg-sv-*` naming model derived from profile names.
  - `internal/config/config_test.go` now validates discovery preserves configured WireGuard `DEV` (no filename-derived override).
- Validation run (all passed):
  - `go test ./internal/config ./internal/vpn -count=1`
  - `node --check ui/web/static/js/app-vpn-helpers.js`
  - `go test ./... -count=1`
  - `go vet ./...`

### 2026-02-21 — UniFi route-table collision completion (201.eth8 hardening)
- Finalized allocator behavior to prevent collisions with UniFi-owned policy-routing tables that appear as suffixed names (for example `lookup 201.eth8`):
  - allocator seeds used tables/marks from `ip rule` and `ip route show table all` (IPv4 + IPv6) and parses numeric prefixes from suffixed table tokens.
  - externally discovered allocations are tracked as sticky reservations and are not released on VPN profile deletion, preventing accidental reuse during delete/recreate iterations.
  - allocator now refreshes live route/rule reservations on every table/mark allocation and explicit reserve call, preventing stale startup snapshots from reusing a table claimed later by UniFi.
- Added regression coverage:
  - `internal/vpn/allocator_test.go` now includes `TestAllocatorReleaseDoesNotFreeStickyExternalReservations` to prove table/mark `201` stays unavailable even after `Release(201, 201)`.
  - `internal/vpn/allocator_test.go` now includes `TestAllocatorRefreshesLiveReservationsOnAllocation` to prove runtime-discovered `lookup 201.eth8` is honored even when it appears after allocator initialization.
- Validation run (all passed):
  - `go test ./internal/vpn -run Allocator -count=1`
  - `go test ./... -count=1`

### 2026-02-21 — First on-device runtime fixes (WireGuard start failures + UX error handling)
- Fixed WireGuard startup failures on systems without `resolvconf`:
  - `internal/vpn/manager_wireguard.go` now strips `DNS =` directives during sanitize when `resolvconf` is unavailable, and returns a warning in the create/update response.
  - Added tests in `internal/vpn/manager_wireguard_test.go` for legacy hook stripping and DNS removal behavior.
- Hardened route-table collision avoidance:
  - `internal/vpn/allocator.go` now also scans live route tables via `ip route show table all` and `ip -6 route show table all` so allocator avoids IDs already carrying routes even when absent from `ip rule`.
  - allocator parsing now recognizes suffixed table tokens (e.g. `201.eth8`) from `ip rule`/`ip route` outputs, preventing collisions with UniFi-named route tables.
  - Added coverage in `internal/vpn/allocator_test.go` (`TestAllocatorAvoidsCollisionsFromExistingRouteEntries`).
- Improved VPN action error handling path end-to-end:
  - `internal/server/handlers_config.go` now runs start/stop/restart synchronously and returns immediate JSON errors when `systemctl` fails (instead of fire-and-forget background calls).
  - `internal/systemd/manager.go` now captures `systemctl` command output on failures and includes it in returned error text.
  - Added regression coverage in `internal/systemd/manager_test.go` (`TestStartIncludesSystemctlOutputOnFailure`).
- Improved frontend error visibility:
  - `ui/web/static/js/app.js` now surfaces backend error updates even during transient status-lock windows.
  - `ui/web/static/js/domain-routing.js`, `ui/web/static/js/prewarm-auth.js`, and `ui/web/static/js/routing-resolver.js` now include HTTP response body fallback in fetch error handling.
  - `ui/web/static/js/app-vpn-helpers.js` action status text updated to match synchronous action completion (`Started/Stopped/Restarted`).
  - `ui/web/static/js/app-vpn-helpers.js` now surfaces backend create/update warnings returned in `vpn.warnings`.
- Validation run (all passed):
  - `go test ./...`
  - `go vet ./...`
  - `bash -n install.sh uninstall.sh deploy/on_boot_hook.sh deploy/dev-deploy.sh deploy/dev-uninstall.sh deploy/dev-cleanup.sh`
  - `node --check ui/web/static/js/app.js ui/web/static/js/app-chart-helpers.js ui/web/static/js/app-vpn-helpers.js ui/web/static/js/domain-routing.js ui/web/static/js/routing-resolver.js ui/web/static/js/prewarm-auth.js`

### 2026-02-21 — Pre-flight hardening review (gateway first-run readiness)
- Performed a full code-vs-`AGENTS.md`/`docs/IMPLEMENTATION_PLAN.md` review plus full verification rerun.
- Fixed first-run access mismatch:
  - `cmd/splitvpnwebui/main.go` now auto-binds to detected LAN IPv4 when running with default listen address (`127.0.0.1:8091`) and no explicit `listenInterface`.
  - Added LAN-detection helpers in `internal/util/network.go` (`DetectLANInterface`, `DetectLANIPv4`) with deterministic bridge-first candidate selection and private-IPv4 filtering.
  - Added LAN selector tests in `internal/util/network_test.go`.
- Fixed routing consistency after VPN profile changes:
  - `internal/server/handlers_vpn.go` now re-applies routing state (`routingManager.Apply`) after VPN create/update/delete to keep fwmark/table/interface rules synchronized immediately.
- Fixed API error-envelope consistency:
  - `internal/server/stream.go` now returns JSON error payloads for `/api/stream` startup failures instead of plain text `http.Error`.
- Validation run (all passed):
  - `go test ./...`
  - `go vet ./...`
  - `bash -n install.sh uninstall.sh deploy/on_boot_hook.sh deploy/dev-deploy.sh deploy/dev-uninstall.sh deploy/dev-cleanup.sh`
  - `node --check ui/web/static/js/app.js ui/web/static/js/app-chart-helpers.js ui/web/static/js/app-vpn-helpers.js ui/web/static/js/domain-routing.js ui/web/static/js/routing-resolver.js ui/web/static/js/prewarm-auth.js`

### 2026-02-21 — Dev iteration deploy/cleanup scripts (no reboot flow)
- Added `deploy/dev-deploy.sh`:
  - targets `root@10.0.0.1` by default (override via `--host`/`--port`).
  - builds binary for remote architecture automatically (`amd64`/`arm64`) unless `--no-build`.
  - copies binary + canonical app unit via SCP into `/data/split-vpn-webui/`.
  - links `/etc/systemd/system/split-vpn-webui.service`, runs `systemctl daemon-reload`, and restarts only `split-vpn-webui.service` (unless `--no-restart`).
  - optional flags to also copy boot hook and uninstall script.
- Added `deploy/dev-cleanup.sh`:
  - now acts as a compatibility wrapper around `deploy/dev-uninstall.sh`.
- Added `deploy/dev-uninstall.sh` with two explicit modes:
  - `iterative` (default):
    - stops/disables `split-vpn-webui.service`.
    - removes app binary, app service unit/symlink, and boot hook (unless `--keep-boot-hook`).
    - intentionally keeps VPN profiles/units/config/stats/runtime routing for fast iteration.
  - `complete`:
    - first attempts `printf 'y\n' | /data/split-vpn-webui/uninstall.sh`.
    - if unavailable, runs fallback full cleanup:
      - stops/disables app + managed `svpn-*` units
      - removes canonical/systemd unit links
      - removes app data dir (`/data/split-vpn-webui`)
      - removes runtime routing artifacts in app namespace (`SVPN_*` chains, `svpn_*` ipsets, app dnsmasq drop-ins, app-marked ip rules).
    - no reboot performed.
- Added Makefile shortcuts:
  - `make dev-deploy`
  - `make dev-cleanup`
  - `make dev-uninstall`
  - with configurable `DEV_HOST` and `DEV_PORT`.
- Updated `README.md` with a “Fast Dev Deploy (no reboot)” section and iterative/complete uninstall examples.
- Validation run:
  - `bash -n deploy/dev-deploy.sh deploy/dev-cleanup.sh deploy/dev-uninstall.sh install.sh uninstall.sh deploy/on_boot_hook.sh`
  - `go test ./...`
  - all passed locally.

### 2026-02-21 — Omission closure follow-up (size-policy + modularization)
- Closed remaining file-size policy omissions:
  - `ui/web/static/js/app.js` reduced from 1247 lines to 425 lines.
  - `internal/routing/resolver.go` reduced from 535 lines to 448 lines.
  - Verified no source file exceeds ~500 lines under `cmd/`, `internal/`, or `ui/`.
- Frontend modularization:
  - Added `ui/web/static/js/app-vpn-helpers.js` to isolate VPN CRUD/table/modal logic.
  - Reworked `ui/web/static/js/app-chart-helpers.js` to own gauge + interface-card rendering/formatting logic.
  - Updated `ui/web/templates/layout.html` script load order:
    - `app-chart-helpers.js`
    - `app-vpn-helpers.js`
    - `app.js`
  - Updated `ui/web/static/js/app.js` to act as the lean orchestration layer (SSE/update loop/settings) and delegate VPN/chart responsibilities to helper modules.
- Resolver split:
  - Added `internal/routing/resolver_types.go` for resolver provider/status/progress/run type definitions and shared clone helpers.
  - Kept runtime behavior unchanged; only structural split for maintainability and policy compliance.
- Validation run:
  - `gofmt -w internal/routing/resolver.go internal/routing/resolver_types.go`
  - `node --check ui/web/static/js/app-chart-helpers.js ui/web/static/js/app-vpn-helpers.js ui/web/static/js/app.js ui/web/static/js/domain-routing.js ui/web/static/js/routing-resolver.js ui/web/static/js/prewarm-auth.js`
  - `go test ./...`
  - line-count audit across `cmd/`, `internal/`, `ui/`
  - all passed locally.

### 2026-02-21 — Post-sprint remediation session (review findings closure)
- Fixed resolver timeout propagation bug:
  - `internal/routing/resolver.go` now rebuilds default domain/ASN/wildcard resolvers per run from current settings so timeout changes apply immediately (no process restart required).
  - test-injected custom resolvers remain stable via explicit custom flags.
- Fixed uninstall orphan symlink cleanup:
  - `uninstall.sh` now removes orphan `/etc/systemd/system/svpn-*.service` symlinks even when canonical unit files under `units/` are already missing.
  - stops/disables corresponding units and triggers `daemon-reload` when needed.
- Hardened VPN uniqueness/clash logic:
  - Added `internal/vpn/manager_conflicts.go`.
  - `vpn.Manager` now rejects interface collisions against:
    - other app-managed VPNs
    - live system interfaces
    - existing `wg-quick@*.service` unit-reserved interfaces
    - peacey profiles discovered in `/data/split-vpn/*/vpn.conf`
  - `vpn.Manager` now rejects route table / fwmark collisions with peacey profiles.
  - Allocator now supports additional config-root scanning and `NewManager` seeds allocator with `/data/split-vpn` to avoid route-table/fwmark allocation clashes with peacey profiles.
- Added/updated tests:
  - `internal/vpn/manager_test.go`:
    - system interface conflict rejection
    - allowed self-update when keeping existing interface
    - peacey interface conflict rejection
    - peacey route-table conflict rejection
  - `internal/vpn/allocator_test.go`:
    - additional config-root scan conflict coverage
- Documentation alignment:
  - Updated `README.md` to current implemented scope and paths (`/data/split-vpn-webui`, full VPN/routing management, resolver/prewarm/auth).
  - Updated `docs/IMPLEMENTATION_PLAN.md` checklist consistency (Sprint 1 completion, Sprint 9 validation wording, IPv6 parity checklist).
- Validation run:
  - `go test ./...`
  - `go vet ./...`
  - `bash -n install.sh uninstall.sh deploy/on_boot_hook.sh`
  - `printf 'n\nn\nn\nn\nn\n' | SKIP_ROOT_CHECK=1 bash uninstall.sh`
  - all passed locally.

### 2026-02-21 — Sprint 12 completion session
- Added `uninstall.sh` in repo root:
  - Interactive prompt flow implemented exactly as planned:
    - `Remove EVERYTHING related to split-vpn-webui? [y/N]`
    - if `No`, category prompts:
      - `Remove binaries? [y/N]`
      - `Remove VPNs + their systemd units? [y/N]`
      - `Remove config files? [y/N]`
      - `Remove statistics data? [y/N]`
  - Category cleanup behavior implemented:
    - binaries: stop/disable `split-vpn-webui.service`, remove app binary + canonical app unit + systemd symlink.
    - VPNs + units: stop/disable `svpn-*.service`, remove managed VPN unit files/symlinks + VPN profile directories.
    - config files: remove app settings and boot hook.
    - statistics data: remove `stats.db` and log files.
  - Runtime cleanup implemented for app namespace:
    - removes `SVPN_*` iptables/ip6tables chains and jump rules.
    - removes managed `ip rule` entries in the app policy namespace.
    - removes `svpn_*` ipsets.
    - removes app dnsmasq drop-in (`split-vpn-webui.conf`) and reload/restart dnsmasq.
  - Summary output implemented with explicit `Removed` and `Kept` sections.
  - Safety behavior:
    - default `No` for all prompts.
    - root check enforced by default.
    - scope restricted to split-vpn-webui namespace only.
- Updated installer deployment:
  - `install.sh` now downloads/deploys `/data/split-vpn-webui/uninstall.sh` and marks it executable.
  - installer output now prints uninstall script path.
- Updated docs:
  - `README.md` now includes uninstall usage + category behavior.
  - `docs/IMPLEMENTATION_PLAN.md` Sprint 12 checklist marked complete.
  - `docs/PROGRESS.md` sprint status updated to Sprint 12 complete.
- Validation run:
  - `bash -n install.sh uninstall.sh deploy/on_boot_hook.sh`
  - `SKIP_ROOT_CHECK=1 printf 'n\nn\nn\nn\nn\n' | bash uninstall.sh`
  - `go test ./...`
  - all passed locally.

### 2026-02-21 — Sprint 11 completion session
- Policy-routing model/storage expansion:
  - `internal/routing/model.go`:
    - Added rule-based group schema (`Rules []RoutingRule`) with selector support for:
      - source CIDR/IP
      - destination CIDR/IP
      - destination port ranges + protocol
      - destination ASN
      - exact domains
      - wildcard domains
    - Added strict normalization/validation and legacy payload compatibility (`domains` => rule conversion).
  - `internal/database/schema.go`:
    - Added new routing rule tables:
      - `routing_rules`
      - `routing_rule_source_cidrs`
      - `routing_rule_destination_cidrs`
      - `routing_rule_ports`
      - `routing_rule_asns`
      - `routing_rule_domains`
    - Added resolver persistence tables:
      - `resolver_cache`
      - `resolver_runs`
  - `internal/routing/store.go` + `internal/routing/store_legacy.go` + `internal/routing/store_resolver.go`:
    - Persist rule selectors and resolver snapshots/runs.
    - Added legacy migration read path from `domain_entries` (existing installs load as an implicit rule).
    - Added atomic resolver snapshot replace logic (stale-entry removal by replacement).
- Runtime routing engine updates:
  - `internal/routing/manager.go`:
    - Builds per-rule bindings (AND within rule, OR across rules) and applies atomically.
    - Creates per-rule source/destination ipsets and populates static + resolved selectors.
  - `internal/routing/ipset.go`:
    - Switched to `hash:net` sets for IPv4/IPv6 CIDR support.
    - `AddIP` now accepts IP or CIDR values.
  - `internal/routing/iptables.go`:
    - Added mangle rule generation for:
      - source-set match (`src`)
      - destination-set match (`dst`)
      - protocol + destination-port matching
    - Preserved IPv4/IPv6 parity and deterministic application order.
  - `internal/routing/dnsmasq.go`:
    - Generates per-rule dnsmasq `ipset=` entries for exact + wildcard domains mapped to rule destination sets.
- Resolver implementation and scheduling:
  - Added:
    - `internal/routing/resolver.go`
    - `internal/routing/resolver_helpers.go`
    - `internal/routing/resolver_domains.go` (Cloudflare DoH A/AAAA + one-level CNAME)
    - `internal/routing/resolver_asn.go` (public ASN prefix resolution via RIPE announced-prefixes API)
    - `internal/routing/resolver_wildcard.go` (public subdomain discovery via `crt.sh`)
  - Resolver scheduler capabilities:
    - periodic runs
    - manual trigger
    - live progress
    - persisted run history
    - stale selector cache replacement
    - automatic routing re-apply after successful snapshot refresh
- API/server wiring:
  - `internal/server/server.go`:
    - Injected resolver scheduler and SSE broadcast for `event: resolver`.
    - Added endpoints:
      - `POST /api/resolver/run`
      - `GET /api/resolver/status`
  - Added `internal/server/handlers_resolver.go`.
  - `internal/server/handlers_routing.go` now accepts rule-based group payloads.
  - `cmd/splitvpnwebui/main.go` now initializes/starts/stops resolver scheduler.
- Settings support:
  - `internal/settings/settings.go`:
    - Added resolver settings:
      - `resolverParallelism`
      - `resolverTimeoutSeconds`
      - `resolverIntervalSeconds`
  - `internal/server/handlers_settings.go`, `ui/web/static/js/app.js`, `ui/web/static/js/prewarm-auth.js` updated to preserve/read/write resolver settings.
- Frontend policy routing and resolver UI:
  - `ui/web/templates/layout.html`:
    - Expanded policy routing section with resolver status + progress UI.
    - Replaced domain-only modal with rule-builder modal.
  - `ui/web/static/js/domain-routing.js`:
    - Implemented rule editor CRUD (source/destination CIDRs, ports/protocols, ASNs, domains, wildcard domains).
  - Added `ui/web/static/js/routing-resolver.js`:
    - Manual resolver trigger
    - status/progress rendering
    - polling + SSE event handling
  - `ui/web/static/css/app.css` updated for rule-card styling.
- Tests added/updated:
  - `internal/routing/resolver_test.go`:
    - selector dedupe
    - scheduler run + snapshot update + routing re-apply
    - stale snapshot replacement behavior
  - `internal/routing/store_test.go`:
    - legacy `domain_entries` migration-read coverage
  - `internal/routing/iptables_test.go`:
    - source/destination + protocol/port rule generation coverage
  - `internal/database/database_test.go`:
    - schema presence checks updated for new Sprint 11 tables
- Validation run:
  - `go test ./...`
  - `node --check ui/web/static/js/domain-routing.js`
  - `node --check ui/web/static/js/routing-resolver.js`
  - `node --check ui/web/static/js/app.js`
  - `node --check ui/web/static/js/prewarm-auth.js`
  - all passed locally.

### 2026-02-21 — Sprint 10 completion session
- Stats history persistence implemented:
  - `internal/stats/persistence.go`:
    - Added `Persist(db *sql.DB) error` for writing in-memory history to `stats_history`.
    - Added `LoadHistory(db *sql.DB) error` for restoring history on startup.
    - Added deferred-history application path so history loaded before interface discovery is applied when interfaces are configured.
  - `internal/stats/stats.go`:
    - Added hidden byte counters to history datapoints for reliable persist/restore.
    - Added base-offset recovery logic after restore to keep counters continuous across restarts.
- Startup retention cleanup implemented:
  - `internal/database/database.go`:
    - Added `Cleanup(db *sql.DB) error` to prune `stats_history` rows older than 7 days.
- App lifecycle integration:
  - `cmd/splitvpnwebui/main.go`:
    - Calls `database.Cleanup(db)` on startup.
    - Calls `collector.LoadHistory(db)` after router/state initialization.
    - Calls `collector.Persist(db)` on graceful shutdown.
- Build/CI deliverables implemented:
  - Added `.github/workflows/build.yml`:
    - `go test ./...`
    - Linux amd64/arm64 builds
    - artifact upload
    - tag-triggered release asset publishing.
  - Added `Makefile` targets:
    - `make test`
    - `make build-amd64`
    - `make build-arm64`
    - `make build`
    - `make install`
- New tests:
  - `internal/stats/stats_persistence_test.go`:
    - persist/load round-trip
    - pending history application when interfaces are configured later.
  - `internal/database/database_test.go`:
    - retention cleanup removes only rows older than 7 days.
- Validation run:
  - `go test ./...`
  - `GOOS=linux GOARCH=amd64 go build ./cmd/splitvpnwebui`
  - `GOOS=linux GOARCH=arm64 go build ./cmd/splitvpnwebui`
  - `make test`
  - `make build`
  - All passed locally.

### 2026-02-21 — Uninstall spec/planning session
- Updated `AGENTS.md`:
  - Added `uninstall.sh` to repository layout.
  - Added explicit **Uninstallation** requirements:
    - first prompt asks whether to remove EVERYTHING
    - if not, prompt category-by-category for:
      - binaries
      - VPNs + their systemd units
      - config files
      - statistics data
    - default `No` prompts
    - final removed/kept summary
    - cleanup restricted to app namespace only
- Updated `docs/IMPLEMENTATION_PLAN.md`:
  - Added Sprint 12 in sprint overview.
  - Added full Sprint 12 section with:
    - required uninstall prompt flow
    - category scope rules
    - service/systemd cleanup requirements
    - deliverables checklist
- Updated progress tracking:
  - Added Sprint 12 as planned/not started.
  - Recorded uninstall flow as pending implementation scope.

### 2026-02-21 — Policy-routing spec expansion session
- Updated `AGENTS.md` requirements:
  - Replaced domain-only routing requirement with policy-based group/rule model supporting:
    - source IP/CIDR
    - destination IP/CIDR
    - destination port/range + protocol
    - destination ASN
    - exact domains
    - wildcard domains (`*.example.com`)
  - Added explicit rule semantics (AND inside rule, OR across rules in a group).
  - Added resolver requirements: periodic runtime refresh for domains, wildcard-discovered subdomains, and ASN prefixes.
  - Added explicit wildcard discovery requirement using public subdomain intelligence sources (CT-backed, `crt.sh` primary).
- Updated `docs/IMPLEMENTATION_PLAN.md`:
  - Added Sprint 11 to the sprint overview.
  - Added full Sprint 11 section with file-level implementation plan and definition-of-done checklist for policy routing + runtime resolver refresh.
- Progress status updated:
  - Sprint 10 remains active.
  - Sprint 11 added as planned/not started.

### 2026-02-21 — Sprint 9 completion session
- Installer overhaul in `install.sh`:
  - Added strict prerequisite check: `systemctl is-active --quiet udm-boot` must pass.
  - Added architecture detection for `amd64`/`arm64`.
  - Added GitHub Releases asset resolution and download for Linux arch-specific binary.
  - Migrated install targets to `/data/split-vpn-webui/` only:
    - binary at `/data/split-vpn-webui/split-vpn-webui`
    - canonical unit at `/data/split-vpn-webui/units/split-vpn-webui.service`
    - persistent boot hook at `/data/on_boot.d/10-split-vpn-webui.sh`
  - Boot hook runs immediately at install end to activate service in current boot session.
  - Installer now prints detected access URL.
- Deploy template change:
  - Renamed `deploy/split-vpn-webui.sh` to `deploy/on_boot_hook.sh` to match Sprint 9 plan.
- Server hardening:
  - Added centralized `{name}` URL param validation in `internal/server/helpers.go` via `vpn.ValidateName`.
  - Applied validation across all VPN/config name-parameter handlers in:
    - `internal/server/handlers_vpn.go`
    - `internal/server/handlers_config.go`
  - Added handler-level group payload validation/normalization in `internal/server/handlers_routing.go` using `routing.NormalizeAndValidate`.
- Config path sanitization:
  - Added base-path containment checks in `internal/config/config.go` so resolved config paths cannot escape configured VPN base directory (including symlink resolution).
- New tests:
  - `internal/server/server_test.go`:
    - reject traversal names
    - reject overlong names
    - reject invalid domains in group payloads
    - verify valid domain normalization
  - `internal/config/config_test.go`:
    - allow in-base config paths
    - reject escaping config directories
  - `integration/integration_test.go`:
    - added `//go:build integration` end-to-end lifecycle test scaffold (start server, create VPN, start VPN, verify systemd active).
- Validation run:
  - `bash -n install.sh deploy/on_boot_hook.sh`
  - `go test ./...`
  - All passing in current dev environment.

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
  - `split-vpn/sgp.contoso.com/wg0.conf`
  - `split-vpn/web.contoso.com/DreamMachine.ovpn`
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
