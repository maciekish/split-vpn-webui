# Architecture & Code Quality Guidelines

## Package Architecture

- **`internal/vpn/`** — `Provider` interface; WireGuard + OpenVPN implementations. Adding a new type = new file only.
- **`internal/config/`** — reads and validates config files. No other package writes files directly.
- **`internal/routing/`** — owns all ipset, iptables, and dnsmasq interactions. Idempotent: `Apply()` twice must not corrupt state.
- **`internal/routing/resolver*.go`** — runtime resolution of domains/ASNs/wildcard subdomains, periodic refresh, stale-entry reconciliation.
- **`internal/systemd/`** — all `systemctl` and unit-file interactions behind an interface for mockability.
- **`internal/prewarm/`** — goroutine pools; progress via channels consumed by the SSE stream.
- **`internal/server/`** — thin handlers only; all business logic in the above packages.

## Code Quality Rules

- No source file exceeds ~500 lines; split into subpackages/modules before hitting the limit.
- No `exec.Command` with shell interpolation — all calls use explicit `[]string{...}` argument slices.
- No shortcuts that create future pain (e.g., shelling out to `wg` for parsing when Go libraries exist).
- Return structured JSON errors from all API endpoints: `{"error": "..."}` envelope.
- All file writes use atomic temp-file + rename pattern (already used by `settings.Save`).
- All mutating operations that can race must hold a mutex: allocator, `routing.Manager.Apply()`, `prewarm.Scheduler`.

## Edge Cases (Always Consider)

Interface not found, ipset already exists, systemd unavailable, partial write failures, concurrent API requests, empty directories on first boot, malformed user input, disk full, permission errors, goroutine races, `SO_BINDTODEVICE` unavailable on non-Linux.

## Testing Requirements

- Unit tests in every package using mocks/interfaces for all kernel interactions.
- Unit tests must not require root or modify the host system.
- Integration tests tagged `//go:build integration` may require root; document how to run them.
- Every new feature ships with tests before being considered complete.
- Run `go test ./...` after every change to verify.

## Security Rules

- Web UI binds to detected LAN IPv4 by default (not `0.0.0.0`). LAN detection: `DetectLANInterface` / `DetectLANIPv4` in `internal/util/network.go`.
- No shell string interpolation with user-supplied data.
- VPN private keys and credentials stored with `0600` permissions; VPN directories `0700`.
- Validate all user input (domain names, interface names, file paths) before using in filesystem or subprocess calls.
- All API URL parameters validated against `^[a-zA-Z0-9_-]+$` minimum; no path traversal.

## VPN Name Validation

Pattern: `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`
- Rejected: path separators (`/`, `\`), parent traversal (`..`), systemd-reserved chars (`@`), whitespace, control characters
- Max 64 chars (keeps systemd unit under 256-char limit including `svpn-` prefix and `.service` suffix)
- Must be unique on disk

## Routing Application Pattern (Hitless Cutover)

1. Build desired ipset state in staged sets
2. Swap staged sets atomically via `ipset swap`
3. Use generation chains (`SVPN_MARK_A/B`, `SVPN_NAT_A/B`) — flush inactive generation, populate it, switch root jump
4. Reconcile `ip rule` / `ip -6 rule` additively (add missing first, remove stale after)
5. Rebuild dnsmasq and reload with `kill -HUP $(pidof dnsmasq)` (fallback: `systemctl restart dnsmasq`)

## dnsmasq Path Detection

The dnsmasq drop-in directory may be `/run/dnsmasq.d/` or `/run/dnsmasq.dhcp.conf.d/`. Probe both at startup; use whichever exists. If neither, create `/run/dnsmasq.d/`.

## WireGuard Interface Naming

Managed interfaces use `wg-sv-<sanitized-name>` format, capped at 15 chars (kernel limit). Long names get a hash suffix to reduce collision probability. The WireGuard config filename is canonicalized to `<interface>.conf` so `wg-quick` always creates the intended interface.

## Route Table / fwmark Allocation

- Allocate from 200 upward
- On startup, scan all persisted `vpn.conf` files to rebuild the in-memory used-set
- Scan `/etc/iproute2/rt_tables`, `ip rule`, and `ip route show table all` (IPv4 + IPv6) to discover externally allocated tables
- Parse suffixed table tokens (e.g. `201.eth8`) — track as sticky reservations never released
- Refresh live reservations on every allocation call (not just startup snapshot)
