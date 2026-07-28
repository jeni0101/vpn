#!/usr/bin/env bash

set -Eeuo pipefail

RULES_FILE="/etc/nftables.d/personal-vpn.nft"
VPN_INTERFACE="@@VPN_INTERFACE@@"
WAN_INTERFACE="@@WAN_INTERFACE@@"

validate_interface_names() {
    [[ "${VPN_INTERFACE}" =~ ^[A-Za-z0-9_.:-]+$ &&
       "${WAN_INTERFACE}" =~ ^[A-Za-z0-9_.:-]+$ ]] || {
        printf 'Invalid rendered network interface\n' >&2
        exit 1
    }
}

remove_tables() {
    local family
    local table_name
    while read -r family table_name; do
        if nft list table "${family}" "${table_name}" >/dev/null 2>&1; then
            nft delete table "${family}" "${table_name}"
        fi
    done <<'EOF'
inet personal_vpn_filter
ip personal_vpn_nat4
ip6 personal_vpn_nat6
EOF
}

docker_is_active() {
    systemctl is-active --quiet docker.service 2>/dev/null
}

docker_chain_exists() {
    local command_name="$1"

    command -v "${command_name}" >/dev/null 2>&1 &&
        "${command_name}" -w -S DOCKER-USER >/dev/null 2>&1
}

add_docker_rule() {
    local command_name="$1"
    shift

    if ! "${command_name}" -w -C DOCKER-USER "$@" >/dev/null 2>&1; then
        "${command_name}" -w -I DOCKER-USER 1 "$@"
    fi
}

remove_docker_rule() {
    local command_name="$1"
    shift

    docker_chain_exists "${command_name}" || return 0
    while "${command_name}" -w -C DOCKER-USER "$@" >/dev/null 2>&1; do
        "${command_name}" -w -D DOCKER-USER "$@"
    done
}

apply_docker_rules_for_family() {
    local command_name="$1"

    docker_chain_exists "${command_name}" || {
        printf 'Active Docker is missing %s DOCKER-USER\n' "${command_name}" >&2
        return 1
    }

    # Insert in reverse order so peer isolation remains the first rule.
    add_docker_rule "${command_name}" \
        -o "${VPN_INTERFACE}" \
        -m comment --comment "personal-vpn:peer-ingress-drop" \
        -j DROP
    add_docker_rule "${command_name}" \
        -o "${VPN_INTERFACE}" \
        -m conntrack --ctstate ESTABLISHED,RELATED \
        -m comment --comment "personal-vpn:peer-return" \
        -j ACCEPT
    add_docker_rule "${command_name}" \
        -i "${VPN_INTERFACE}" -o "${WAN_INTERFACE}" \
        -m comment --comment "personal-vpn:internet-egress" \
        -j ACCEPT
    add_docker_rule "${command_name}" \
        -i "${VPN_INTERFACE}" -o "${VPN_INTERFACE}" \
        -m comment --comment "personal-vpn:peer-isolation" \
        -j DROP
}

remove_docker_rules_for_family() {
    local command_name="$1"

    remove_docker_rule "${command_name}" \
        -i "${VPN_INTERFACE}" -o "${VPN_INTERFACE}" \
        -m comment --comment "personal-vpn:peer-isolation" \
        -j DROP
    remove_docker_rule "${command_name}" \
        -i "${VPN_INTERFACE}" -o "${WAN_INTERFACE}" \
        -m comment --comment "personal-vpn:internet-egress" \
        -j ACCEPT
    remove_docker_rule "${command_name}" \
        -o "${VPN_INTERFACE}" \
        -m conntrack --ctstate ESTABLISHED,RELATED \
        -m comment --comment "personal-vpn:peer-return" \
        -j ACCEPT
    remove_docker_rule "${command_name}" \
        -o "${VPN_INTERFACE}" \
        -m comment --comment "personal-vpn:peer-ingress-drop" \
        -j DROP
}

apply_docker_rules() {
    docker_is_active || return 0

    apply_docker_rules_for_family iptables &&
        apply_docker_rules_for_family ip6tables
}

remove_docker_rules() {
    remove_docker_rules_for_family iptables
    remove_docker_rules_for_family ip6tables
}

validate_interface_names

case "${1:-}" in
    apply)
        [[ -r "${RULES_FILE}" ]] || {
            printf 'Missing rules file: %s\n' "${RULES_FILE}" >&2
            exit 1
        }
        remove_tables
        nft -f "${RULES_FILE}"
        if ! apply_docker_rules; then
            remove_docker_rules
            remove_tables
            exit 1
        fi
        ;;
    remove)
        remove_docker_rules
        remove_tables
        ;;
    *)
        printf 'Usage: %s <apply|remove>\n' "$0" >&2
        exit 2
        ;;
esac
