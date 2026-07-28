#!/usr/bin/env bash

set -Eeuo pipefail

replace_token() {
    local variable_name="$1"
    local token="$2"
    local replacement="$3"
    local value="${!variable_name}"
    value="${value//@@${token}@@/${replacement}}"
    printf -v "${variable_name}" '%s' "${value}"
}

render_client_config() {
    local name="$1"
    local secret_dir="$2"
    local output="$3"
    local template
    local client_private_key
    local preshared_key
    local server_public_key
    local client_ipv4
    local client_ipv6

    load_config
    [[ -f "${STATE_DIR}/server-public.key" ]] ||
        die "Server public key is missing; deploy the server first"
    [[ -f "${secret_dir}/private.key" && -f "${secret_dir}/psk" ]] ||
        die "Peer secrets are incomplete for ${name}"

    template="$(<"${ROOT_DIR}/config/client.conf.tpl")"
    client_private_key="$(<"${secret_dir}/private.key")"
    preshared_key="$(<"${secret_dir}/psk")"
    server_public_key="$(<"${STATE_DIR}/server-public.key")"
    client_ipv4="$(peer_ipv4 "${name}")"
    client_ipv6="$(peer_ipv6 "${name}")"

    assert_wireguard_key "Client private key" "${client_private_key}"
    assert_wireguard_key "PreSharedKey" "${preshared_key}"
    assert_wireguard_key "Server public key" "${server_public_key}"

    replace_token template CLIENT_PRIVATE_KEY "${client_private_key}"
    replace_token template CLIENT_IPV4 "${client_ipv4}"
    replace_token template CLIENT_IPV6 "${client_ipv6}"
    replace_token template VPN_DNS "${VPN_DNS}"
    replace_token template VPN_MTU "${VPN_MTU}"
    replace_token template SERVER_PUBLIC_KEY "${server_public_key}"
    replace_token template PRESHARED_KEY "${preshared_key}"
    replace_token template VPN_ENDPOINT_IPV4 "${VPN_ENDPOINT_IPV4}"
    replace_token template VPN_PORT "${VPN_PORT}"
    replace_token template VPN_KEEPALIVE "${VPN_KEEPALIVE}"

    umask 077
    printf '%s\n' "${template}" >"${output}"
    chmod 0600 "${output}"
}

render_peer_stanza() {
    local meta_file="$1"
    local secret_dir="$2"
    local name
    local ipv4
    local ipv6
    local public_key
    local preshared_key

    name="$(meta_get "${meta_file}" name)"
    ipv4="$(meta_get "${meta_file}" ipv4)"
    ipv6="$(meta_get "${meta_file}" ipv6)"
    public_key="$(meta_get "${meta_file}" public_key)"
    preshared_key="$(<"${secret_dir}/psk")"

    assert_safe_peer_name "${name}"
    assert_wireguard_key "Peer public key (${name})" "${public_key}"
    assert_wireguard_key "PreSharedKey (${name})" "${preshared_key}"

    cat <<EOF
# peer: ${name}
[Peer]
PublicKey = ${public_key}
PresharedKey = ${preshared_key}
AllowedIPs = ${ipv4}/32, ${ipv6}/128
EOF
}

render_server_config() {
    local output="$1"
    local replacement_name="${2:-}"
    local replacement_meta="${3:-}"
    local replacement_secret_dir="${4:-}"
    local template
    local peer_stanzas=""
    local meta_file
    local name
    local secret_dir

    load_config

    while IFS= read -r meta_file; do
        [[ -n "${meta_file}" ]] || continue
        name="$(meta_get "${meta_file}" name)"
        if [[ -n "${replacement_name}" && "${name}" == "${replacement_name}" ]]; then
            continue
        fi
        secret_dir="${SECRETS_DIR}/peers/${name}"
        [[ -f "${secret_dir}/psk" ]] ||
            die "Missing active PSK for ${name}"
        peer_stanzas+="$(render_peer_stanza "${meta_file}" "${secret_dir}")"
        peer_stanzas+=$'\n\n'
    done < <(find "${STATE_DIR}/peers" -maxdepth 1 -type f -name '*.meta' -print | sort)

    if [[ -n "${replacement_name}" && -n "${replacement_meta}" ]]; then
        [[ -f "${replacement_meta}" && -f "${replacement_secret_dir}/psk" ]] ||
            die "Replacement peer material is incomplete"
        peer_stanzas+="$(render_peer_stanza "${replacement_meta}" "${replacement_secret_dir}")"
        peer_stanzas+=$'\n'
    fi

    template="$(<"${ROOT_DIR}/config/wg0.conf.tpl")"
    replace_token template VPN_SERVER_IPV4 "${VPN_SERVER_IPV4}"
    replace_token template VPN_SERVER_IPV6 "${VPN_SERVER_IPV6}"
    replace_token template VPN_PORT "${VPN_PORT}"
    replace_token template VPN_MTU "${VPN_MTU}"
    replace_token template SERVER_PRIVATE_KEY "__SERVER_PRIVATE_KEY__"
    replace_token template PEERS "${peer_stanzas%$'\n'}"

    umask 077
    printf '%s\n' "${template}" >"${output}"
    chmod 0600 "${output}"
}

render_nftables_config() {
    local output="$1"
    local wan_interface="$2"
    local template

    load_config
    [[ "${wan_interface}" =~ ^[A-Za-z0-9_.:-]+$ ]] ||
        die "Unsafe WAN interface name: ${wan_interface}"

    template="$(<"${ROOT_DIR}/config/personal-vpn.nft.tpl")"
    replace_token template SSH_PORT "${SSH_PORT}"
    replace_token template VPN_PORT "${VPN_PORT}"
    replace_token template VPN_INTERFACE "${VPN_INTERFACE}"
    replace_token template WAN_INTERFACE "${wan_interface}"
    replace_token template VPN_IPV4_NETWORK "${VPN_IPV4_NETWORK}"
    replace_token template VPN_IPV6_NETWORK "${VPN_IPV6_NETWORK}"

    printf '%s\n' "${template}" >"${output}"
}

render_sysctl_config() {
    local output="$1"
    cp "${ROOT_DIR}/config/70-personal-vpn.conf.tpl" "${output}"
}

render_firewall_service() {
    local output="$1"
    local template

    load_config
    template="$(<"${ROOT_DIR}/config/personal-vpn-firewall.service.tpl")"
    replace_token template VPN_INTERFACE "${VPN_INTERFACE}"
    printf '%s\n' "${template}" >"${output}"
}

render_firewall_script() {
    local output="$1"
    local wan_interface="$2"
    local template

    load_config
    [[ "${wan_interface}" =~ ^[A-Za-z0-9_.:-]+$ ]] ||
        die "Unsafe WAN interface name: ${wan_interface}"

    template="$(<"${ROOT_DIR}/config/personal-vpn-firewall.sh")"
    replace_token template VPN_INTERFACE "${VPN_INTERFACE}"
    replace_token template WAN_INTERFACE "${wan_interface}"
    printf '%s\n' "${template}" >"${output}"
    chmod 0755 "${output}"
}
