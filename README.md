# split-vpn-webui

Standalone Go web UI for managing split-tunnel VPN on UniFi gateways (UDM SE/Pro/UDR class devices).  
The app is self-contained and does not depend on `peacey/split-vpn` scripts at runtime.

## What It Does

- Manage VPN profiles end-to-end from the browser:
  - WireGuard and OpenVPN create/edit/delete
  - Start/stop/restart and autostart via systemd
- Apply split-routing policies to VPN-assigned routing groups:
  - source IP/CIDR
  - destination IP/CIDR
  - destination ports/protocol
  - destination ASN (resolved to prefixes)
  - exact domains
  - wildcard domains (`*.example.com`) with public subdomain discovery
- Keep dynamic selectors fresh at runtime:
  - periodic resolver refresh (domain/ASN/wildcard)
  - manual resolver run from UI/API
  - stale snapshot replacement
- DNS pre-warm worker:
  - Cloudflare DoH over VPN interfaces
  - A/AAAA + one-level CNAME follow
  - writes to app-managed ipsets with timeout
- Real-time monitoring:
  - per-interface throughput
  - latency tracking
  - SSE live updates
- Authentication:
  - password login (default password: `split-vpn`)
  - bearer token API auth for reverse-proxy auto-login patterns

## Persistence and Paths

All durable app state lives under:

`/data/split-vpn-webui/`

Important locations:

- Binary: `/data/split-vpn-webui/split-vpn-webui`
- Settings: `/data/split-vpn-webui/settings.json`
- Stats DB: `/data/split-vpn-webui/stats.db`
- Logs: `/data/split-vpn-webui/logs/`
- VPN profiles: `/data/split-vpn-webui/vpns/<vpn-name>/`
- Canonical units: `/data/split-vpn-webui/units/`
- Boot hook: `/data/on_boot.d/10-split-vpn-webui.sh`

The app uses namespaced resources to avoid clashes:

- systemd units: `svpn-<vpn-name>.service`
- ipset names: `svpn_*`
- iptables chains: `SVPN_*`

## Build and Test

```sh
go test ./...
go build ./cmd/splitvpnwebui
```

Cross-compile:

```sh
GOOS=linux GOARCH=amd64 go build -o split-vpn-webui-amd64 ./cmd/splitvpnwebui
GOOS=linux GOARCH=arm64 go build -o split-vpn-webui-arm64 ./cmd/splitvpnwebui
```

Run locally:

```sh
go run ./cmd/splitvpnwebui --addr :8091 --data-dir ./tmp-data
```

## Install on UniFi Gateway

Prerequisite: `udm-boot-2x` must already be installed and active.

```sh
curl -fsSL https://raw.githubusercontent.com/maciekish/split-vpn-webui/main/install.sh | bash
```

Installer behavior:

1. verifies `udm-boot` is active
2. downloads the correct release binary for `amd64`/`arm64`
3. writes binary and app files to `/data/split-vpn-webui/`
4. writes canonical unit files to `/data/split-vpn-webui/units/`
5. writes boot hook to `/data/on_boot.d/10-split-vpn-webui.sh`
6. runs the boot hook immediately to activate service

## Uninstall

Interactive uninstall script:

```sh
/data/split-vpn-webui/uninstall.sh
```

Flow:

1. asks `Remove EVERYTHING related to split-vpn-webui? [y/N]`
2. if `No`, asks category-by-category:
   - binaries
   - VPNs + their systemd units
   - config files
   - statistics data

It only removes app-owned resources and does not touch `/data/split-vpn`.

## Notes

- Supports coexistence with `peacey/split-vpn` and UniFi VPN manager (read-only neighbor behavior).
- API errors use a consistent JSON shape: `{"error":"..."}`.
- Static UI assets are embedded in the binary (`embed.FS`).
