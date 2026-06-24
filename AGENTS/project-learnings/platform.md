# Target Platform Details

**Device:** UniFi Dream Machine SE (and UDM Pro, UDR, etc.)
**OS:** Debian-based, systemd init, BusyBox utilities available alongside standard GNU tools.
**Persistence layer:** [unifi-utilities/unifios-utilities](https://github.com/unifi-utilities/unifios-utilities) `on-boot-script-2.x` package (`udm-boot-2x`).
**Web UI port:** 8091 (configurable via `--addr` flag).

## Filesystem Persistence Model (Modern Firmware 2.x+)

- **`/data/`** is the real persistent partition (ext4, directly mounted). Survives reboots and firmware updates. Not a symlink — the actual mount point.
- **`/mnt/data/`** is legacy 1.x firmware only. **Never use `/mnt/data/` in this project.**
- **`/etc/systemd/system/`**, `/usr/local/bin/`, and everything outside `/data/` lives on the rootfs, **completely replaced by every firmware update**. Never treat these as durable.

The binary, all config, all VPN unit files, stats DB, and logs must live under `/data/split-vpn-webui/`. Only transient symlinks go outside `/data/`, re-created by the boot script on every boot.

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

## How unifios-utilities Boot Persistence Works

`udm-boot-2x` installs a systemd unit (`udm-boot.service`, `Type=oneshot`) that runs after `network-online.target` on every boot. It executes every file in `/data/on_boot.d/` in sorted alphabetical order:

- File **has executable bit**: run directly (`"$0"`)
- File **not executable but ends in `.sh`**: sourced (`. "$0"`)
- All other files ignored

**Boot scripts must be `chmod +x`, have `.sh` extension, and `#!/bin/bash` shebang.** Numeric prefixes control execution order (e.g. `10-split-vpn-webui.sh`).

## The Boot Script Pattern

Because `/etc/systemd/system/` is wiped on firmware updates, the `on_boot.d` script **must re-create symlinks and reload systemd on every boot**:

```bash
#!/bin/bash
# /data/on_boot.d/10-split-vpn-webui.sh

ln -sf /data/split-vpn-webui/units/split-vpn-webui.service \
    /etc/systemd/system/split-vpn-webui.service

for unit in /data/split-vpn-webui/units/svpn-*.service; do
    [ -f "$unit" ] && ln -sf "$unit" "/etc/systemd/system/$(basename "$unit")"
done

systemctl daemon-reload
systemctl enable split-vpn-webui
systemctl restart split-vpn-webui
```

## Coexistence with peacey/split-vpn and UniFi VPN Manager

Treat both as strictly read-only neighbours — never write to, delete, or modify any resource owned by either.

**peacey/split-vpn owns:** `/data/split-vpn/` and everything inside it — **never write here**. Its own ipset names, dnsmasq config, iptables rules, route tables, systemd units (`wg0-sgp.contoso.com.service`, `wg-quick@*.service`), and `on_boot.d` scripts.

**UniFi VPN Manager owns:** Interface names `wg0`, `wg1`, …; `wg-quick@<name>.service`; route tables and fwmarks in the low numeric range.

**This app's exclusive namespace:**
- Data: `/data/split-vpn-webui/`
- Boot script: `/data/on_boot.d/10-split-vpn-webui.sh`
- systemd units: `svpn-<vpn-name>.service`
- ipset names: `svpn_<group>_v4` / `svpn_<group>_v6` / `svpn_<group>_src_v4` / `svpn_<group>_src_v6`
- dnsmasq drop-in: `/run/dnsmasq.d/split-vpn-webui.conf`
- Route table IDs and fwmarks: allocated from `200` upward; scan `/etc/iproute2/rt_tables` and live `ip rule` output before allocating to guarantee no collision
- WireGuard interface names: user-supplied, validated against all existing interfaces before use; warn in UI on conflict
