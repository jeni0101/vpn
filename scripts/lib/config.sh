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

config_has_peer_state() {
    [[ -f "${STATE_DIR}/server-public.key" ]] ||
        find \
            "${STATE_DIR}/peers" \
            "${STATE_DIR}/pending" \
            -maxdepth 1 \
            -type f \
            -name '*.meta' \
            -print -quit 2>/dev/null |
            grep -q .
}

config_prepare() {
    local public_ipv4="$1"
    local public_ipv6="$2"
    local firewall_confirmation="$3"
    local recovery_confirmation="$4"
    local ssh_identity
    local age_identity
    local generated_age_recipient
    local canonical_ipv4
    local canonical_ipv6
    local current_ipv4=""
    local current_ipv6=""
    local current_age_recipient=""
    local deployment_ssh_user
    local deployment_vpn_port
    local deployment_web_domain
    local temp_config

    [[ "${firewall_confirmation}" == "--cloud-firewall-ready" ]] ||
        die "Pass --cloud-firewall-ready after configuring the cloud security group"
    [[ "${recovery_confirmation}" == "--recovery-ready" ]] ||
        die "Pass --recovery-ready after creating a snapshot and checking console access"

    require_command age-keygen python3 realpath
    canonical_ipv4="$(canonical_global_ip "${public_ipv4}" 4)" ||
        die "The server IPv4 must be a globally routable IPv4 literal"
    canonical_ipv6="$(canonical_global_ip "${public_ipv6}" 6)" ||
        die "The server IPv6 must be a globally routable IPv6 literal without a prefix"

    ssh_identity="$(
        realpath -m \
            "${VPNCTL_SSH_IDENTITY_FILE:-/home/ubuntu/.ssh/vpn_server_ed25519}"
    )"
    [[ -f "${ssh_identity}" ]] ||
        die "Singapore SSH identity does not exist: ${ssh_identity}"
    assert_private_file_mode "${ssh_identity}" "Singapore SSH identity"

    ensure_runtime_dirs
    age_identity="${SECRETS_DIR}/backup-age-identity.txt"

    if [[ -f "${CONFIG_FILE}" ]]; then
        # shellcheck source=/dev/null
        source "${CONFIG_FILE}"
        current_ipv4="${SERVER_PUBLIC_IPV4:-${VPN_ENDPOINT_IPV4:-}}"
        current_ipv6="${SERVER_PUBLIC_IPV6:-}"
        current_age_recipient="${AGE_RECIPIENT:-}"

        if [[ -n "${current_ipv4}" &&
              "${current_ipv4}" != "203.0.113.10" &&
              "${current_ipv4}" != "${canonical_ipv4}" ]]; then
            die "Configuration already targets another server IPv4: ${current_ipv4}"
        fi
        if [[ -n "${current_ipv6}" &&
              "${current_ipv6}" != "2001:db8::10" &&
              "$(
                  canonical_global_ip "${current_ipv6}" 6 2>/dev/null || true
              )" != "${canonical_ipv6}" ]]; then
            die "Configuration already targets another server IPv6: ${current_ipv6}"
        fi
        if config_has_peer_state &&
            [[ "${current_ipv4}" != "${canonical_ipv4}" ||
               -z "${current_ipv6}" ]]; then
            die "Existing server or peer state prevents changing the deployment target"
        fi
    fi

    if [[ ! -f "${age_identity}" ]]; then
        if find "${BACKUP_DIR}" -mindepth 2 -maxdepth 2 -type f \
            -name '*.age' -print -quit 2>/dev/null |
            grep -q .; then
            die "Encrypted backups exist but ${age_identity} is missing; restore the original identity instead of generating a new one"
        fi
        umask 077
        age-keygen -o "${age_identity}"
    fi
    chmod 0600 "${age_identity}"
    generated_age_recipient="$(age-keygen -y "${age_identity}")" ||
        die "The local age identity is invalid: ${age_identity}"
    [[ "${generated_age_recipient}" == age1* ]] ||
        die "Could not derive an age recipient"
    if [[ "${current_age_recipient}" == age1* &&
          "${current_age_recipient}" != *replace* &&
          "${current_age_recipient}" != "${generated_age_recipient}" ]]; then
        die "Existing AGE_RECIPIENT does not match ${age_identity}"
    fi

    deployment_ssh_user="${SSH_USER:-${VPNCTL_SSH_USER:-vpnadmin}}"
    deployment_vpn_port="${VPN_PORT:-${VPNCTL_VPN_PORT:-51999}}"
    deployment_web_domain="${VPN_WEB_DOMAIN:-${VPNCTL_WEB_DOMAIN:-vpn.example.com}}"
    [[ "${deployment_ssh_user}" =~ ^[a-z_][a-z0-9_-]*$ ]] ||
        die "VPNCTL_SSH_USER contains unsafe characters"
    if [[ ! "${deployment_vpn_port}" =~ ^[0-9]+$ ]] ||
        ((deployment_vpn_port < 1024 || deployment_vpn_port > 65535)); then
        die "VPNCTL_VPN_PORT must be between 1024 and 65535"
    fi
    [[ "${deployment_vpn_port}" != "443" ]] ||
        die "VPNCTL_VPN_PORT must not use the website UDP port 443"
    [[ "${deployment_web_domain}" =~ ^[A-Za-z0-9.-]+$ &&
       "${deployment_web_domain}" == *.* ]] ||
        die "VPNCTL_WEB_DOMAIN must be a DNS hostname"

    temp_config="$(mktemp "${ROOT_DIR}/config/local.env.tmp.XXXXXXXX")"
    trap 'rm -f "${temp_config}"' RETURN
    umask 077
    {
        printf '# Generated by: scripts/vpnctl config prepare\n'
        printf 'SSH_HOST="%s"\n' "${canonical_ipv4}"
        printf 'SSH_USER="%s"\n' "${deployment_ssh_user}"
        printf 'SSH_PORT="22"\n'
        printf 'SSH_IDENTITY_FILE="%s"\n' "${ssh_identity}"
        printf '\n'
        printf 'SERVER_PUBLIC_IPV4="%s"\n' "${canonical_ipv4}"
        printf 'SERVER_PUBLIC_IPV6="%s"\n' "${canonical_ipv6}"
        printf 'VPN_ENDPOINT_IPV4="%s"\n' "${canonical_ipv4}"
        printf 'VPN_PORT="%s"\n' "${deployment_vpn_port}"
        printf 'VPN_INTERFACE="wg0"\n'
        printf 'VPN_WEB_DOMAIN="%s"\n' "${deployment_web_domain}"
        printf 'WAN_INTERFACE=""\n'
        printf '\n'
        printf 'VPN_IPV4_NETWORK="10.66.0.0/24"\n'
        printf 'VPN_IPV6_NETWORK="fd66:66:66::/64"\n'
        printf 'VPN_SERVER_IPV4="10.66.0.1/24"\n'
        printf 'VPN_SERVER_IPV6="fd66:66:66::1/64"\n'
        printf 'VPN_DNS="1.1.1.1, 1.0.0.1, 2606:4700:4700::1111, 2606:4700:4700::1001"\n'
        printf 'VPN_MTU="1420"\n'
        printf 'VPN_KEEPALIVE="25"\n'
        printf '\n'
        printf 'CLOUD_FIREWALL_CONFIRMED="yes"\n'
        printf 'CLOUD_RECOVERY_CONFIRMED="yes"\n'
        printf '\n'
        printf 'AGE_RECIPIENT="%s"\n' "${generated_age_recipient}"
        printf 'AGE_IDENTITY_FILE="%s"\n' "${age_identity}"
    } >"${temp_config}"
    chmod 0600 "${temp_config}"
    mv -f "${temp_config}" "${CONFIG_FILE}"
    trap - RETURN

    log "Prepared ${CONFIG_FILE} for ${canonical_ipv4} / ${canonical_ipv6}"
    warn "Copy ${age_identity} to secure offline storage; it is intentionally excluded from VPN backups"
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
        SERVER_PUBLIC_IPV4
        SERVER_PUBLIC_IPV6
        VPN_ENDPOINT_IPV4
        VPN_PORT
        VPN_INTERFACE
        VPN_WEB_DOMAIN
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
    is_ipv4_literal "${SERVER_PUBLIC_IPV4}" ||
        die "SERVER_PUBLIC_IPV4 must be an IPv4 literal"
    is_ipv6_literal "${SERVER_PUBLIC_IPV6}" ||
        die "SERVER_PUBLIC_IPV6 must be an IPv6 literal"
    [[ "${SSH_HOST}" == "${SERVER_PUBLIC_IPV4}" ]] ||
        die "SSH_HOST must match SERVER_PUBLIC_IPV4"
    [[ "${VPN_ENDPOINT_IPV4}" == "${SERVER_PUBLIC_IPV4}" ]] ||
        die "VPN_ENDPOINT_IPV4 must match SERVER_PUBLIC_IPV4"
    [[ "${SSH_HOST}" =~ ^[A-Za-z0-9._:-]+$ ]] ||
        die "SSH_HOST contains unsafe characters"
    [[ "${SSH_USER}" =~ ^[a-z_][a-z0-9_-]*$ ]] ||
        die "SSH_USER contains unsafe characters"
    if [[ ! "${VPN_PORT}" =~ ^[0-9]+$ ]] ||
        ((VPN_PORT < 1024 || VPN_PORT > 65535)); then
        die "VPN_PORT must be between 1024 and 65535"
    fi
    [[ "${VPN_PORT}" != "443" ]] ||
        die "VPN_PORT must not use the website UDP port 443"
    [[ "${VPN_WEB_DOMAIN}" =~ ^[A-Za-z0-9.-]+$ &&
       "${VPN_WEB_DOMAIN}" == *.* ]] ||
        die "VPN_WEB_DOMAIN must be a DNS hostname"
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
    require_command python3 ssh scp
    [[ -f "${SSH_IDENTITY_FILE}" ]] ||
        die "SSH identity does not exist: ${SSH_IDENTITY_FILE}"
    assert_private_file_mode "${SSH_IDENTITY_FILE}" "SSH identity"
    is_global_ipv4 "${SERVER_PUBLIC_IPV4}" ||
        die "SERVER_PUBLIC_IPV4 must be globally routable"
    is_global_ipv6 "${SERVER_PUBLIC_IPV6}" ||
        die "SERVER_PUBLIC_IPV6 must be globally routable"
    [[ "${CLOUD_FIREWALL_CONFIRMED}" == "yes" ]] ||
        die "Set CLOUD_FIREWALL_CONFIRMED=yes after allowing ${VPN_PORT}/udp and ICMPv6"
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
