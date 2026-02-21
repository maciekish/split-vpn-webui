#!/bin/bash
set -euo pipefail

REPO="maciekish/split-vpn-webui"
DATA_DIR="/data/split-vpn-webui"
BINARY_PATH="${DATA_DIR}/split-vpn-webui"
UNITS_DIR="${DATA_DIR}/units"
SERVICE_NAME="split-vpn-webui.service"
SERVICE_PATH="${UNITS_DIR}/${SERVICE_NAME}"
BOOT_SCRIPT_PATH="/data/on_boot.d/10-split-vpn-webui.sh"
GITHUB_API="https://api.github.com/repos/${REPO}"

need_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command not found: $1" >&2
		exit 1
	fi
}

fail() {
	echo "error: $*" >&2
	exit 1
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64)
			echo "amd64"
			;;
		aarch64|arm64)
			echo "arm64"
			;;
		*)
			fail "unsupported architecture: $(uname -m) (expected amd64 or arm64)"
			;;
	esac
}

resolve_release_asset() {
	local arch="$1"
	local release_ref
	if [[ -n "${VERSION:-}" ]]; then
		release_ref="tags/${VERSION}"
	else
		release_ref="latest"
	fi

	local json
	json="$(curl -fsSL -H 'Accept: application/vnd.github+json' "${GITHUB_API}/releases/${release_ref}")"

	local url
	while IFS= read -r url; do
		local lower
		lower="$(printf '%s' "${url}" | tr '[:upper:]' '[:lower:]')"
		case "${lower}" in
			*sha256*|*checksum*|*.sig|*.asc)
				continue
				;;
		esac
		if [[ "${lower}" == *linux*"${arch}"* || "${lower}" == *"${arch}"*linux* || "${lower}" == */split-vpn-webui-"${arch}"* || "${lower}" == */split-vpn-webui_"${arch}"* ]]; then
			echo "${url}"
			return 0
		fi
	done < <(printf '%s\n' "${json}" | sed -n 's/^[[:space:]]*"browser_download_url":[[:space:]]*"\(.*\)",\{0,1\}[[:space:]]*$/\1/p')

	fail "no linux/${arch} release asset found for ${REPO}"
}

install_binary_from_asset() {
	local asset_url="$1"
	local tmp
	tmp="$(mktemp)"
	trap 'rm -f "${tmp}"' RETURN

	echo "Downloading release binary: ${asset_url}"
	curl -fsSL "${asset_url}" -o "${tmp}"
	install -m 0755 "${tmp}" "${BINARY_PATH}"
}

write_service_unit() {
	cat >"${SERVICE_PATH}" <<'UNIT'
[Unit]
Description=Split VPN Web UI
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/data/split-vpn-webui/split-vpn-webui --systemd
WorkingDirectory=/data/split-vpn-webui
Restart=on-failure
RestartSec=5s
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=true
StandardOutput=append:/data/split-vpn-webui/logs/split-vpn-webui.log
StandardError=append:/data/split-vpn-webui/logs/split-vpn-webui.log

[Install]
WantedBy=multi-user.target
UNIT
	chmod 0644 "${SERVICE_PATH}"
}

write_boot_hook() {
	cat >"${BOOT_SCRIPT_PATH}" <<'HOOK'
#!/bin/bash
set -e

DATA_DIR="/data/split-vpn-webui"
UNITS_DIR="${DATA_DIR}/units"
SYSTEMD_DIR="/etc/systemd/system"

mkdir -p "${UNITS_DIR}"

if [ -f "${UNITS_DIR}/split-vpn-webui.service" ]; then
	ln -sf "${UNITS_DIR}/split-vpn-webui.service" "${SYSTEMD_DIR}/split-vpn-webui.service"
fi

for unit in "${UNITS_DIR}"/svpn-*.service; do
	[ -f "${unit}" ] || continue
	ln -sf "${unit}" "${SYSTEMD_DIR}/$(basename "${unit}")"
done

systemctl daemon-reload
systemctl enable split-vpn-webui.service >/dev/null 2>&1 || true
systemctl restart split-vpn-webui.service
HOOK
	chmod 0755 "${BOOT_SCRIPT_PATH}"
}

detect_access_url() {
	local lan_ip
	lan_ip="$(ip -4 -o addr show | awk '/ br[0-9]* / {split($4, a, "/"); print a[1]; exit}')"
	if [[ -z "${lan_ip}" ]]; then
		lan_ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
	fi
	if [[ -n "${lan_ip}" ]]; then
		echo "http://${lan_ip}:8091"
	else
		echo "http://<gateway-lan-ip>:8091"
	fi
}

main() {
	need_cmd curl
	need_cmd systemctl
	need_cmd install
	need_cmd uname
	need_cmd awk
	need_cmd sed
	need_cmd ip

	if [[ "${EUID}" -ne 0 ]]; then
		fail "this installer must run as root"
	fi

	if ! systemctl is-active --quiet udm-boot; then
		cat >&2 <<'MSG'
error: udm-boot-2x is required but not active.

Install it first from unifi-utilities:
https://github.com/unifi-utilities/unifios-utilities/tree/main/on-boot-script-2.x
Then ensure `systemctl is-active udm-boot` returns active.
MSG
		exit 1
	fi

	local arch
	arch="$(detect_arch)"
	local asset_url
	asset_url="$(resolve_release_asset "${arch}")"

	install -d -m 0700 "${DATA_DIR}"
	install -d -m 0755 "${DATA_DIR}/logs" "${UNITS_DIR}"
	install -d -m 0700 "${DATA_DIR}/vpns"
	install -d -m 0755 "/data/on_boot.d"

	install_binary_from_asset "${asset_url}"
	write_service_unit
	write_boot_hook

	echo "Activating service via boot hook..."
	bash "${BOOT_SCRIPT_PATH}"

	echo
	echo "split-vpn-webui installed successfully"
	echo "Binary: ${BINARY_PATH}"
	echo "Service unit: ${SERVICE_PATH}"
	echo "Boot hook: ${BOOT_SCRIPT_PATH}"
	echo "Access URL: $(detect_access_url)"
}

main "$@"
