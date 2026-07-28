#!/usr/bin/env bash

set -Eeuo pipefail

VPN_INTERFACE="${1:?VPN interface is required}"
WAN_INTERFACE="${2:?WAN interface is required}"
DOCKER_INTEGRATION="${3:?Docker integration mode is required}"
EXPECTED_TCP_PORTS="${4:-}"
EXPECTED_UDP_PORTS="${5:-}"

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

check_docker_rule() {
    local command_name="$1"
    shift

    "${command_name}" -w -C DOCKER-USER "$@" >/dev/null 2>&1 ||
        fail "Missing Docker compatibility rule: $*"
}

check_docker_rules_for_family() {
    local command_name="$1"

    command -v "${command_name}" >/dev/null 2>&1 ||
        fail "Missing Docker firewall command: ${command_name}"
    "${command_name}" -w -C FORWARD -j DOCKER-USER >/dev/null 2>&1 ||
        fail "${command_name} FORWARD no longer calls DOCKER-USER"
    check_docker_rule "${command_name}" \
        -i "${VPN_INTERFACE}" -o "${VPN_INTERFACE}" \
        -m comment --comment "personal-vpn:peer-isolation" \
        -j DROP
    check_docker_rule "${command_name}" \
        -i "${VPN_INTERFACE}" -o "${WAN_INTERFACE}" \
        -m comment --comment "personal-vpn:internet-egress" \
        -j ACCEPT
    check_docker_rule "${command_name}" \
        -o "${VPN_INTERFACE}" \
        -m conntrack --ctstate ESTABLISHED,RELATED \
        -m comment --comment "personal-vpn:peer-return" \
        -j ACCEPT
    check_docker_rule "${command_name}" \
        -o "${VPN_INTERFACE}" \
        -m comment --comment "personal-vpn:peer-ingress-drop" \
        -j DROP
}

[[ "${WAN_INTERFACE}" =~ ^[A-Za-z0-9_.:-]+$ ]] ||
    fail "Unsafe WAN interface"
[[ "${DOCKER_INTEGRATION}" == "yes" ||
   "${DOCKER_INTEGRATION}" == "no" ]] ||
    fail "Unsafe Docker integration mode"

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

if [[ "${DOCKER_INTEGRATION}" == "yes" ]]; then
    systemctl is-active --quiet docker.service ||
        fail "Docker stopped during deployment"
    check_docker_rules_for_family iptables
    check_docker_rules_for_family ip6tables
fi

check_port_list tcp "${EXPECTED_TCP_PORTS}"
check_port_list udp "${EXPECTED_UDP_PORTS}"

printf 'POSTCHECK=ok\n'
