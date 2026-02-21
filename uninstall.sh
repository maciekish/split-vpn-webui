#!/bin/bash
set -euo pipefail

DATA_DIR="${DATA_DIR:-/data/split-vpn-webui}"
UNITS_DIR="${UNITS_DIR:-${DATA_DIR}/units}"
VPNS_DIR="${VPNS_DIR:-${DATA_DIR}/vpns}"
LOGS_DIR="${LOGS_DIR:-${DATA_DIR}/logs}"
STATS_DB="${STATS_DB:-${DATA_DIR}/stats.db}"
SETTINGS_FILE="${SETTINGS_FILE:-${DATA_DIR}/settings.json}"
BOOT_HOOK="${BOOT_HOOK:-/data/on_boot.d/10-split-vpn-webui.sh}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SERVICE_NAME="${SERVICE_NAME:-split-vpn-webui.service}"
SERVICE_SYMLINK="${SYSTEMD_DIR}/${SERVICE_NAME}"
BINARY_PATH="${BINARY_PATH:-${DATA_DIR}/split-vpn-webui}"
UNINSTALL_PATH="${UNINSTALL_PATH:-${DATA_DIR}/uninstall.sh}"
DNSMASQ_DROPIN_PRIMARY="${DNSMASQ_DROPIN_PRIMARY:-/run/dnsmasq.d/split-vpn-webui.conf}"
DNSMASQ_DROPIN_SECONDARY="${DNSMASQ_DROPIN_SECONDARY:-/run/dnsmasq.dhcp.conf.d/split-vpn-webui.conf}"
IPRULE_PRIORITY="${IPRULE_PRIORITY:-100}"

removed_items=()
kept_items=()
daemon_reload_required=0

add_removed() {
	removed_items+=("$1")
}

add_kept() {
	kept_items+=("$1")
}

is_yes() {
	case "${1:-}" in
		y|Y|yes|YES|Yes)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

prompt_yes_no() {
	local prompt="$1"
	local answer=""
	read -r -p "${prompt} " answer || true
	is_yes "${answer}"
}

remove_path() {
	local path="$1"
	if [[ -e "${path}" || -L "${path}" ]]; then
		rm -rf -- "${path}"
		return 0
	fi
	return 1
}

safe_systemctl() {
	if ! command -v systemctl >/dev/null 2>&1; then
		return 0
	fi
	systemctl "$@" >/dev/null 2>&1 || true
}

remove_binaries_category() {
	safe_systemctl stop "${SERVICE_NAME}"
	safe_systemctl disable "${SERVICE_NAME}"

	if remove_path "${BINARY_PATH}"; then
		add_removed "Binary removed (${BINARY_PATH})"
	else
		add_kept "Binary not present (${BINARY_PATH})"
	fi

	if remove_path "${UNITS_DIR}/${SERVICE_NAME}"; then
		add_removed "Canonical app unit removed (${UNITS_DIR}/${SERVICE_NAME})"
		daemon_reload_required=1
	else
		add_kept "Canonical app unit kept (not present)"
	fi

	if remove_path "${SERVICE_SYMLINK}"; then
		add_removed "App unit symlink removed (${SERVICE_SYMLINK})"
		daemon_reload_required=1
	else
		add_kept "App unit symlink kept (not present)"
	fi
}

remove_vpns_units_category() {
	local matched=0
	local unit_path=""
	shopt -s nullglob
	for unit_path in "${UNITS_DIR}"/svpn-*.service; do
		matched=1
		local unit_name
		unit_name="$(basename "${unit_path}")"
		safe_systemctl stop "${unit_name}"
		safe_systemctl disable "${unit_name}"
		if remove_path "${unit_path}"; then
			add_removed "VPN unit removed (${unit_path})"
			daemon_reload_required=1
		fi
		if remove_path "${SYSTEMD_DIR}/${unit_name}"; then
			add_removed "VPN unit symlink removed (${SYSTEMD_DIR}/${unit_name})"
			daemon_reload_required=1
		fi
	done

	for unit_path in "${SYSTEMD_DIR}"/svpn-*.service; do
		matched=1
		local unit_name
		unit_name="$(basename "${unit_path}")"
		safe_systemctl stop "${unit_name}"
		safe_systemctl disable "${unit_name}"
		if remove_path "${unit_path}"; then
			add_removed "VPN unit symlink removed (${unit_path})"
			daemon_reload_required=1
		fi
	done
	shopt -u nullglob

	if [[ "${matched}" -eq 0 ]]; then
		add_kept "No managed VPN units found under ${UNITS_DIR}"
	fi

	if [[ -d "${VPNS_DIR}" ]]; then
		shopt -s nullglob dotglob
		local vpn_entries=("${VPNS_DIR}"/*)
		if [[ "${#vpn_entries[@]}" -gt 0 ]]; then
			rm -rf -- "${vpn_entries[@]}"
			add_removed "VPN profiles removed (${VPNS_DIR}/*)"
		else
			add_kept "No VPN profiles found under ${VPNS_DIR}"
		fi
		shopt -u nullglob dotglob
	else
		add_kept "VPN profile directory not present (${VPNS_DIR})"
	fi
}

remove_config_category() {
	if remove_path "${SETTINGS_FILE}"; then
		add_removed "Settings removed (${SETTINGS_FILE})"
	else
		add_kept "Settings not present (${SETTINGS_FILE})"
	fi

	if remove_path "${BOOT_HOOK}"; then
		add_removed "Boot hook removed (${BOOT_HOOK})"
	else
		add_kept "Boot hook not present (${BOOT_HOOK})"
	fi
}

remove_statistics_category() {
	if remove_path "${STATS_DB}"; then
		add_removed "Statistics database removed (${STATS_DB})"
	else
		add_kept "Statistics database not present (${STATS_DB})"
	fi

	if [[ -d "${LOGS_DIR}" ]]; then
		shopt -s nullglob dotglob
		local log_entries=("${LOGS_DIR}"/*)
		if [[ "${#log_entries[@]}" -gt 0 ]]; then
			rm -rf -- "${log_entries[@]}"
			add_removed "Statistics logs removed (${LOGS_DIR}/*)"
		else
			add_kept "No log files found under ${LOGS_DIR}"
		fi
		shopt -u nullglob dotglob
	else
		add_kept "Log directory not present (${LOGS_DIR})"
	fi
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

cleanup_runtime_routing() {
	cleanup_chain "iptables" "mangle" "SVPN_MARK" "PREROUTING"
	cleanup_chain "iptables" "nat" "SVPN_NAT" "POSTROUTING"
	cleanup_chain "ip6tables" "mangle" "SVPN_MARK" "PREROUTING"
	cleanup_chain "ip6tables" "nat" "SVPN_NAT" "POSTROUTING"

	if command -v ip >/dev/null 2>&1; then
		cleanup_ip_rules_family "ipv4"
		cleanup_ip_rules_family "ipv6"
	fi

	if command -v ipset >/dev/null 2>&1; then
		local set_name=""
		while IFS= read -r set_name; do
			[[ -z "${set_name}" ]] && continue
			case "${set_name}" in
				svpn_*)
					ipset destroy "${set_name}" >/dev/null 2>&1 || true
					;;
			esac
		done < <(ipset list -name 2>/dev/null || true)
	fi

	local removed_dnsmasq=0
	if remove_path "${DNSMASQ_DROPIN_PRIMARY}"; then
		removed_dnsmasq=1
	fi
	if remove_path "${DNSMASQ_DROPIN_SECONDARY}"; then
		removed_dnsmasq=1
	fi
	if [[ "${removed_dnsmasq}" -eq 1 ]]; then
		safe_systemctl reload dnsmasq
		safe_systemctl restart dnsmasq
		add_removed "Runtime routing artifacts removed (SVPN chains/rules/ipsets + dnsmasq drop-in)"
	else
		add_removed "Runtime routing artifacts removed (SVPN chains/rules/ipsets)"
	fi
}

print_summary() {
	echo
	echo "Uninstall summary"
	echo "================="
	echo "Removed:"
	if [[ "${#removed_items[@]}" -eq 0 ]]; then
		echo "  - nothing"
	else
		local item=""
		for item in "${removed_items[@]}"; do
			echo "  - ${item}"
		done
	fi
	echo
	echo "Kept:"
	if [[ "${#kept_items[@]}" -eq 0 ]]; then
		echo "  - nothing"
	else
		local kept=""
		for kept in "${kept_items[@]}"; do
			echo "  - ${kept}"
		done
	fi
}

main() {
	if [[ "${SKIP_ROOT_CHECK:-0}" != "1" && "${EUID}" -ne 0 ]]; then
		echo "error: uninstall.sh must run as root" >&2
		exit 1
	fi

	echo "split-vpn-webui uninstall"
	echo "This script only removes split-vpn-webui resources in its own namespace."
	echo "It never touches /data/split-vpn (peacey/unifi managed resources)."
	echo

	local remove_all=0
	local remove_binaries=0
	local remove_vpns_units=0
	local remove_config=0
	local remove_stats=0

	if prompt_yes_no "Remove EVERYTHING related to split-vpn-webui? [y/N]"; then
		remove_all=1
		remove_binaries=1
		remove_vpns_units=1
		remove_config=1
		remove_stats=1
	else
		if prompt_yes_no "Remove binaries? [y/N]"; then
			remove_binaries=1
		fi
		if prompt_yes_no "Remove VPNs + their systemd units? [y/N]"; then
			remove_vpns_units=1
		fi
		if prompt_yes_no "Remove config files? [y/N]"; then
			remove_config=1
		fi
		if prompt_yes_no "Remove statistics data? [y/N]"; then
			remove_stats=1
		fi
	fi

	if [[ "${remove_binaries}" -eq 1 ]]; then
		remove_binaries_category
	else
		add_kept "Binaries category"
	fi

	if [[ "${remove_vpns_units}" -eq 1 ]]; then
		remove_vpns_units_category
	else
		add_kept "VPNs + units category"
	fi

	if [[ "${remove_config}" -eq 1 ]]; then
		remove_config_category
	else
		add_kept "Config files category"
	fi

	if [[ "${remove_stats}" -eq 1 ]]; then
		remove_statistics_category
	else
		add_kept "Statistics data category"
	fi

	if [[ "${remove_all}" -eq 1 || "${remove_vpns_units}" -eq 1 || "${remove_config}" -eq 1 ]]; then
		cleanup_runtime_routing
	else
		add_kept "Runtime routing artifacts"
	fi

	if [[ "${remove_all}" -eq 1 ]]; then
		if remove_path "${DATA_DIR}"; then
			add_removed "Data directory removed (${DATA_DIR})"
		else
			add_kept "Data directory not present (${DATA_DIR})"
		fi
	else
		add_kept "Data directory root (${DATA_DIR})"
	fi

	if [[ "${daemon_reload_required}" -eq 1 ]]; then
		safe_systemctl daemon-reload
		add_removed "systemd daemon-reload executed"
	fi

	if [[ "${remove_all}" -eq 0 && "${remove_config}" -eq 0 ]]; then
		add_kept "Boot hook (${BOOT_HOOK})"
	fi
	if [[ "${remove_all}" -eq 0 ]]; then
		add_kept "Uninstall script (${UNINSTALL_PATH})"
	fi

	print_summary
}

main "$@"
