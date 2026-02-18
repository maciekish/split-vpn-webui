# split-vpn-webui — Project Progress

> **For agents:** Read this file at the start of every session. Update it before ending every session.
> The implementation plan is in `docs/IMPLEMENTATION_PLAN.md`.
> Project requirements are in `AGENTS.md`.

---

## Current Status

**Active sprint:** None — ready to begin Sprint 1
**Last updated:** 2026-02-18
**Last session summary:** Initial planning session. Codebase audited, AGENTS.md written and corrected, implementation plan created. No code changes made yet.

---

## Sprint Status

| Sprint | Status | Notes |
|---|---|---|
| **1** — Foundation Reset | Not started | |
| **2** — VPN Provider Abstraction | Not started | Blocked until Sprint 1 complete |
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

- All existing code still references `/mnt/data/split-vpn` (wrong path — must be `/data/split-vpn-webui`). Sprint 1 fixes this.
- `deploy/split-vpn-webui.service` has wrong `ExecStart` path. Sprint 1 fixes this.
- `install.sh` uses the wrong installation pattern entirely. Sprint 9 rewrites it.
- `handleStartVPN`, `handleStopVPN`, `handleAutostart`, `handleWriteConfig` all return 501. Sprint 1 partially fixes `handleWriteConfig`; Sprint 3 fixes start/stop/autostart.
- `controlsEnabled = false` in `app.js`. Sprint 6 enables it.
- No authentication. Sprint 1 adds it.
- No SQLite. Sprint 1 adds schema; Sprint 10 uses it for stats persistence.

---

## Session Notes

### 2026-02-18 — Planning session
- Conducted full codebase audit (see IMPLEMENTATION_PLAN.md baseline section for findings).
- Researched unifios-utilities boot persistence for modern (2.x) firmware.
- Key finding: `/data/` is the real persistent mount (NOT a symlink to `/mnt/data/`). `/etc/systemd/system/` is wiped on firmware updates. Boot hook must re-create symlinks on every boot.
- Wrote AGENTS.md with all corrected paths and coexistence rules.
- Wrote `docs/IMPLEMENTATION_PLAN.md` with 10 sprints.
- No code changes made this session.

---

## How to Resume

1. Read `AGENTS.md` for full project context and requirements.
2. Read `docs/IMPLEMENTATION_PLAN.md` for detailed sprint breakdown.
3. Check the **Sprint Status** table above for the active sprint.
4. Read the **Session Notes** for the most recent session to understand where things were left.
5. Run `go test ./...` to confirm baseline before starting work.
6. Start the active sprint. Work through its checklist in `IMPLEMENTATION_PLAN.md`.
7. Update this file before ending the session: update sprint status, add session notes.
