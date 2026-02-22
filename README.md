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
- Version/update management:
  - release checks against GitHub Releases
  - checksum-verified binary updates from installer and web UI
  - self-update worker with rollback-aware service restart path

## Persistence and Paths

All durable app state lives under:

`/data/split-vpn-webui/`

Important locations:

- Binary: `/data/split-vpn-webui/split-vpn-webui`
- Settings: `/data/split-vpn-webui/settings.json`
- Stats DB: `/data/split-vpn-webui/stats.db`
- Logs: `/data/split-vpn-webui/logs/`
- Updater status: `/data/split-vpn-webui/update-status.json`
- Updater job: `/data/split-vpn-webui/update-job.json`
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

## Fast Dev Deploy (no reboot)

For iterative development against a gateway at `root@10.0.0.1`:

```sh
bash ./deploy/dev-deploy.sh
```

This performs a minimal app-only deploy:

- builds `split-vpn-webui` for remote architecture
- copies binary and canonical app unit to `/data/split-vpn-webui/`
- links `/etc/systemd/system/split-vpn-webui.service`
- runs `systemctl daemon-reload` and restarts only `split-vpn-webui.service`

No gateway reboot is performed.

Common options:

```sh
bash ./deploy/dev-deploy.sh --host root@10.0.0.1 --port 22
bash ./deploy/dev-deploy.sh --no-restart
bash ./deploy/dev-deploy.sh --copy-boot-hook --copy-uninstall
```

Uninstall/cleanup for dev iteration:

```sh
bash ./deploy/dev-uninstall.sh --mode iterative
```

Complete uninstall:

```sh
bash ./deploy/dev-uninstall.sh --mode complete
```

Mode behavior:

- `iterative`: removes app bootstrap/service/binary artifacts for fast redeploy, keeps VPN profiles/units/config/stats.
- `complete`: removes everything created by split-vpn-webui (services, boot hook, managed VPNs/units, config, stats, runtime artifacts).

Compatibility alias:

```sh
bash ./deploy/dev-cleanup.sh
```

## Install on UniFi Gateway

Prerequisite: `udm-boot-2x` must already be installed and active.

```sh
curl -fsSL https://raw.githubusercontent.com/maciekish/split-vpn-webui/main/install.sh | bash
```

Installer behavior:

1. verifies `udm-boot` is active
2. resolves target release (`latest` or `VERSION=vX.Y.Z`)
3. detects existing install and prompts before update/reinstall
4. downloads the correct release binary for `amd64`/`arm64`
5. verifies SHA256 checksum from release `SHA256SUMS`
6. writes binary and app files to `/data/split-vpn-webui/`
7. writes canonical unit files to `/data/split-vpn-webui/units/`
8. writes boot hook to `/data/on_boot.d/10-split-vpn-webui.sh`
9. runs the boot hook immediately to activate service

Example pinned install/update:

```sh
VERSION=v1.2.3 curl -fsSL https://raw.githubusercontent.com/maciekish/split-vpn-webui/main/install.sh | bash
```

Non-interactive update (auto-confirm prompt):

```sh
ASSUME_YES=1 curl -fsSL https://raw.githubusercontent.com/maciekish/split-vpn-webui/main/install.sh | bash
```

## Versioning and Releases

- Tags in the format `vX.Y.Z` trigger CI builds for Linux `amd64` and `arm64`.
- Release artifacts include:
  - `split-vpn-webui-linux-amd64`
  - `split-vpn-webui-linux-arm64`
  - `SHA256SUMS`
- GitHub auto-generated release notes are enabled.
- Workflow also attempts an AI summary section for "What's New" and falls back cleanly if unavailable.

## Web UI Updates

- Settings modal includes:
  - current version/build metadata
  - latest release status
  - manual "Check" and "Update" actions
  - optional target tag override
- Update flow:
  1. checks release metadata
  2. downloads and verifies checksum
  3. stages update and starts dedicated updater unit
  4. updater swaps binary and restarts app service
  5. on restart failure, updater restores previous binary and retries service start

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
