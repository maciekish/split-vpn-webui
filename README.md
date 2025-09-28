# Split VPN Web UI

A lightweight Go web application for monitoring [peacey/split-vpn](https://github.com/peacey/split-vpn) on UniFi gateways. The UI provides live interface statistics, latency visibility, and persisted preferences while remaining responsive on constrained hardware. VPN lifecycle and configuration management are planned for a future release; the controls are visible in the UI but intentionally disabled in this monitoring-focused build.

## Features

- 📊 **Live interface metrics** – WAN and VPN interfaces display current throughput, per-interface history charts, and corrected WAN usage (VPN traffic is subtracted automatically).
- 📶 **Latency visibility** – Periodic pings to each tunnel gateway while the UI is open. Monitoring pauses automatically when the browser tab is hidden to minimise load.
- ⚙️ **Persisted preferences** – Choose the WAN interface used for corrected statistics and restrict the UI to a specific listen interface via the settings panel. Selections are saved under `/mnt/data/split-vpn` and survive restarts.
- 🎨 **Responsive design** – Bootstrap 5 based layout with Chart.js visualisations, local assets, and mobile-friendly controls.

## Planned functionality

The UI exposes placeholders for these capabilities so the layout remains familiar, but they are currently disabled while the monitoring workflow stabilises:

- 🛡️ Autostart toggles for each tunnel
- 📝 Direct editing of `vpn.conf`
- 🔌 Start/stop buttons for tunnel processes

## Building

```sh
# Build the static binary
GOOS=linux GOARCH=amd64 go build -o bin/split-vpn-webui ./cmd/splitvpnwebui
```

During development you can run the server directly:

```sh
go run ./cmd/splitvpnwebui --addr :8091 --split-vpn-dir ./testdata
```

The server defaults to `/mnt/data/split-vpn` but the base directory is configurable through `--split-vpn-dir`. It listens on TCP port `8091` by default (UniFi reserves the `808x` range); override with `--addr` or via the in-app settings if needed. Pass `--systemd` when running under the provided unit so the service can restart itself after settings changes.

## Deployment on UniFi Gateways

1. Copy the compiled `split-vpn-webui` binary to `/usr/local/bin/` on the gateway.
2. From the project directory run `./install.sh` to install or refresh the systemd unit and boot script. The installer writes the unit with the absolute path to your `bin/split-vpn-webui` binary (including `--systemd`), reloads systemd, enables the service, and starts it immediately.
3. Browse to `https://<gateway-ip>:8091` (adjust port/host as needed) to access the UI.

## Autostart markers

Autostart selections are saved by creating or removing a `.split-vpn-webui-autostart` file inside each VPN directory. The current UI does not modify these markers, but future releases will re-enable the toggle switches once control workflows are complete.

## Latency and statistics behaviour

- Interface statistics are sampled every two seconds and stored in memory (default history of 120 points).
- WAN throughput is corrected by subtracting VPN interface usage so the WAN card reflects actual non-tunnel traffic.
- Latency pings run every 10 seconds **only** while at least one browser tab has the UI visible.

## Development workflow helpers

The codebase is organised for hot-reload friendly tools such as [`air`](https://github.com/cosmtrek/air) or [`reflex`](https://github.com/cespare/reflex). Run your preferred watcher to rebuild `cmd/splitvpnwebui` as files change.

## Notes

- All CSS/JS dependencies are vendored locally to remain operational during WAN outages.
- Bootstrap Icons webfonts are not bundled. Download the latest release from [twbs/icons](https://github.com/twbs/icons/releases), copy the `.woff`/`.woff2` files into `ui/web/static/vendor/bootstrap-icons/fonts/`, and rebuild to embed them.
- Planned control features will rely on the upstream `run-vpn.sh` / `stop-vpn.sh` scripts once they return.

