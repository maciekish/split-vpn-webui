# split-vpn-webui — Agent Briefing

## Project Goal

A standalone web UI for managing split-tunnel VPN on UniFi Dream Machine SE (and compatible Debian-based UniFi gateways). It replaces the shell-script-based [peacey/split-vpn](https://github.com/peacey/split-vpn) setup with a fully self-contained web application — every feature must be controllable through the UI without SSH or manual file editing.

---

## Current Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.24+ |
| HTTP router | `github.com/go-chi/chi/v5` |
| Frontend | Bootstrap 5, Chart.js, Bootstrap Icons, vanilla JS |
| Asset delivery | Go `embed.FS` (all static assets compiled into the binary) |
| VPN runtime | systemd units managed by this app |
| Persistence | JSON files under `/mnt/data/split-vpn/` |
| Live updates | Server-Sent Events (SSE) at `/api/stream` |

There are **no other runtime dependencies** — the binary must be fully self-contained. Do not add databases, container runtimes, or additional system daemons.

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
- **Settings persistence** — WAN interface override and listen interface, stored at `/mnt/data/split-vpn/.split-vpn-webui-settings.json`.
- **HTTP API** — REST endpoints for config, settings, reload; SSE stream at `/api/stream`.
- **Auto-detect WAN interface** — with manual config fallback via settings UI.

---

## Target Platform Details

**Device:** UniFi Dream Machine SE (and UDM Pro, UDR, etc.)
**OS:** Debian-based, systemd init, BusyBox utilities available alongside standard GNU tools.
**Persistence layer:** [unifi-utilities/unifios-utilities](https://github.com/unifi-utilities/unifios-utilities) — scripts in `/data/on_boot.d/` are re-executed on every boot to restore configuration that UniFi firmware resets on upgrade.
**Data directory:** `/mnt/data/split-vpn/` (survives firmware updates; symlinked to `/data/split-vpn/`).
**Install location:** `/data/split-vpn-webui/` with binary at `/usr/local/bin/split-vpn-webui`.
**Web UI port:** 8091 (configurable via `--addr` flag).

### UniFi VPN Manager Coexistence

UniFi's own VPN manager uses:
- Interface names prefixed with `wg` for its WireGuard tunnels (e.g., `wg0`, `wg1`).
- systemd service names following the pattern `wg-quick@<name>.service`.
- Route tables and marks allocated from a low range.

**This app must NOT clash with UniFi's namespace.** Use the following conventions for managed VPNs:
- systemd unit names: `svpn-<vpn-name>.service` (prefix `svpn-` to avoid collision).
- WireGuard interface names: use the name from the user-supplied `.wg` config file, but validate it does not collide with any interface already managed by UniFi.
- Route table IDs and fwmarks: start allocation from `200` upward (UniFi uses low values).
- Before creating any resource (interface, route table, mark), check for conflicts and warn the user in the UI.

---

## Requirements

### 1. Standalone Operation (no peacey/split-vpn dependency)

Remove any runtime dependency on the `peacey/split-vpn` shell scripts. The Go application must implement all routing logic itself or generate the required kernel/iptables/ipset configuration directly. Use `peacey/split-vpn` only as a reference for what commands need to be run.

### 2. VPN Support (initial scope: WireGuard and OpenVPN only)

Other VPN types (OpenConnect, OpenVPN over TCP, etc.) may be deferred. The architecture must be extensible — use a VPN-type interface/strategy pattern so additional types can be added later without refactoring the core.

**WireGuard:**
- Config file format: standard `<vpn-name>.wg` (identical to wg-quick `.conf` format). See `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/sgp.swic.name/wg0.conf` for a real example.
- Required fields the UI must expose: `[Interface]` — `PrivateKey`, `Address` (comma-separated CIDR list, IPv4 and/or IPv6), `DNS` (optional), `Table` (route table ID); `[Peer]` — `PublicKey`, `AllowedIPs`, `Endpoint`, `PersistentKeepalive`.
- systemd unit generated by this app wraps `wg-quick up/down`.

**OpenVPN:**
- Config file format: standard `.ovpn` client config. See `/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/web.appulize.com/DreamMachine.ovpn` for a real example.
- The UI must allow uploading the `.ovpn` file and any associated credentials/certificates as separate files; the app stores them under `/mnt/data/split-vpn/<vpn-name>/`.
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
- Unit file path: `/etc/systemd/system/svpn-<vpn-name>.service`.
- After writing a unit file: run `systemctl daemon-reload`.
- Start/stop/restart via `systemctl` subprocess calls (not D-Bus).
- The app itself runs as a separate systemd unit (`split-vpn-webui.service`) — VPN units are independent so a VPN crash does not affect the web UI.

### 7. Installation

Installer must work as: `curl -fsSL https://raw.githubusercontent.com/maciekish/split-vpn-webui/main/install.sh | bash`

`install.sh` must:
1. Detect architecture (amd64 / arm64) and download the appropriate pre-built binary from GitHub Releases.
2. Place binary at `/usr/local/bin/split-vpn-webui`.
3. Create `/mnt/data/split-vpn/` if absent.
4. Write the systemd unit file to `/etc/systemd/system/split-vpn-webui.service`.
5. Write the boot hook to `/data/on_boot.d/10-split-vpn-webui.sh` (makes the app restart-persistent across UniFi firmware upgrades via unifios-utilities).
6. Run `systemctl daemon-reload && systemctl enable --now split-vpn-webui`.
7. Print the access URL at the end.

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

For each domain, bound to each VPN interface:
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
- IPv6 WAN support (IPv6 LAN traffic through VPN is in scope; IPv6 WAN is not).
