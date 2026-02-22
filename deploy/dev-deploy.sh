#!/bin/bash
set -euo pipefail

HOST="${HOST:-root@10.0.0.1}"
SSH_PORT="${SSH_PORT:-22}"
CMD_PATH="${CMD_PATH:-./cmd/splitvpnwebui}"
BUILD_DIR="${BUILD_DIR:-./dist}"
LOCAL_BIN="${LOCAL_BIN:-${BUILD_DIR}/split-vpn-webui-dev}"
REMOTE_DATA_DIR="${REMOTE_DATA_DIR:-/data/split-vpn-webui}"
REMOTE_BINARY="${REMOTE_DATA_DIR}/split-vpn-webui"
REMOTE_UNITS_DIR="${REMOTE_DATA_DIR}/units"
REMOTE_SERVICE_NAME="${REMOTE_SERVICE_NAME:-split-vpn-webui.service}"
REMOTE_SERVICE_UNIT="${REMOTE_UNITS_DIR}/${REMOTE_SERVICE_NAME}"
REMOTE_SERVICE_LINK="/etc/systemd/system/${REMOTE_SERVICE_NAME}"
REMOTE_BOOT_HOOK="${REMOTE_BOOT_HOOK:-/data/on_boot.d/10-split-vpn-webui.sh}"
REMOTE_UNINSTALL="${REMOTE_UNINSTALL:-${REMOTE_DATA_DIR}/uninstall.sh}"

DO_BUILD=1
DO_RESTART=1
COPY_BOOT_HOOK=1
COPY_UNINSTALL=1
GOARCH_OVERRIDE=""

usage() {
	cat <<'EOF'
Usage: deploy/dev-deploy.sh [options]

Fast iterative deploy to a UniFi gateway over SSH/SCP.
By default this script:
  - builds split-vpn-webui for remote architecture
  - copies binary + canonical app systemd unit
  - links /etc/systemd/system/split-vpn-webui.service
  - daemon-reload + restart split-vpn-webui.service

Options:
  --host <user@host>       SSH host (default: root@10.0.0.1)
  --port <port>            SSH port (default: 22)
  --goarch <arch>          Override GOARCH for build (amd64|arm64)
  --local-bin <path>       Local binary path (default: ./dist/split-vpn-webui-dev)
  --no-build               Skip local build and deploy existing --local-bin
  --no-restart             Do not restart service after deploy
  --copy-boot-hook         Also copy deploy/on_boot_hook.sh to /data/on_boot.d/10-split-vpn-webui.sh
  --copy-uninstall         Also copy uninstall.sh to /data/split-vpn-webui/uninstall.sh
  -h, --help               Show this help
EOF
}

need_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command not found: $1" >&2
		exit 1
	fi
}

ssh_cmd() {
	ssh -p "${SSH_PORT}" -o BatchMode=yes "${HOST}" "$@"
}

scp_cmd() {
	scp -P "${SSH_PORT}" "$@"
}

detect_remote_goarch() {
	local machine
	machine="$(ssh_cmd "uname -m" | tr -d '\r\n')"
	case "${machine}" in
		x86_64|amd64)
			echo "amd64"
			;;
		aarch64|arm64)
			echo "arm64"
			;;
		*)
			echo "error: unsupported remote architecture: ${machine}" >&2
			exit 1
			;;
	esac
}

parse_args() {
	while [[ $# -gt 0 ]]; do
		case "$1" in
			--host)
				HOST="$2"
				shift 2
				;;
			--port)
				SSH_PORT="$2"
				shift 2
				;;
			--goarch)
				GOARCH_OVERRIDE="$2"
				shift 2
				;;
			--local-bin)
				LOCAL_BIN="$2"
				shift 2
				;;
			--no-build)
				DO_BUILD=0
				shift
				;;
			--no-restart)
				DO_RESTART=0
				shift
				;;
			--copy-boot-hook)
				COPY_BOOT_HOOK=1
				shift
				;;
			--copy-uninstall)
				COPY_UNINSTALL=1
				shift
				;;
			-h|--help)
				usage
				exit 0
				;;
			*)
				echo "error: unknown argument: $1" >&2
				usage
				exit 1
				;;
		esac
	done
}

build_binary() {
	local goarch="$1"
	mkdir -p "$(dirname "${LOCAL_BIN}")"
	echo "Building binary for linux/${goarch}..."
	GOOS=linux GOARCH="${goarch}" CGO_ENABLED=0 go build -o "${LOCAL_BIN}" "${CMD_PATH}"
	chmod 0755 "${LOCAL_BIN}"
}

copy_files() {
	echo "Ensuring remote directories..."
	ssh_cmd "
		set -euo pipefail
		install -d -m 0700 '${REMOTE_DATA_DIR}'
		install -d -m 0755 '${REMOTE_DATA_DIR}/logs' '${REMOTE_UNITS_DIR}'
		install -d -m 0700 '${REMOTE_DATA_DIR}/vpns'
		install -d -m 0755 '/data/on_boot.d'
	"

	echo "Copying binary and systemd unit to ${HOST}..."
	scp_cmd "${LOCAL_BIN}" "${HOST}:${REMOTE_BINARY}"
	scp_cmd "./deploy/split-vpn-webui.service" "${HOST}:${REMOTE_SERVICE_UNIT}"

	if [[ "${COPY_BOOT_HOOK}" -eq 1 ]]; then
		echo "Copying boot hook..."
		scp_cmd "./deploy/on_boot_hook.sh" "${HOST}:${REMOTE_BOOT_HOOK}"
	fi

	if [[ "${COPY_UNINSTALL}" -eq 1 ]]; then
		echo "Copying uninstall script..."
		scp_cmd "./uninstall.sh" "${HOST}:${REMOTE_UNINSTALL}"
	fi
}

configure_remote() {
	echo "Applying permissions and linking service..."
	ssh_cmd "
		set -euo pipefail
		chmod 0755 '${REMOTE_BINARY}'
		chmod 0644 '${REMOTE_SERVICE_UNIT}'
		ln -sf '${REMOTE_SERVICE_UNIT}' '${REMOTE_SERVICE_LINK}'
		systemctl daemon-reload
	"

	if [[ "${COPY_BOOT_HOOK}" -eq 1 ]]; then
		ssh_cmd "chmod 0755 '${REMOTE_BOOT_HOOK}'"
	fi

	if [[ "${COPY_UNINSTALL}" -eq 1 ]]; then
		ssh_cmd "chmod 0755 '${REMOTE_UNINSTALL}'"
	fi

	if [[ "${DO_RESTART}" -eq 1 ]]; then
		echo "Restarting ${REMOTE_SERVICE_NAME}..."
		ssh_cmd "systemctl restart '${REMOTE_SERVICE_NAME}'"
	else
		echo "Skipping service restart (--no-restart)."
	fi
}

show_status() {
	local active
	active="$(ssh_cmd "systemctl is-active '${REMOTE_SERVICE_NAME}' 2>/dev/null || true" | tr -d '\r\n')"
	echo
	echo "Deploy complete"
	echo "  host: ${HOST}"
	echo "  binary: ${REMOTE_BINARY}"
	echo "  unit: ${REMOTE_SERVICE_UNIT}"
	echo "  service active: ${active:-unknown}"
	echo "  status check: ssh -p ${SSH_PORT} ${HOST} 'systemctl status ${REMOTE_SERVICE_NAME} --no-pager'"
}

main() {
	parse_args "$@"

	need_cmd ssh
	need_cmd scp
	need_cmd go

	local goarch="${GOARCH_OVERRIDE}"
	if [[ "${DO_BUILD}" -eq 1 ]]; then
		if [[ -z "${goarch}" ]]; then
			goarch="$(detect_remote_goarch)"
		fi
		build_binary "${goarch}"
	fi

	if [[ ! -f "${LOCAL_BIN}" ]]; then
		echo "error: local binary not found: ${LOCAL_BIN}" >&2
		exit 1
	fi

	copy_files
	configure_remote
	show_status
}

main "$@"
