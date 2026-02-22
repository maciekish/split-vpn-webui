#!/bin/bash
set -euo pipefail

HOST="${HOST:-root@10.0.0.1}"
SSH_PORT="${SSH_PORT:-22}"
MODE="${MODE:-iterative}"
REMOVE_BOOT_HOOK=1

REMOTE_DATA_DIR="${REMOTE_DATA_DIR:-/data/split-vpn-webui}"
REMOTE_UNITS_DIR="${REMOTE_UNITS_DIR:-${REMOTE_DATA_DIR}/units}"
REMOTE_SERVICE_NAME="${REMOTE_SERVICE_NAME:-split-vpn-webui.service}"
REMOTE_SERVICE_LINK="/etc/systemd/system/${REMOTE_SERVICE_NAME}"
REMOTE_SERVICE_UNIT="${REMOTE_UNITS_DIR}/${REMOTE_SERVICE_NAME}"
REMOTE_UPDATER_SERVICE_NAME="${REMOTE_UPDATER_SERVICE_NAME:-split-vpn-webui-updater.service}"
REMOTE_UPDATER_SERVICE_LINK="/etc/systemd/system/${REMOTE_UPDATER_SERVICE_NAME}"
REMOTE_UPDATER_SERVICE_UNIT="${REMOTE_UNITS_DIR}/${REMOTE_UPDATER_SERVICE_NAME}"
REMOTE_BINARY="${REMOTE_DATA_DIR}/split-vpn-webui"
REMOTE_BOOT_HOOK="${REMOTE_BOOT_HOOK:-/data/on_boot.d/10-split-vpn-webui.sh}"
REMOTE_UNINSTALL_SCRIPT="${REMOTE_UNINSTALL_SCRIPT:-${REMOTE_DATA_DIR}/uninstall.sh}"
DNSMASQ_DROPIN_PRIMARY="${DNSMASQ_DROPIN_PRIMARY:-/run/dnsmasq.d/split-vpn-webui.conf}"
DNSMASQ_DROPIN_SECONDARY="${DNSMASQ_DROPIN_SECONDARY:-/run/dnsmasq.dhcp.conf.d/split-vpn-webui.conf}"
IPRULE_PRIORITY="${IPRULE_PRIORITY:-100}"

usage() {
	cat <<'EOF'
Usage: deploy/dev-uninstall.sh [options]

Removes split-vpn-webui from a development gateway over SSH.
Default host: root@10.0.0.1

Modes:
  iterative (default):
    - stops/disables split-vpn-webui service
    - removes app unit link + canonical app unit + binary
    - removes boot hook by default
    - keeps VPN profiles, VPN units, config, stats, and runtime routing state

  complete:
    - removes everything owned by split-vpn-webui
    - includes managed VPN units/profiles, config, stats/logs, runtime routing artifacts
    - uses /data/split-vpn-webui/uninstall.sh when available; otherwise uses fallback cleanup

Options:
  --host <user@host>       SSH host (default: root@10.0.0.1)
  --port <port>            SSH port (default: 22)
  --mode <iterative|complete>
  --iterative              Alias for --mode iterative
  --complete               Alias for --mode complete
  --purge-data             Backward-compatible alias for --mode complete
  --keep-boot-hook         Iterative mode only: keep /data/on_boot.d/10-split-vpn-webui.sh
  -h, --help               Show this help
EOF
}

ssh_cmd() {
	ssh -p "${SSH_PORT}" -o BatchMode=yes "${HOST}" "$@"
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
			--mode)
				MODE="$2"
				shift 2
				;;
			--iterative)
				MODE="iterative"
				shift
				;;
			--complete|--purge-data)
				MODE="complete"
				shift
				;;
			--keep-boot-hook)
				REMOVE_BOOT_HOOK=0
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

run_iterative() {
	echo "Running iterative uninstall on ${HOST}..."
	ssh_cmd \
		"REMOTE_SERVICE_NAME='${REMOTE_SERVICE_NAME}' \
		REMOTE_SERVICE_LINK='${REMOTE_SERVICE_LINK}' \
		REMOTE_SERVICE_UNIT='${REMOTE_SERVICE_UNIT}' \
		REMOTE_UPDATER_SERVICE_NAME='${REMOTE_UPDATER_SERVICE_NAME}' \
		REMOTE_UPDATER_SERVICE_LINK='${REMOTE_UPDATER_SERVICE_LINK}' \
		REMOTE_UPDATER_SERVICE_UNIT='${REMOTE_UPDATER_SERVICE_UNIT}' \
		REMOTE_BINARY='${REMOTE_BINARY}' \
		REMOTE_BOOT_HOOK='${REMOTE_BOOT_HOOK}' \
		REMOVE_BOOT_HOOK='${REMOVE_BOOT_HOOK}' \
		bash -s" <<'EOF'
set -euo pipefail

if command -v systemctl >/dev/null 2>&1; then
	systemctl stop "${REMOTE_SERVICE_NAME}" >/dev/null 2>&1 || true
	systemctl disable "${REMOTE_SERVICE_NAME}" >/dev/null 2>&1 || true
	systemctl stop "${REMOTE_UPDATER_SERVICE_NAME}" >/dev/null 2>&1 || true
	systemctl disable "${REMOTE_UPDATER_SERVICE_NAME}" >/dev/null 2>&1 || true
fi

rm -f "${REMOTE_SERVICE_LINK}" "${REMOTE_SERVICE_UNIT}" "${REMOTE_UPDATER_SERVICE_LINK}" "${REMOTE_UPDATER_SERVICE_UNIT}" "${REMOTE_BINARY}"
if [[ "${REMOVE_BOOT_HOOK}" == "1" ]]; then
	rm -f "${REMOTE_BOOT_HOOK}"
fi

if command -v systemctl >/dev/null 2>&1; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi
EOF
}

run_complete_via_remote_uninstall() {
	if ! ssh_cmd "[ -x '${REMOTE_UNINSTALL_SCRIPT}' ]"; then
		return 1
	fi

	echo "Running complete uninstall via remote ${REMOTE_UNINSTALL_SCRIPT}..."
	ssh_cmd "printf 'y\n' | '${REMOTE_UNINSTALL_SCRIPT}'"
	return 0
}

run_complete_fallback() {
	echo "Remote uninstall script not found; using fallback complete uninstall..."
	ssh_cmd \
		"REMOTE_DATA_DIR='${REMOTE_DATA_DIR}' \
		REMOTE_UNITS_DIR='${REMOTE_UNITS_DIR}' \
		REMOTE_SERVICE_NAME='${REMOTE_SERVICE_NAME}' \
		REMOTE_SERVICE_LINK='${REMOTE_SERVICE_LINK}' \
		REMOTE_SERVICE_UNIT='${REMOTE_SERVICE_UNIT}' \
		REMOTE_UPDATER_SERVICE_NAME='${REMOTE_UPDATER_SERVICE_NAME}' \
		REMOTE_UPDATER_SERVICE_LINK='${REMOTE_UPDATER_SERVICE_LINK}' \
		REMOTE_UPDATER_SERVICE_UNIT='${REMOTE_UPDATER_SERVICE_UNIT}' \
		REMOTE_BINARY='${REMOTE_BINARY}' \
		REMOTE_BOOT_HOOK='${REMOTE_BOOT_HOOK}' \
		DNSMASQ_DROPIN_PRIMARY='${DNSMASQ_DROPIN_PRIMARY}' \
		DNSMASQ_DROPIN_SECONDARY='${DNSMASQ_DROPIN_SECONDARY}' \
		IPRULE_PRIORITY='${IPRULE_PRIORITY}' \
		bash -s" <<'EOF'
set -euo pipefail

SYSTEMD_DIR="/etc/systemd/system"

safe_systemctl() {
	if ! command -v systemctl >/dev/null 2>&1; then
		return 0
	fi
	systemctl "$@" >/dev/null 2>&1 || true
}

cleanup_chain() {
	local tool="$1"
	local table="$2"
	local chain="$3"
	local parent="$4"
	if ! command -v "${tool}" >/dev/null 2>&1; then
		return
	fi
	while "${tool}" -t "${table}" -D "${parent}" -j "${chain}" >/dev/null 2>&1; do :; done
	"${tool}" -t "${table}" -F "${chain}" >/dev/null 2>&1 || true
	"${tool}" -t "${table}" -X "${chain}" >/dev/null 2>&1 || true
}

cleanup_ip_rules_family() {
	local family="$1"
	local args_show=("rule" "show")
	local args_prefix=()
	if [[ "${family}" == "ipv6" ]]; then
		args_show=("-6" "rule" "show")
		args_prefix=("-6")
	fi
	local lines
	lines="$(ip "${args_show[@]}" 2>/dev/null || true)"
	if [[ -z "${lines}" ]]; then
		return
	fi
	while IFS= read -r line; do
		[[ -z "${line}" ]] && continue
		local priority=""
		local mark_token=""
		local table_id=""
		if [[ "${line}" =~ ^([0-9]+): ]]; then
			priority="${BASH_REMATCH[1]}"
		fi
		if [[ "${line}" =~ fwmark[[:space:]]+([^[:space:]/]+) ]]; then
			mark_token="${BASH_REMATCH[1]}"
		fi
		if [[ "${line}" =~ (lookup|table)[[:space:]]+([0-9]+) ]]; then
			table_id="${BASH_REMATCH[2]}"
		fi
		[[ -z "${priority}" || -z "${mark_token}" || -z "${table_id}" ]] && continue
		[[ "${priority}" != "${IPRULE_PRIORITY}" ]] && continue
		local mark_num
		mark_num=$((mark_token)) || continue
		if [[ "${mark_num}" -lt 200 || "${table_id}" -lt 200 ]]; then
			continue
		fi
		local mark_hex
		printf -v mark_hex '0x%x' "${mark_num}"
		while ip "${args_prefix[@]}" rule del fwmark "${mark_hex}" table "${table_id}" priority "${IPRULE_PRIORITY}" >/dev/null 2>&1; do :; done
	done <<< "${lines}"
}

safe_systemctl stop "${REMOTE_SERVICE_NAME}"
safe_systemctl disable "${REMOTE_SERVICE_NAME}"
safe_systemctl stop "${REMOTE_UPDATER_SERVICE_NAME}"
safe_systemctl disable "${REMOTE_UPDATER_SERVICE_NAME}"

shopt -s nullglob
for unit in "${REMOTE_UNITS_DIR}"/svpn-*.service "${SYSTEMD_DIR}"/svpn-*.service; do
	[[ -f "${unit}" || -L "${unit}" ]] || continue
	unit_name="$(basename "${unit}")"
	safe_systemctl stop "${unit_name}"
	safe_systemctl disable "${unit_name}"
	rm -f "${SYSTEMD_DIR}/${unit_name}" "${REMOTE_UNITS_DIR}/${unit_name}"
done
shopt -u nullglob

rm -f "${REMOTE_SERVICE_LINK}" "${REMOTE_SERVICE_UNIT}" "${REMOTE_UPDATER_SERVICE_LINK}" "${REMOTE_UPDATER_SERVICE_UNIT}" "${REMOTE_BINARY}" "${REMOTE_BOOT_HOOK}"

cleanup_chain "iptables" "mangle" "SVPN_MARK" "PREROUTING"
cleanup_chain "iptables" "nat" "SVPN_NAT" "POSTROUTING"
cleanup_chain "ip6tables" "mangle" "SVPN_MARK" "PREROUTING"
cleanup_chain "ip6tables" "nat" "SVPN_NAT" "POSTROUTING"

if command -v ip >/dev/null 2>&1; then
	cleanup_ip_rules_family "ipv4"
	cleanup_ip_rules_family "ipv6"
fi

if command -v ipset >/dev/null 2>&1; then
	while IFS= read -r set_name; do
		[[ -z "${set_name}" ]] && continue
		case "${set_name}" in
			svpn_*)
				ipset destroy "${set_name}" >/dev/null 2>&1 || true
				;;
		esac
	done < <(ipset list -name 2>/dev/null || true)
fi

dnsmasq_changed=0
if [[ -e "${DNSMASQ_DROPIN_PRIMARY}" ]]; then
	rm -f "${DNSMASQ_DROPIN_PRIMARY}"
	dnsmasq_changed=1
fi
if [[ -e "${DNSMASQ_DROPIN_SECONDARY}" ]]; then
	rm -f "${DNSMASQ_DROPIN_SECONDARY}"
	dnsmasq_changed=1
fi

safe_systemctl daemon-reload
if [[ "${dnsmasq_changed}" -eq 1 ]]; then
	safe_systemctl reload dnsmasq
	safe_systemctl restart dnsmasq
fi

rm -rf "${REMOTE_DATA_DIR}"
EOF
}

print_summary() {
	echo
	echo "dev-uninstall complete"
	echo "  host: ${HOST}"
	echo "  mode: ${MODE}"
	if [[ "${MODE}" == "iterative" ]]; then
		if [[ "${REMOVE_BOOT_HOOK}" -eq 1 ]]; then
			echo "  removed: app service link/unit, binary, boot hook"
		else
			echo "  removed: app service link/unit, binary"
		fi
		echo "  kept: VPN profiles, VPN units, config, stats, runtime routing state"
	else
		echo "  removed: full split-vpn-webui app namespace (units, VPNs, config, stats, runtime artifacts)"
	fi
	echo "  no reboot performed"
}

main() {
	parse_args "$@"
	case "${MODE}" in
		iterative|complete)
			;;
		*)
			echo "error: invalid mode: ${MODE} (expected iterative or complete)" >&2
			exit 1
			;;
	esac

	if [[ "${MODE}" == "iterative" ]]; then
		run_iterative
	else
		if ! run_complete_via_remote_uninstall; then
			run_complete_fallback
		fi
	fi
	print_summary
}

main "$@"
