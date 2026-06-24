# Feature Requirements

## 1. Standalone Operation

No runtime dependency on peacey/split-vpn shell scripts. All routing logic implemented in Go. Use peacey/split-vpn only as a reference.

## 2. VPN Support (WireGuard and OpenVPN)

Architecture must be extensible via `VPNProvider` interface. Adding a new type = new implementation file only.

**WireGuard:** Config as `<vpn-name>.wg` (wg-quick format). UI must expose: `[Interface]` — `PrivateKey`, `Address`, `DNS`, `Table`; `[Peer]` — `PublicKey`, `AllowedIPs`, `Endpoint`, `PersistentKeepalive`. Uploadable via file or paste; editable after upload. systemd unit wraps `wg-quick up/down`.

**OpenVPN:** Config as `.ovpn`. UI allows uploading the `.ovpn` and any associated credential/cert files. Also editable via textarea. systemd unit wraps `openvpn --config`.

**vpn.conf** (routing metadata alongside each VPN config):
```
VPN_PROVIDER=external|openvpn
DEV=<interface>
ROUTE_TABLE=<id>
MARK=<hex>
FORCED_IPSETS="<name_v4>:dst <name_v6>:dst"
```

## 3. Full Web UI — No SSH Required

- Add / edit / remove VPN profiles (editor + file upload)
- Start / stop / restart VPN via `systemctl`
- View real-time status, throughput, latency

## 4. Policy-Based Routing

Routing Groups have one egress VPN and one or more rules. Rules are ORed within a group; selectors within a rule are ANDed.

**Selector types per rule:**
- Source interface (e.g. `br0`)
- Source IP/CIDR (IPv4 and IPv6)
- Source MAC address
- Destination IP/CIDR (IPv4 and IPv6)
- Destination port/range (TCP, UDP, or both)
- Destination ASN (e.g. `AS13335`)
- Domains (exact FQDN)
- Wildcard domains (e.g. `*.apple.com`)
- Excluded source/destination CIDRs, ports, ASNs (negation)
- Exclude multicast (default on)

**Runtime mechanism:**
- Per-group destination sets `svpn_<group>_v4/v6`, source sets `svpn_<group>_src_v4/v6`
- Custom iptables chains `SVPN_MARK_A/B` (generation-swap pattern for hitless apply) + `SVPN_NAT_A/B`
- Per-rule subchains `SVPNA_*/SVPNB_*` for exclusion RETURN logic
- `ip rule` + `ip -6 rule` to route fwmark traffic to VPN route table
- MASQUERADE in nat POSTROUTING for each VPN
- All changes applied atomically; `Apply()` is idempotent

## 5. Resolver & Pre-Warm Workers

Background workers resolve and refresh all dynamic selectors:
- Domain resolver: A/AAAA + one-level CNAME chaining via Cloudflare DoH
- Wildcard subdomain discovery: `crt.sh` certificate-transparency source
- ASN resolver: RIPE announced-prefixes API

Cache: 24h additive TTL. Items evicted only when not refreshed for >24h (not on first miss). Clear-cache + re-run controls in UI.

Pre-warm queries each domain through **every active VPN interface** (not just egress), collecting all unique IPs.

Settings: parallelism, timeout, schedule interval, extra nameservers, ECS profiles.

UI shows: last run timestamp, duration, items processed, IPs inserted/removed, live progress.

## 6. systemd Unit Management

- Canonical unit files in `/data/split-vpn-webui/units/svpn-<name>.service`
- Symlinked to `/etc/systemd/system/svpn-<name>.service`; boot hook re-creates on every boot
- Self-healing: `ensureLinkedUnit` before every start/stop/restart
- Remove: delete canonical + symlink + `daemon-reload`
- Start/stop/restart via `exec.Command("systemctl", ...)` (not D-Bus)

## 7. Installation

```
curl -fsSL https://raw.githubusercontent.com/maciekish/split-vpn-webui/main/install.sh | bash
```

1. Verify `udm-boot` is active; abort with instructions if not (`systemctl is-active udm-boot`)
2. Detect arch (amd64/arm64); download binary from GitHub Releases with checksum verification
3. Create `/data/split-vpn-webui/{logs,vpns,units}/`
4. Write binary to `/data/split-vpn-webui/split-vpn-webui` + `chmod +x`
5. Write systemd unit to `/data/split-vpn-webui/units/split-vpn-webui.service`
6. Write boot hook to `/data/on_boot.d/10-split-vpn-webui.sh` + `chmod +x`
7. Run boot hook immediately
8. Print access URL

## 8. Uninstallation

`/data/split-vpn-webui/uninstall.sh` — interactive flow:
1. Prompt "Remove EVERYTHING? [y/N]"
2. If No, prompt per category: binaries, VPNs+units, config files, statistics
3. Default No for all prompts
4. Stop/disable services before removing, run `daemon-reload` after unit changes
5. Remove only app-namespace resources (`svpn-*`, `svpn_*`, `SVPN_*`, boot hook)
6. Print final summary of removed vs kept

## Authentication

- Password auth (default password: `split-vpn`); bcrypt hash stored in settings
- Token auth via `Authorization: Bearer <token>` header for reverse proxy auto-login
- Session cookie for browser sessions
- Change password requires current password

## Out of Scope (Deferred)

- OpenConnect, L2TP, or other VPN types beyond WireGuard and OpenVPN
- Multi-user authentication (single-admin assumed)
