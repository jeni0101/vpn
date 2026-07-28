#!/usr/bin/env bash

set -Eeuo pipefail

STAGED_CONFIG="${1:?staged WireGuard config is required}"
VPN_INTERFACE="${2:?VPN interface is required}"

[[ -f "${STAGED_CONFIG}" ]] || {
    printf 'Missing staged WireGuard config\n' >&2
    exit 1
}
[[ -f /etc/wireguard/wg0.key ]] || {
    printf 'Missing server private key\n' >&2
    exit 1
}

server_private_key="$(</etc/wireguard/wg0.key)"
rendered_config="${STAGED_CONFIG}.rendered"
old_config="$(mktemp /etc/wireguard/wg0.conf.previous.XXXXXXXX)"
trap 'rm -f "${rendered_config}" "${old_config}"' EXIT

while IFS= read -r line || [[ -n "${line}" ]]; do
    if [[ "${line}" == "PrivateKey = __SERVER_PRIVATE_KEY__" ]]; then
        printf 'PrivateKey = %s\n' "${server_private_key}"
    else
        printf '%s\n' "${line}"
    fi
done <"${STAGED_CONFIG}" >"${rendered_config}"
chmod 0600 "${rendered_config}"

wg-quick strip "${rendered_config}" >/dev/null
cp -a /etc/wireguard/wg0.conf "${old_config}"
install -m 0600 "${rendered_config}" /etc/wireguard/wg0.conf

if ! wg syncconf "${VPN_INTERFACE}" <(wg-quick strip "${VPN_INTERFACE}"); then
    install -m 0600 "${old_config}" /etc/wireguard/wg0.conf
    wg syncconf "${VPN_INTERFACE}" <(wg-quick strip "${VPN_INTERFACE}") || true
    printf 'WireGuard update failed and previous config was restored\n' >&2
    exit 1
fi

printf 'SYNC=ok\n'
