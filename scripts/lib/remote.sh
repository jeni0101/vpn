#!/usr/bin/env bash

set -Eeuo pipefail

ssh_args() {
    SSH_ARGS=(
        -i "${SSH_IDENTITY_FILE}"
        -p "${SSH_PORT}"
        -o BatchMode=yes
        -o ConnectTimeout=10
        -o StrictHostKeyChecking=accept-new
    )
}

scp_args() {
    SCP_ARGS=(
        -i "${SSH_IDENTITY_FILE}"
        -P "${SSH_PORT}"
        -o BatchMode=yes
        -o ConnectTimeout=10
        -o StrictHostKeyChecking=accept-new
    )
}

remote_target() {
    printf '%s@%s\n' "${SSH_USER}" "${SSH_HOST}"
}

remote_exec() {
    ssh_args
    # shellcheck disable=SC2029 # Callers deliberately provide the remote command.
    ssh "${SSH_ARGS[@]}" "$(remote_target)" "$@"
}

remote_exec_script() {
    local script="$1"
    shift
    local remote_command="bash -s --"
    local argument

    ssh_args
    for argument in "$@"; do
        printf -v argument '%q' "${argument}"
        remote_command+=" ${argument}"
    done
    # shellcheck disable=SC2029 # Arguments are individually escaped with printf %q.
    ssh "${SSH_ARGS[@]}" "$(remote_target)" "${remote_command}" <"${script}"
}

remote_sudo_script() {
    local script="$1"
    shift
    local remote_command="sudo -n bash -s --"
    local argument

    ssh_args
    for argument in "$@"; do
        printf -v argument '%q' "${argument}"
        remote_command+=" ${argument}"
    done
    # shellcheck disable=SC2029 # Arguments are individually escaped with printf %q.
    ssh "${SSH_ARGS[@]}" "$(remote_target)" "${remote_command}" <"${script}"
}

copy_to_remote() {
    local local_path="$1"
    local remote_path="$2"
    scp_args
    scp -q "${SCP_ARGS[@]}" "${local_path}" \
        "$(remote_target):${remote_path}"
}

server_preflight() {
    local output

    require_remote_config
    output="$(
        remote_exec_script \
            "${ROOT_DIR}/scripts/remote/preflight.sh" \
            "${VPN_PORT}" \
            "${VPN_INTERFACE}" \
            "${WAN_INTERFACE:-}" \
            "${SERVER_PUBLIC_IPV4}" \
            "${SERVER_PUBLIC_IPV6}"
    )"
    printf '%s\n' "${output}"

    DETECTED_WAN_INTERFACE="$(
        awk -F= '$1 == "WAN_INTERFACE" {print $2}' <<<"${output}"
    )"
    [[ -n "${DETECTED_WAN_INTERFACE}" ]] ||
        die "Remote preflight did not return a WAN interface"

    PREFLIGHT_WEB_TCP_PORTS="$(
        awk -F= '$1 == "WEB_TCP_PORTS" {print $2}' <<<"${output}"
    )"
    PREFLIGHT_WEB_UDP_PORTS="$(
        awk -F= '$1 == "WEB_UDP_PORTS" {print $2}' <<<"${output}"
    )"
    [[ -n "${PREFLIGHT_WEB_TCP_PORTS}" ]] ||
        die "Remote preflight did not return a website TCP listener"
    [[ "${PREFLIGHT_WEB_TCP_PORTS}" =~ ^[0-9]+(,[0-9]+)*$ ]] ||
        die "Remote preflight returned unsafe website TCP ports"
    [[ -z "${PREFLIGHT_WEB_UDP_PORTS}" ||
       "${PREFLIGHT_WEB_UDP_PORTS}" =~ ^[0-9]+(,[0-9]+)*$ ]] ||
        die "Remote preflight returned unsafe website UDP ports"
    PREFLIGHT_DOCKER_INTEGRATION="$(
        awk -F= '$1 == "DOCKER_INTEGRATION" {print $2}' <<<"${output}"
    )"
    [[ "${PREFLIGHT_DOCKER_INTEGRATION}" == "yes" ||
       "${PREFLIGHT_DOCKER_INTEGRATION}" == "no" ]] ||
        die "Remote preflight returned an unsafe Docker integration mode"
}

tcp_port_reachable() {
    local host="$1"
    local port="$2"

    # shellcheck disable=SC2016 # $1 and $2 are expanded by the child bash.
    timeout 5 bash -c \
        'exec 3<>"/dev/tcp/$1/$2"' \
        personal-vpn-tcp-probe "${host}" "${port}" \
        >/dev/null 2>&1
}

capture_external_web_ports() {
    local expected_ports="$1"
    local port
    local result=""
    local -a ports=()

    IFS=',' read -r -a ports <<<"${expected_ports}"
    for port in "${ports[@]}"; do
        if tcp_port_reachable "${SERVER_PUBLIC_IPV4}" "${port}"; then
            if [[ -n "${result}" ]]; then
                result+=","
            fi
            result+="${port}"
        fi
    done
    printf '%s\n' "${result}"
}

verify_external_web_ports() {
    local expected_ports="$1"
    local port
    local -a ports=()

    [[ -n "${expected_ports}" ]] || return 0
    IFS=',' read -r -a ports <<<"${expected_ports}"
    for port in "${ports[@]}"; do
        tcp_port_reachable "${SERVER_PUBLIC_IPV4}" "${port}" ||
            return 1
    done
}

cancel_remote_rollback() {
    local rollback_unit="$1"
    [[ "${rollback_unit}" =~ ^personal-vpn-rollback-[a-z0-9]+$ ]] ||
        die "Unsafe rollback unit returned by server"

    remote_exec \
        "sudo -n systemctl stop '${rollback_unit}.timer' 2>/dev/null || true; \
         sudo -n systemctl reset-failed '${rollback_unit}.service' 2>/dev/null || true"
}

server_deploy() (
    local temp_dir
    local stamp
    local stage_name
    local remote_stage
    local output
    local rollback_unit
    local server_public_key
    local wan_interface
    local web_tcp_ports
    local web_udp_ports
    local externally_reachable_web_ports
    local docker_integration

    require_remote_config
    require_command timeout wg
    ensure_runtime_dirs
    server_preflight >/dev/stderr
    wan_interface="${WAN_INTERFACE:-${DETECTED_WAN_INTERFACE}}"
    web_tcp_ports="${PREFLIGHT_WEB_TCP_PORTS}"
    web_udp_ports="${PREFLIGHT_WEB_UDP_PORTS}"
    docker_integration="${PREFLIGHT_DOCKER_INTEGRATION}"
    externally_reachable_web_ports="$(
        capture_external_web_ports "${web_tcp_ports}"
    )"
    if [[ -z "${externally_reachable_web_ports}" ]]; then
        warn "Website listeners are present remotely but were not directly reachable before deployment"
    else
        log "Externally reachable website TCP ports before deployment: ${externally_reachable_web_ports}"
    fi
    stamp="$(utc_timestamp)"
    stage_name="personal-vpn-${stamp,,}-$$"
    remote_stage="/tmp/${stage_name}"
    temp_dir="$(make_temp_dir)"
    trap '
        find "${temp_dir}" -mindepth 1 -maxdepth 1 -type f -delete 2>/dev/null || true
        rmdir "${temp_dir}" 2>/dev/null || true
    ' EXIT

    render_server_config "${temp_dir}/wg0.conf"
    render_nftables_config "${temp_dir}/personal-vpn.nft" "${wan_interface}"
    render_sysctl_config "${temp_dir}/70-personal-vpn.conf"
    render_firewall_service "${temp_dir}/personal-vpn-firewall.service"
    render_firewall_script \
        "${temp_dir}/personal-vpn-firewall" \
        "${wan_interface}"
    cp "${ROOT_DIR}/config/personal-vpn-rollback.sh" \
        "${temp_dir}/personal-vpn-rollback"

    remote_exec "umask 077 && mkdir '${remote_stage}'"
    copy_to_remote "${temp_dir}/wg0.conf" "${remote_stage}/wg0.conf"
    copy_to_remote "${temp_dir}/personal-vpn.nft" \
        "${remote_stage}/personal-vpn.nft"
    copy_to_remote "${temp_dir}/70-personal-vpn.conf" \
        "${remote_stage}/70-personal-vpn.conf"
    copy_to_remote "${temp_dir}/personal-vpn-firewall.service" \
        "${remote_stage}/personal-vpn-firewall.service"
    copy_to_remote "${temp_dir}/personal-vpn-firewall" \
        "${remote_stage}/personal-vpn-firewall"
    copy_to_remote "${temp_dir}/personal-vpn-rollback" \
        "${remote_stage}/personal-vpn-rollback"

    if ! output="$(
        remote_sudo_script \
            "${ROOT_DIR}/scripts/remote/install.sh" \
            "${remote_stage}" \
            "${VPN_INTERFACE}" \
            "${stamp}"
    )"; then
        warn "Deployment failed. If firewall application began, wait two minutes for automatic rollback."
        remote_exec "rm -rf '${remote_stage}'" >/dev/null 2>&1 || true
        return 1
    fi
    printf '%s\n' "${output}"

    rollback_unit="$(
        awk -F= '$1 == "ROLLBACK_UNIT" {print $2}' <<<"${output}"
    )"
    server_public_key="$(
        awk -F= '$1 == "SERVER_PUBLIC_KEY" {print $2}' <<<"${output}"
    )"
    assert_wireguard_key "Server public key" "${server_public_key}"

    if ! remote_exec "sudo -n wg show '${VPN_INTERFACE}' >/dev/null"; then
        warn "Fresh SSH verification failed; automatic rollback remains armed."
        return 1
    fi

    if ! remote_sudo_script \
        "${ROOT_DIR}/scripts/remote/postcheck.sh" \
        "${VPN_INTERFACE}" \
        "${wan_interface}" \
        "${docker_integration}" \
        "${web_tcp_ports}" \
        "${web_udp_ports}"; then
        warn "Remote VPN or website verification failed; automatic rollback remains armed."
        return 1
    fi

    if ! verify_external_web_ports "${externally_reachable_web_ports}"; then
        warn "A previously reachable website port is unavailable; automatic rollback remains armed."
        return 1
    fi

    cancel_remote_rollback "${rollback_unit}"
    printf '%s\n' "${server_public_key}" >"${STATE_DIR}/server-public.key"
    chmod 0600 "${STATE_DIR}/server-public.key"
    remote_exec "rm -rf '${remote_stage}'" >/dev/null
    log "Server deployment verified and rollback timer cancelled"
)

server_bootstrap() {
    require_remote_config
    require_age_identity_config
    require_command age age-keygen bash scp sha256sum ssh tar timeout wg

    peer_assert_bootstrap_ready
    server_deploy
    peer_bootstrap_all
    server_status

    log "Bootstrap completed; four client configurations are in ${EXPORT_DIR}"
    log "Show a temporary mobile QR code with: scripts/vpnctl peer export <ios|android> --qr"
    warn "Keep an offline copy of ${AGE_IDENTITY_FILE}; VPN backups intentionally exclude it"
}

sync_server_wireguard() {
    local local_config="$1"
    local remote_config
    local token

    require_remote_config
    token="$(utc_timestamp)-$$"
    remote_config="/tmp/personal-vpn-wg0-${token}.conf"
    copy_to_remote "${local_config}" "${remote_config}"

    if ! remote_sudo_script \
        "${ROOT_DIR}/scripts/remote/sync-wg.sh" \
        "${remote_config}" \
        "${VPN_INTERFACE}"; then
        remote_exec "rm -f '${remote_config}'" >/dev/null 2>&1 || true
        return 1
    fi
    remote_exec "rm -f '${remote_config}'" >/dev/null
}

server_status() {
    require_remote_config
    remote_exec \
        "sudo -n wg show '${VPN_INTERFACE}'; \
         printf '\\nIPv4 forwarding: '; sysctl -n net.ipv4.ip_forward; \
         printf 'IPv6 forwarding: '; sysctl -n net.ipv6.conf.all.forwarding; \
         printf '\\nProject nftables tables:\\n'; \
         sudo -n nft list tables | grep personal_vpn || true; \
         printf '\\nGlobal IPv6 addresses:\\n'; \
         ip -6 -o addr show scope global; \
         printf '\\nWebsite listeners:\\n'; \
         ss -H -ltn | awk '\$4 ~ /:(80|443)\$/ {print}'; \
         ss -H -lun | awk '\$4 ~ /:443\$/ {print}'"
}
