#!/bin/sh
set -e

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
BIN_PATH="$SCRIPT_DIR/bin/split-vpn-webui"
SERVICE_NAME="split-vpn-webui.service"
SERVICE_PATH="/etc/systemd/system/$SERVICE_NAME"
BOOT_SCRIPT="/mnt/data/on_boot.d/split-vpn-webui.sh"
SYSTEMCTL="/usr/bin/systemctl"

if [ ! -x "$BIN_PATH" ]; then
  echo "warning: $BIN_PATH is missing or not executable" >&2
fi

cat >"$SERVICE_PATH" <<UNIT
[Unit]
Description=Split VPN Web UI
After=network.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$SCRIPT_DIR
ExecStart=$BIN_PATH --addr :8091 --split-vpn-dir /mnt/data/split-vpn --systemd
Restart=on-failure
RestartSec=5s
AmbientCapabilities=CAP_NET_ADMIN
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
chmod 644 "$SERVICE_PATH"

install -d -m 755 /mnt/data/on_boot.d
cat >"$BOOT_SCRIPT" <<'BOOT'
#!/bin/sh
/usr/bin/systemctl enable split-vpn-webui.service >/dev/null 2>&1
/usr/bin/systemctl restart split-vpn-webui.service >/dev/null 2>&1
BOOT
chmod 755 "$BOOT_SCRIPT"

$SYSTEMCTL daemon-reload
$SYSTEMCTL enable split-vpn-webui.service
$SYSTEMCTL restart split-vpn-webui.service

echo "Installed $SERVICE_NAME with binary $BIN_PATH"
