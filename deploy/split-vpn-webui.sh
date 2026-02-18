#!/bin/bash
# /data/on_boot.d/10-split-vpn-webui.sh
#
# Boot hook for split-vpn-webui — installed by install.sh.
# Runs on EVERY boot (including after firmware updates) via udm-boot.service.
#
# Firmware updates wipe /etc/systemd/system/, so this script re-creates the
# necessary symlinks and (re)starts the service on every boot.

set -e

DATA_DIR="/data/split-vpn-webui"
UNITS_DIR="${DATA_DIR}/units"
SYSTEMD_DIR="/etc/systemd/system"

# Ensure the units directory exists (it should, but be defensive).
mkdir -p "${UNITS_DIR}"

# Re-link this app's own service unit.
if [ -f "${UNITS_DIR}/split-vpn-webui.service" ]; then
    ln -sf "${UNITS_DIR}/split-vpn-webui.service" \
        "${SYSTEMD_DIR}/split-vpn-webui.service"
fi

# Re-link all managed VPN units (svpn-*.service).
for unit in "${UNITS_DIR}"/svpn-*.service; do
    [ -f "${unit}" ] || continue
    ln -sf "${unit}" "${SYSTEMD_DIR}/$(basename "${unit}")"
done

# Reload systemd so it picks up the newly linked unit files.
systemctl daemon-reload

# Enable and start the web UI service.
systemctl enable split-vpn-webui.service 2>/dev/null || true
systemctl restart split-vpn-webui.service
