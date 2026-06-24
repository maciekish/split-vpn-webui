# split-vpn-webui — Agent Briefing

A standalone web UI for managing split-tunnel VPN on UniFi Dream Machine SE and compatible Debian-based UniFi gateways. Replaces shell-script-based [peacey/split-vpn](https://github.com/peacey/split-vpn) with a fully self-contained Go web application. Every feature is controllable through the UI — no SSH, no manual file editing. Full IPv4 and IPv6 support throughout.

This file should be read in conjunction with the user's CLAUDE.md (in `~/.claude/CLAUDE.md` locally, or account custom instructions on claude.ai). STOP and WARN if you are missing the 8-point custom instructions starting with "0. Non-negotiables"!

---

## Core Constraints (always apply without reading further)

- All persistent state lives under `/data/split-vpn-webui/` only. **Never write outside this tree** except transient symlinks in `/etc/systemd/system/` and the dnsmasq drop-in in `/run/dnsmasq.d/`.
- `/data/split-vpn/` is owned by peacey/split-vpn. **Never write there.**
- Namespace prefixes: systemd units `svpn-<name>.service`, ipsets `svpn_<group>_*`, data dir `split-vpn-webui`.
- No shell string interpolation. All `exec.Command` calls use explicit `[]string{...}` argument slices.
- Route table IDs and fwmarks allocated from 200 upward; never below 200.
- No source file exceeds ~500 lines. Split into subpackages before hitting the limit.
- Return `{"error": "..."}` JSON from all API endpoints on failure.

---

## Reading Before Acting

| Situation | File to read |
|---|---|
| Start of any session | `AGENTS/progress.md` |
| Working on platform, persistence, boot scripts, coexistence | `AGENTS/project-learnings/platform.md` |
| Working on VPN types, routing groups, resolver, prewarm | `AGENTS/project-learnings/requirements.md` |
| Architecture decisions, code quality, testing, security | `AGENTS/project-learnings/architecture.md` |
| Tech stack, which packages implement what | `AGENTS/project-learnings/tech-stack.md` |
| Reference configs, shell script patterns, vpn.conf format | `AGENTS/project-learnings/reference.md` |
| Sprint-level implementation detail | `AGENTS/implementation-plan.md` |

---

## 9. Self-improvement loop

This file is living. Keep it short by keeping it honest.

After every session where the agent did something wrong: was the mistake because this file lacks a rule, or because the agent ignored a rule? If lacking: add the rule under "Project Learnings" below, written as concretely as possible. If ignored: tighten or move the rule up. Every few weeks, prune — if removing a line would not cause a mistake, delete it.

Under 300 lines is the target ceiling.

---

## 10. Skills

Procedures to follow for specific user requests. Read the relevant file before acting.

- `AGENTS/skills/release.md` — **use this when asked to make a release / submit for review / cut a version**

---

## 11. Project Learnings

Detailed notes live in `AGENTS/project-learnings/`. Read the relevant file when working on that area.

- `platform.md` — target hardware, filesystem persistence model, boot scripts, coexistence namespaces
- `tech-stack.md` — language/library choices, currently implemented packages and their responsibilities
- `requirements.md` — feature requirements: VPN support, routing groups, resolver, prewarm, install, uninstall, auth
- `architecture.md` — package boundaries, code quality rules, testing requirements, security rules, routing application pattern
- `reference.md` — unifi-split-vpn reference scripts, policy routing shell patterns, vpn.conf format examples
- `regional-ip-lookup-research.md` — research on regional IP lookup approaches (self-hosted probes vs ECS vs datasets)
- `vpn-connection-inspector-plan.md` — plan for per-VPN active connection visibility feature
