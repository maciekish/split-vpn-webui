#!/bin/bash
set -euo pipefail

REPO="maciekish/split-vpn-webui"
DATA_DIR="/data/split-vpn-webui"
BINARY_PATH="${DATA_DIR}/split-vpn-webui"
UNITS_DIR="${DATA_DIR}/units"
SERVICE_NAME="split-vpn-webui.service"
SERVICE_PATH="${UNITS_DIR}/${SERVICE_NAME}"
BOOT_SCRIPT_PATH="/data/on_boot.d/10-split-vpn-webui.sh"
UNINSTALL_PATH="${DATA_DIR}/uninstall.sh"
GITHUB_API="https://api.github.com/repos/${REPO}"
RELEASE_JSON=""
RELEASE_TAG=""
RELEASE_ASSET_URL=""
RELEASE_CHECKSUM_URL=""

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
	done < <(printf '%s\n' "${RELEASE_JSON}" | sed -n 's/^[[:space:]]*"browser_download_url":[[:space:]]*"\(.*\)",\{0,1\}[[:space:]]*$/\1/p')

	fail "no linux/${arch} release asset found for ${REPO}"
}

resolve_release_checksum_asset() {
	local url
	while IFS= read -r url; do
		local lower
		lower="$(printf '%s' "${url}" | tr '[:upper:]' '[:lower:]')"
		if [[ "${lower}" == *sha256* || "${lower}" == *checksum* ]]; then
			echo "${url}"
			return 0
		fi
	done < <(printf '%s\n' "${RELEASE_JSON}" | sed -n 's/^[[:space:]]*"browser_download_url":[[:space:]]*"\(.*\)",\{0,1\}[[:space:]]*$/\1/p')
	fail "no checksum asset found for release ${RELEASE_TAG}"
}

fetch_release_metadata() {
	local release_ref
	if [[ -n "${VERSION:-}" ]]; then
		release_ref="tags/${VERSION}"
	else
		release_ref="latest"
	fi
	RELEASE_JSON="$(curl -fsSL -H 'Accept: application/vnd.github+json' "${GITHUB_API}/releases/${release_ref}")"
	RELEASE_TAG="$(printf '%s\n' "${RELEASE_JSON}" | sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*$/\1/p' | head -n1)"
	[[ -n "${RELEASE_TAG}" ]] || fail "unable to determine release tag from GitHub response"
}

sha256_file() {
	local path="$1"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "${path}" | awk '{print tolower($1)}'
		return 0
	fi
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "${path}" | awk '{print tolower($1)}'
		return 0
	fi
	fail "neither sha256sum nor shasum is available for checksum verification"
}

verify_asset_checksum() {
	local asset_url="$1"
	local checksum_url="$2"
	local binary_path="$3"
	local checksum_tmp
	checksum_tmp="$(mktemp)"
	curl -fsSL "${checksum_url}" -o "${checksum_tmp}"

	local asset_name
	asset_name="$(basename "${asset_url}")"
	local expected
	expected="$(
		awk -v file="${asset_name}" '
		{
			sum=tolower($1)
			name=$NF
			gsub(/^\*/, "", name)
			sub(/^\.\//, "", name)
			n=split(name, parts, "/")
			if (n > 0) {
				name=parts[n]
			}
			if (name==file) {
				print sum
				exit
			}
		}
		' "${checksum_tmp}"
	)"
	if [[ -z "${expected}" ]]; then
		rm -f "${checksum_tmp}"
		fail "checksum entry for ${asset_name} not found in ${checksum_url}"
	fi

	local actual
	actual="$(sha256_file "${binary_path}")"
	rm -f "${checksum_tmp}"
	[[ "${expected}" == "${actual}" ]] || fail "checksum mismatch for ${asset_name} (expected ${expected}, got ${actual})"
}

install_binary_from_asset() {
	local asset_url="$1"
	local checksum_url="$2"
	local tmp
	tmp="$(mktemp)"

	echo "Downloading release binary: ${asset_url}"
	curl -fsSL "${asset_url}" -o "${tmp}"
	echo "Verifying checksum..."
	verify_asset_checksum "${asset_url}" "${checksum_url}" "${tmp}"
	install -m 0755 "${tmp}" "${BINARY_PATH}"
	rm -f "${tmp}"
}

extract_installed_version() {
	if [[ ! -x "${BINARY_PATH}" ]]; then
		return 0
	fi
	local info
	info="$("${BINARY_PATH}" --version-json 2>/dev/null || true)"
	printf '%s\n' "${info}" | sed -n 's/.*"version":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
}

prompt_yes_no() {
	local question="$1"
	local default_answer="$2"
	local prompt
	local answer

	if [[ "${ASSUME_YES:-0}" == "1" || "${AUTO_UPDATE:-0}" == "1" ]]; then
		return 0
	fi
	if [[ ! -r /dev/tty ]]; then
		echo "No interactive terminal available; proceeding automatically."
		return 0
	fi

	if [[ "${default_answer}" == "y" ]]; then
		prompt="[Y/n]"
	else
		prompt="[y/N]"
	fi
	while true; do
		printf '%s %s ' "${question}" "${prompt}" > /dev/tty
		IFS= read -r answer < /dev/tty || answer=""
		answer="$(printf '%s' "${answer}" | tr '[:upper:]' '[:lower:]')"
		if [[ -z "${answer}" ]]; then
			answer="${default_answer}"
		fi
		case "${answer}" in
			y|yes)
				return 0
				;;
			n|no)
				return 1
				;;
		esac
	done
}

confirm_existing_installation() {
	if [[ ! -e "${BINARY_PATH}" ]]; then
		return 0
	fi
	local installed_version
	installed_version="$(extract_installed_version)"
	if [[ -z "${installed_version}" ]]; then
		installed_version="unknown"
	fi
	echo "Existing installation detected at ${BINARY_PATH} (current: ${installed_version}, target: ${RELEASE_TAG})"

	if [[ "${installed_version}" == "${RELEASE_TAG}" ]]; then
		if [[ "${FORCE_REINSTALL:-0}" == "1" ]]; then
			return 0
		fi
		if prompt_yes_no "This version is already installed. Reinstall anyway?" "n"; then
			return 0
		fi
		echo "Keeping existing installation unchanged."
		exit 0
	fi
	if prompt_yes_no "Update split-vpn-webui from ${installed_version} to ${RELEASE_TAG}?" "y"; then
		return 0
	fi
	echo "Update cancelled by user."
	exit 0
}

resolve_uninstall_script_url() {
	local ref="main"
	if [[ -n "${VERSION:-}" ]]; then
		ref="${VERSION}"
	fi
	echo "https://raw.githubusercontent.com/${REPO}/${ref}/uninstall.sh"
}

install_uninstall_script() {
	local url
	url="$(resolve_uninstall_script_url)"
	echo "Downloading uninstall script: ${url}"
	curl -fsSL "${url}" -o "${UNINSTALL_PATH}"
	chmod 0755 "${UNINSTALL_PATH}"
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
	fetch_release_metadata
	RELEASE_ASSET_URL="$(resolve_release_asset "${arch}")"
	RELEASE_CHECKSUM_URL="$(resolve_release_checksum_asset)"
	confirm_existing_installation

	install -d -m 0700 "${DATA_DIR}"
	install -d -m 0755 "${DATA_DIR}/logs" "${UNITS_DIR}"
	install -d -m 0700 "${DATA_DIR}/vpns"
	install -d -m 0755 "/data/on_boot.d"

	install_binary_from_asset "${RELEASE_ASSET_URL}" "${RELEASE_CHECKSUM_URL}"
	install_uninstall_script
	write_service_unit
	write_boot_hook

	echo "Activating service via boot hook..."
	bash "${BOOT_SCRIPT_PATH}"

	echo
	echo "split-vpn-webui installed successfully"
	echo "Version: ${RELEASE_TAG}"
	echo "Binary: ${BINARY_PATH}"
	echo "Service unit: ${SERVICE_PATH}"
	echo "Boot hook: ${BOOT_SCRIPT_PATH}"
	echo "Uninstall script: ${UNINSTALL_PATH}"
	echo "Access URL: $(detect_access_url)"
}

main "$@"
