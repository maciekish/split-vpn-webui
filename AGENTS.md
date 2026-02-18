# split-vpn-webui — Agent Briefing

## Project Goal

A standalone web UI for managing split-tunnel VPN on UniFi Dream Machine SE (and compatible Debian-based UniFi gateways). It replaces the shell-script-based [peacey/split-vpn](https://github.com/peacey/split-vpn) setup with a fully self-contained web application — every feature must be controllable through the UI without SSH or manual file editing. The project must have full IPv4 and IPv6 support of all features. The webui should have simple configurable password authentication with the default password being "split-vpn", but also support simple token auth to allow reverse-proxies to "auto-login".

---

##

## Important notes

* Always reference this file when working, and any files in docs/
* Do proper research against peacey/split-vpn, unifi-utilities/unifios-utilities, and /Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/ while working and replan if necessary.
* Always work on the claude-code branch and commit work as appropriate.

--

## Current Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.24+ |
| HTTP router | `github.com/go-chi/chi/v5` |
| Frontend | Bootstrap 5, Chart.js, Bootstrap Icons, vanilla JS |
| Asset delivery | Go `embed.FS` (all static assets compiled into the binary) |
| VPN runtime | systemd units managed by this app |
| App data | JSON config files under `/data/split-vpn-webui/` |
| Stats database | SQLite at `/data/split-vpn-webui/stats.db` |
| Logs | `/data/split-vpn-webui/logs/` |
| Live updates | Server-Sent Events (SSE) at `/api/stream` |

There are **no runtime dependencies beyond the binary and SQLite** (CGo with `mattn/go-sqlite3`, or a pure-Go SQLite driver). Do not add other databases, container runtimes, or additional system daemons.

---

## Repository Layout

```
split-vpn-webui/
├── cmd/splitvpnwebui/          # Binary entry point (main.go)
├── internal/
│   ├── config/                 # VPN config parsing & discovery
│   ├── latency/                # ICMP latency monitoring
│   ├── server/                 # HTTP server & route handlers
│   ├── settings/               # User preference persistence (JSON)
│   ├── stats/                  # Network statistics collection
│   └── util/                   # Network utility helpers
├── ui/
│   ├── embed.go                # Go embed declarations
│   └── web/
│       ├── static/             # CSS, JS, vendored frontend libs
│       └── templates/          # HTML templates (layout.html)
├── deploy/
│   ├── split-vpn-webui.service # systemd unit for the web app itself
│   └── split-vpn-webui.sh      # Boot hook (goes into /data/on_boot.d)
├── install.sh                  # Installer script
├── go.mod / go.sum
└── AGENTS.md                   # This file
```

**File size limit:** No source file may exceed ~500 lines. Refactor into separate files/packages when approaching this limit. Keep areas of responsibility cleanly separated.

---

## Currently Implemented (keep and do not regress)

- **Statistics collection** — per-interface RX/TX throughput sampled every 2 s, up to 120-point history for Chart.js graphs, WAN throughput corrected by subtracting VPN traffic.
- **Latency monitoring** — ICMP ping to each VPN gateway every 10 s (paused when browser tab is hidden).
- **Config discovery** — recursive scan for `vpn.conf` files, extracts interface name (`DEV=`), VPN type, gateway, autostart marker (`.split-vpn-webui-autostart`).
- **Settings persistence** — WAN interface override and listen interface, stored at `/data/split-vpn-webui/settings.json`.
- **HTTP API** — REST endpoints for config, settings, reload; SSE stream at `/api/stream`.
- **Auto-detect WAN interface** — with manual config fallback via settings UI.

---

## Target Platform Details

**Device:** UniFi Dream Machine SE (and UDM Pro, UDR, etc.)
**OS:** Debian-based, systemd init, BusyBox utilities available alongside standard GNU tools.
**Persistence layer:** [unifi-utilities/unifios-utilities](https://github.com/unifi-utilities/unifios-utilities) `on-boot-script-2.x` package (`udm-boot-2x`).
**Web UI port:** 8091 (configurable via `--addr` flag).

### Filesystem Persistence Model (Modern Firmware 2.x+)

On **UniFi OS 2.x and above** (the target; non-podman, non-container firmware):

- **`/data/`** is the real persistent partition (ext4, directly mounted). It survives both reboots and firmware updates. This is **not** a symlink — it is the actual mount point.
- **`/mnt/data/`** is relevant on legacy 1.x firmware only. **Do not use `/mnt/data/` anywhere in this project.**
- **`/etc/systemd/system/`**, `/usr/local/bin/`, and everything outside `/data/` lives on the rootfs which is **completely replaced by every firmware update**. No file outside `/data/` should be treated as durable.

**Consequence:** The binary, all config, all VPN unit files, the stats DB, and logs must all live under `/data/split-vpn-webui/`. The only things that go outside `/data/` are transient symlinks that the boot script re-creates on every boot.

**Directory layout on device:**
```
/data/split-vpn-webui/
├── split-vpn-webui             # the binary itself
├── settings.json               # app settings
├── stats.db                    # SQLite stats & history database
├── logs/
│   └── split-vpn-webui.log     # application log (rotated)
├── units/
│   ├── split-vpn-webui.service # this app's systemd unit (canonical copy)
│   └── svpn-<vpn-name>.service # VPN systemd units (canonical copies)
└── vpns/
    └── <vpn-name>/
        ├── vpn.conf             # routing metadata
        ├── <name>.wg            # WireGuard config (or .ovpn for OpenVPN)
        └── ...                  # certs, credentials, etc.
```

### How unifios-utilities Boot Persistence Works

The `udm-boot-2x` package installs a **systemd unit** (`udm-boot.service`, `Type=oneshot`) that runs after `network-online.target` on every boot. It executes every file in `/data/on_boot.d/` in sorted alphabetical order using this exact logic:

- If the file **has the executable bit set**: it is run directly (`"$0"`)
- If the file is **not executable but ends in `.sh`**: it is sourced (`. "$0"`)
- All other files are ignored

**Boot scripts must be `chmod +x` and have a `.sh` extension and a `#!/bin/bash` shebang.** Numeric prefixes control execution order (e.g. `10-split-vpn-webui.sh`).

### The Boot Script Pattern for Custom Systemd Services

Because `/etc/systemd/system/` is wiped on firmware updates, the `on_boot.d` script **must re-create symlinks and reload systemd on every boot** — not just on first install. This is the standard pattern:

```bash
#!/bin/bash
# /data/on_boot.d/10-split-vpn-webui.sh

# Re-link this app's service unit (wiped by firmware updates)
ln -sf /data/split-vpn-webui/units/split-vpn-webui.service \
    /etc/systemd/system/split-vpn-webui.service

# Re-link all managed VPN units
for unit in /data/split-vpn-webui/units/svpn-*.service; do
    [ -f "$unit" ] && ln -sf "$unit" "/etc/systemd/system/$(basename "$unit")"
done

systemctl daemon-reload
systemctl enable split-vpn-webui
systemctl restart split-vpn-webui
```

This script runs after every boot (and after every firmware update), making the service fully self-healing.

### Coexistence with peacey/split-vpn and UniFi VPN Manager

This app must coexist peacefully with **both** peacey/split-vpn (if installed) and UniFi's native VPN manager. Treat both as strictly read-only neighbours — never write to, delete, or modify any resource owned by either.

**peacey/split-vpn owns:**
- `/data/split-vpn/` and everything inside it — **never write here**. (On 1.x legacy firmware this was `/mnt/data/split-vpn/`, but on the target 2.x firmware it is `/data/split-vpn/`.)
- Its own ipset names, dnsmasq config entries, iptables rules, and route tables.
- Its own systemd unit files (e.g. `wg0-sgp.swic.name.service`, `wg-quick@*.service`).
- Its own `on_boot.d` scripts in `/data/on_boot.d/`.

**UniFi VPN Manager owns:**
- Interface names `wg0`, `wg1`, … used by its own tunnels.
- systemd service names matching `wg-quick@<name>.service`.
- Route tables and fwmarks in a low numeric range.

**This app's exclusive namespace:**
- Data directory: `/data/split-vpn-webui/` — all app state (binary, config, DB, logs, unit files) lives here.
- Boot script: `/data/on_boot.d/10-split-vpn-webui.sh`.
- systemd unit names: `svpn-<vpn-name>.service` (prefix `svpn-` avoids all known conflicts).
- ipset names: `svpn_<group>_v4` / `svpn_<group>_v6` (prefix `svpn_`).
- dnsmasq drop-in: `/run/dnsmasq.d/split-vpn-webui.conf`.
- Route table IDs and fwmarks: allocated from `200` upward; scan `/etc/iproute2/rt_tables` and live `ip rule` output before allocating to guarantee no collision.
- WireGuard interface names for managed VPNs: user-supplied, but validated against all existing interfaces before use; warn in UI if a conflict is detected.

**Optional read-only discovery:** The app may optionally scan `/data/split-vpn/` to display peacey-managed VPNs in a read-only "existing VPNs" panel, but must never write to that directory or attempt to manage those VPNs.

---

## Requirements

### 1. Standalone Operation (no peacey/split-vpn dependency)

Remove any runtime dependency on the `peacey/split-vpn` shell scripts. The Go application must implement all routing logic itself or generate the required kernel/iptables/ipset configuration directly. Use `peacey/split-vpn` only as a reference for what commands need to be run.

### 2. VPN Support (initial scope: WireGuard and OpenVPN only)

Other VPN types (OpenConnect, OpenVPN over TCP, etc.) may be deferred. The architecture must be extensible — use a VPN-type interface/strategy pattern so additional types can be added later without refactoring the core.

**WireGuard:**
- Config file format: standard `<vpn-name>.wg` (identical to wg-quick `.conf` format). See `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/sgp.swic.name/wg0.conf` for a real example. It should also be "uploadable" via a file and large edit-box and also editable after being uploaded regardless of whether a file was uploaded or the contents were pasted.
- Required fields the UI must expose: `[Interface]` — `PrivateKey`, `Address` (comma-separated CIDR list, IPv4 and/or IPv6), `DNS` (optional), `Table` (route table ID); `[Peer]` — `PublicKey`, `AllowedIPs`, `Endpoint`, `PersistentKeepalive`.
- systemd unit generated by this app wraps `wg-quick up/down`.

**OpenVPN:**
- Config file format: standard `.ovpn` client config. See `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/web.appulize.com/DreamMachine.ovpn` for a real example.
- The UI must allow uploading the `.ovpn` file and any associated credentials/certificates as separate files; the app stores them under `/data/split-vpn-webui/vpns/<vpn-name>/`. It should also be "uploadable" via a large edit-box and also editable after being uploaded regardless of whether a file was uploaded or the contents were pasted.
- systemd unit wraps `openvpn --config <file>`.

**vpn.conf** (split-VPN routing metadata, one per VPN, stored alongside the VPN config):
```
VPN_PROVIDER=external
DEV=<interface>           # e.g. wg0-sgp
ROUTE_TABLE=<id>          # unique integer, this app manages allocation
MARK=<hex>                # unique fwmark, this app manages allocation
FORCED_IPSETS="<name_v4>:dst <name_v6>:dst"
# ... other peacey/split-vpn compatible keys
```

### 3. Full Web UI — No SSH Required

Every action a user would previously perform via SSH must be available in the UI:
- Add / edit / remove a VPN profile (inline config editor or form + file upload).
- Start / stop / restart a VPN (calls `systemctl start|stop|restart svpn-<name>.service`).
- View real-time VPN status, throughput, and latency.

### 4. Domain-Based Routing (ipset / dnsmasq integration)

**Domain Groups:** Users create named groups (e.g., "Streaming-SG", "Gaming") and assign domains to them (one domain per line, wildcards like `*.example.com` supported). Each group is assigned an egress VPN.

**Mechanism (mirrors `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/on_boot.d/20-ipset.sh`):**
1. For each domain group, create two `ipset` sets: `svpn_<group>_v4` (hash:ip, 24 h timeout) and `svpn_<group>_v6` (hash:ip6, 24 h timeout).
2. Write a dnsmasq config file at `/run/dnsmasq.d/split-vpn-webui.conf` with `ipset=/<domain>/<v4set>,<v6set>` entries.
3. Reload dnsmasq (`systemctl reload dnsmasq` or `kill -HUP <pid>`).
4. Add iptables/ip6tables rules to PREROUTING and FORWARD chains to mark packets whose destination is in each ipset with the corresponding VPN fwmark, then route via the VPN's route table.
5. On any change (domain added/removed, group egress changed), regenerate and reload atomically.

All of this must be driven from Go code — not delegated to a shell script. Shell scripts in `deploy/` are only for bootstrap and systemd units.

### 5. DNS Pre-Warm

A background worker (exposed via UI) that pre-fetches DNS records for all configured domains and populates ipsets before clients make requests.

**Behaviour (mirrors `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/on_boot.d/90-ipset-prewarm.sh`):**
- For each domain in every group, query A and AAAA records via Cloudflare DoH (`https://cloudflare-dns.com/dns-query?name=<domain>&type=A`) using the egress VPN's network interface (bind socket to the VPN interface).
- Follow CNAMEs one level deep.
- Insert resolved IPs into the corresponding ipsets with a 12-hour timeout (`ipset add <set> <ip> timeout 43200`).
- Configurable parameters (stored in settings): parallelism (goroutines), per-VPN DoH timeout, run schedule (cron expression or fixed interval).
- UI shows: last run timestamp, duration, domains processed, IPs inserted, per-VPN progress bar for live runs.
- Trigger manually from UI or on configurable schedule.

### 6. systemd Unit Management

- The app creates, writes, enables, and manages systemd unit files for each VPN.
- **Canonical unit file location: `/data/split-vpn-webui/units/svpn-<vpn-name>.service`** — this is the durable copy that survives firmware updates.
- The app also creates a symlink at `/etc/systemd/system/svpn-<vpn-name>.service` → the canonical path, then runs `systemctl daemon-reload`. Because `/etc/systemd/system/` is wiped on firmware updates, the boot script (see Target Platform Details) re-creates all such symlinks on every boot.
- When a VPN is removed, delete both the canonical file and the symlink, then `daemon-reload`.
- Start/stop/restart via `systemctl` subprocess calls (not D-Bus).
- The app itself runs as a separate systemd unit — VPN unit crashes do not affect the web UI.

### 7. Installation

**Prerequisite:** The user must have already installed `udm-boot-2x` from unifios-utilities. The installer should check for it and print a clear error with install instructions if it is absent. Do not attempt to install `udm-boot-2x` automatically — that is the user's responsibility.

Check: `systemctl is-active udm-boot` (exit 0 = installed and active).

Installer must work as: `curl -fsSL https://raw.githubusercontent.com/maciekish/split-vpn-webui/main/install.sh | bash`

`install.sh` must:
1. Verify `udm-boot` is active; abort with clear instructions if not.
2. Detect architecture (amd64 / arm64) and download the appropriate pre-built binary from GitHub Releases.
3. Create `/data/split-vpn-webui/` and subdirectories (`logs/`, `vpns/`, `units/`) if absent. **Do not touch `/data/split-vpn/`.**
4. Place the binary at `/data/split-vpn-webui/split-vpn-webui` and `chmod +x` it. **Never place it in `/usr/local/bin/` or any rootfs path — those are wiped by firmware updates.**
5. Write the canonical systemd unit file to `/data/split-vpn-webui/units/split-vpn-webui.service`.
6. Write the boot hook to `/data/on_boot.d/10-split-vpn-webui.sh` and `chmod +x` it. This script is the persistence mechanism — it runs after every boot (including post-firmware-update) and re-creates all `/etc/systemd/system/` symlinks, runs `systemctl daemon-reload`, enables and starts the service.
7. Run the boot hook immediately to activate the service for the current session: `bash /data/on_boot.d/10-split-vpn-webui.sh`.
8. Print the access URL at the end.

> **Why this works:** `/data/` is the real persistent partition on UniFi OS 2.x+ (not a symlink — the actual mount point). It survives firmware updates. `/etc/systemd/system/` is on the rootfs and is wiped on every firmware update. The boot hook lives safely in `/data/on_boot.d/` and re-creates all ephemeral symlinks on every boot, making the service fully self-healing.

---

## Implementation Guidelines

### Architecture

- **Strategy pattern for VPN types** — `VPNProvider` interface with `WireGuard` and `OpenVPN` implementations. Adding a new VPN type = add a new implementation file, zero changes to core logic.
- **Config manager** — owns reading, writing, and validation of all config files. No other package writes files directly.
- **Routing manager** — owns all ipset, iptables, and dnsmasq interactions. Idempotent: running it twice must not corrupt state.
- **Systemd manager** — owns all `systemctl` and unit-file interactions.
- **Prewarm worker** — runs as a goroutine pool, reports progress via channels consumed by the SSE stream.
- **HTTP layer** — thin handlers only; all business logic in the above packages.

### Code Quality

- Bug-free on first run — test with the real kernel where possible; mock kernel interfaces in unit tests.
- No 500+ line source files; split into subpackages/modules etc before hitting the limit.
- No shortcuts that create future pain (e.g., shelling out to `wg` for parsing when the Go `wireguard-go` libraries exist).
- Handle all reasonable edge cases: interface not found, ipset already exists, systemd unavailable, partial write failures, concurrent API requests.
- Return structured JSON errors from all API endpoints with a consistent `{"error": "..."}` envelope.

### Testing

- Write tests for every package. Run tests continuously during development.
- Unit tests must not require root or modify the host system — use interfaces/mocks for ipset, iptables, systemctl, and file I/O.
- Integration tests (clearly labelled with a build tag `//go:build integration`) may require root and interact with real kernel subsystems; document how to run them.
- Target: every new feature ships with tests before being considered complete.

### Security

- The web UI must be bound to `localhost` or a specific interface by default — not `0.0.0.0` — since it runs with elevated privileges. Figure out how to bind it to the first default LAN on UniFi. Perhaps you can search some config files on the gateway to figure out which interface is "LAN"? It's named LAN in the original UniFi UI. 
- No shell string interpolation with user-supplied data. All subprocess calls must use `exec.Command` with explicit argument slices.
- VPN private keys and credentials must be stored with `0600` permissions.
- Sanitise all user input (domain names, interface names, file paths) before using in filesystem or subprocess calls.

---

## Reference: unifi-split-vpn (Local Dev Copy)

The working split-VPN reference implementation lives at:
```
/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/
```

Key files to consult during development:
- `on_boot.d/20-ipset.sh` — domain routing via ipset/dnsmasq (the pattern to re-implement in Go)
- `on_boot.d/90-ipset-prewarm.sh` — DNS pre-warm script (the pattern to re-implement in Go)
- `on_boot.d/21-wg0.sgp.swic.name-unit.sh` — how systemd VPN units are created on boot
- `on_boot.d/91-ipset-prewarm-cron.sh` — cron wrapper for periodic pre-warming
- `systemd/units/` — example systemd service unit files for VPN interfaces
- `sgp.swic.name/wg0.conf` — real WireGuard client config (use as format reference)
- `web.appulize.com/DreamMachine.ovpn` — real OpenVPN client config (use as format reference)
- `sgp.swic.name/vpn.conf` and `web.appulize.com/vpn.conf` — real split-VPN routing metadata files

These are **examples of what needs to be re-implemented in Go**, not runtime dependencies.

### Domain Routing Setup (`20-ipset.sh` equivalent)

For each domain group:
```bash
# Create ipsets
ipset create svpn_<group>_v4 hash:ip family inet timeout 86400 -exist
ipset create svpn_<group>_v6 hash:ip6 family inet6 timeout 86400 -exist

# dnsmasq entry (written to /run/dnsmasq.d/split-vpn-webui.conf)
ipset=/<domain>/svpn_<group>_v4,svpn_<group>_v6

# iptables rule to mark and route packets
iptables -t mangle -A PREROUTING -m set --match-set svpn_<group>_v4 dst \
    -j MARK --set-mark <fwmark>
ip rule add fwmark <fwmark> table <route_table>
```

### DNS Pre-Warm (`90-ipset-prewarm.sh` equivalent)

For each domain, for each VPN interface regardless of its configured egress:
```bash
# DoH query via specific interface
curl --interface <vpn_dev> -s \
    "https://cloudflare-dns.com/dns-query?name=<domain>&type=A" \
    -H "accept: application/dns-json"
# Parse JSON response, extract "Answer" array, filter type=1 (A) or type=28 (AAAA)
# Insert IPs: ipset add svpn_<group>_v4 <ip> timeout 43200 -exist
```

CNAME chaining: if an answer contains type=5 (CNAME) records, query the CNAME target recursively (one level) before querying A/AAAA.

### WireGuard vpn.conf (reference values)

```ini
VPN_PROVIDER=external
DEV=wg0-sgp
ROUTE_TABLE=101
MARK=0x169
FORCED_IPSETS="svpn_sgp_v4:dst svpn_sgp_v6:dst"
VPN_BOUND_IFACE=br0
```

### OpenVPN vpn.conf (reference values)

```ini
VPN_PROVIDER=openvpn
DEV=tun0
ROUTE_TABLE=102
MARK=0x170
FORCED_IPSETS="svpn_web_v4:dst svpn_web_v6:dst"
VPN_BOUND_IFACE=br0
```

---

## Out of Scope (deferred)

- OpenConnect, L2TP, or other VPN types beyond WireGuard and OpenVPN.
- Multi-user authentication for the web UI (single-admin assumed for now).
