#!/usr/bin/env bash

set -Eeuo pipefail

VPN_INTERFACE="${1:?VPN interface is required}"
EXPECTED_TCP_PORTS="${2:-}"
EXPECTED_UDP_PORTS="${3:-}"

fail() {
    printf 'POSTCHECK_ERROR=%s\n' "$*" >&2
    exit 1
}

port_is_listening() {
    local protocol="$1"
    local port="$2"
    local ss_flag

    if [[ "${protocol}" == "tcp" ]]; then
        ss_flag="-ltn"
    else
        ss_flag="-lun"
    fi

    ss -H "${ss_flag}" |
        awk -v wanted="${port}" '
            {
                endpoint = $4
                sub(/^.*:/, "", endpoint)
                if (endpoint == wanted) {
                    found = 1
                }
            }
            END { exit(found ? 0 : 1) }
        '
}

check_port_list() {
    local protocol="$1"
    local csv="$2"
    local port
    local -a ports=()

    [[ -n "${csv}" ]] || return 0
    IFS=',' read -r -a ports <<<"${csv}"
    for port in "${ports[@]}"; do
        [[ "${port}" =~ ^[0-9]+$ ]] ||
            fail "Unsafe ${protocol} port in preflight result"
        port_is_listening "${protocol}" "${port}" ||
            fail "Website ${protocol} port ${port} stopped listening"
    done
}

[[ "$(sysctl -n net.ipv4.ip_forward)" == "1" ]] ||
    fail "IPv4 forwarding is disabled"
[[ "$(sysctl -n net.ipv6.conf.all.forwarding)" == "1" ]] ||
    fail "IPv6 forwarding is disabled"

systemctl is-active --quiet "wg-quick@${VPN_INTERFACE}.service" ||
    fail "WireGuard service is not active"
systemctl is-active --quiet personal-vpn-firewall.service ||
    fail "Personal VPN firewall service is not active"
wg show "${VPN_INTERFACE}" >/dev/null ||
    fail "WireGuard interface is unavailable"

for spec in \
    "inet personal_vpn_filter" \
    "ip personal_vpn_nat4" \
    "ip6 personal_vpn_nat6"; do
    read -r family table_name <<<"${spec}"
    nft list table "${family}" "${table_name}" >/dev/null 2>&1 ||
        fail "Missing nftables table: ${family} ${table_name}"
done

check_port_list tcp "${EXPECTED_TCP_PORTS}"
check_port_list udp "${EXPECTED_UDP_PORTS}"

printf 'POSTCHECK=ok\n'
