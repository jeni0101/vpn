#!/usr/bin/env bash

set -Eeuo pipefail

config_init() {
    ensure_runtime_dirs

    if [[ -e "${CONFIG_FILE}" ]]; then
        die "Configuration already exists: ${CONFIG_FILE}"
    fi

    install -m 0600 "${ROOT_DIR}/config/local.env.example" "${CONFIG_FILE}"
    log "Created ${CONFIG_FILE}"
    log "Edit the file, then run: scripts/vpnctl server preflight"
}

load_config() {
    [[ -f "${CONFIG_FILE}" ]] ||
        die "Missing ${CONFIG_FILE}; run: scripts/vpnctl config init"

    # shellcheck source=/dev/null
    source "${CONFIG_FILE}"

    local required_var
    local required_vars=(
        SSH_HOST
        SSH_USER
        SSH_PORT
        SSH_IDENTITY_FILE
        VPN_ENDPOINT_IPV4
        VPN_PORT
        VPN_INTERFACE
        VPN_IPV4_NETWORK
        VPN_IPV6_NETWORK
        VPN_SERVER_IPV4
        VPN_SERVER_IPV6
        VPN_DNS
        VPN_MTU
        VPN_KEEPALIVE
        CLOUD_FIREWALL_CONFIRMED
        CLOUD_RECOVERY_CONFIRMED
        AGE_RECIPIENT
        AGE_IDENTITY_FILE
    )

    for required_var in "${required_vars[@]}"; do
        [[ -n "${!required_var:-}" ]] ||
            die "Required configuration is empty: ${required_var}"
    done

    is_ipv4_literal "${VPN_ENDPOINT_IPV4}" ||
        die "VPN_ENDPOINT_IPV4 must be an IPv4 literal"
    [[ "${SSH_HOST}" =~ ^[A-Za-z0-9._:-]+$ ]] ||
        die "SSH_HOST contains unsafe characters"
    [[ "${SSH_USER}" =~ ^[a-z_][a-z0-9_-]*$ ]] ||
        die "SSH_USER contains unsafe characters"
    [[ "${VPN_PORT}" == "51999" ]] ||
        die "VPN_PORT must remain 51999 for this deployment"
    [[ "${VPN_INTERFACE}" == "wg0" ]] ||
        die "VPN_INTERFACE must remain wg0"
    [[ "${VPN_IPV4_NETWORK}" == "10.66.0.0/24" ]] ||
        die "VPN_IPV4_NETWORK must remain 10.66.0.0/24"
    [[ "${VPN_IPV6_NETWORK}" == "fd66:66:66::/64" ]] ||
        die "VPN_IPV6_NETWORK must remain fd66:66:66::/64"
    [[ "${VPN_SERVER_IPV4}" == "10.66.0.1/24" ]] ||
        die "VPN_SERVER_IPV4 must remain 10.66.0.1/24"
    [[ "${VPN_SERVER_IPV6}" == "fd66:66:66::1/64" ]] ||
        die "VPN_SERVER_IPV6 must remain fd66:66:66::1/64"
    if [[ ! "${SSH_PORT}" =~ ^[0-9]+$ ]] ||
        ((SSH_PORT < 1 || SSH_PORT > 65535)); then
        die "SSH_PORT must be between 1 and 65535"
    fi
    [[ "${SSH_PORT}" != "80" && "${SSH_PORT}" != "443" ]] ||
        die "SSH_PORT must not use website ports 80 or 443"
    if [[ ! "${VPN_MTU}" =~ ^[0-9]+$ ]] ||
        ((VPN_MTU < 1280 || VPN_MTU > 1500)); then
        die "VPN_MTU must be between 1280 and 1500"
    fi
    [[ "${VPN_KEEPALIVE}" == "25" ]] ||
        die "VPN_KEEPALIVE must remain 25"
    [[ "${VPN_DNS}" == \
        "1.1.1.1, 1.0.0.1, 2606:4700:4700::1111, 2606:4700:4700::1001" ]] ||
        die "VPN_DNS must remain the approved dual-stack resolver list"
    assert_private_file_mode "${CONFIG_FILE}" "Local configuration"
}

require_remote_config() {
    load_config
    require_command ssh scp
    [[ -f "${SSH_IDENTITY_FILE}" ]] ||
        die "SSH identity does not exist: ${SSH_IDENTITY_FILE}"
    assert_private_file_mode "${SSH_IDENTITY_FILE}" "SSH identity"
    [[ "${VPN_ENDPOINT_IPV4}" != "203.0.113.10" ]] ||
        die "Replace the documentation VPN endpoint before connecting"
    [[ "${SSH_HOST}" != "203.0.113.10" ]] ||
        die "Replace the documentation SSH host before connecting"
    [[ "${CLOUD_FIREWALL_CONFIRMED}" == "yes" ]] ||
        die "Set CLOUD_FIREWALL_CONFIRMED=yes after allowing 51999/udp and ICMPv6"
    [[ "${CLOUD_RECOVERY_CONFIRMED}" == "yes" ]] ||
        die "Set CLOUD_RECOVERY_CONFIRMED=yes after creating a snapshot and testing console access"
}

require_age_create_config() {
    load_config
    require_command age tar sha256sum
    [[ "${AGE_RECIPIENT}" == age1* ]] ||
        die "AGE_RECIPIENT must contain a real age recipient"
    [[ "${AGE_RECIPIENT}" != *replace* ]] ||
        die "Replace the example AGE_RECIPIENT before creating backups"
}

require_age_identity_config() {
    require_age_create_config
    [[ -f "${AGE_IDENTITY_FILE}" ]] ||
        die "AGE_IDENTITY_FILE does not exist: ${AGE_IDENTITY_FILE}"
    assert_private_file_mode "${AGE_IDENTITY_FILE}" "age identity"
}

peer_ipv4() {
    case "$1" in
        windows) printf '10.66.0.10\n' ;;
        macos) printf '10.66.0.11\n' ;;
        ios) printf '10.66.0.12\n' ;;
        android) printf '10.66.0.13\n' ;;
        *) return 1 ;;
    esac
}

peer_ipv6() {
    case "$1" in
        windows) printf 'fd66:66:66::10\n' ;;
        macos) printf 'fd66:66:66::11\n' ;;
        ios) printf 'fd66:66:66::12\n' ;;
        android) printf 'fd66:66:66::13\n' ;;
        *) return 1 ;;
    esac
}
