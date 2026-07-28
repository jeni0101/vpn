#!/usr/bin/env bash

set -Eeuo pipefail

SOURCE_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d /tmp/personal-vpn-peer-flow.XXXXXXXX)"
trap 'rm -rf "${TEST_ROOT}"' EXIT

mkdir -p \
    "${TEST_ROOT}/config" \
    "${TEST_ROOT}/state/peers" \
    "${TEST_ROOT}/state/pending" \
    "${TEST_ROOT}/secrets/peers" \
    "${TEST_ROOT}/secrets/pending" \
    "${TEST_ROOT}/backups" \
    "${TEST_ROOT}/exports"
cp "${SOURCE_ROOT}/config/"*.tpl "${TEST_ROOT}/config/"
cp "${SOURCE_ROOT}/config/local.env.example" "${TEST_ROOT}/config/local.env"
chmod 0700 \
    "${TEST_ROOT}/state" \
    "${TEST_ROOT}/secrets" \
    "${TEST_ROOT}/backups" \
    "${TEST_ROOT}/exports"
chmod 0600 "${TEST_ROOT}/config/local.env"

ROOT_DIR="${TEST_ROOT}"
export ROOT_DIR
VPNCTL_CONFIG="${TEST_ROOT}/config/local.env"
export VPNCTL_CONFIG

# shellcheck source=scripts/lib/common.sh
source "${SOURCE_ROOT}/scripts/lib/common.sh"
# shellcheck source=scripts/lib/config.sh
source "${SOURCE_ROOT}/scripts/lib/config.sh"
# shellcheck source=scripts/lib/render.sh
source "${SOURCE_ROOT}/scripts/lib/render.sh"
# shellcheck source=scripts/lib/peers.sh
source "${SOURCE_ROOT}/scripts/lib/peers.sh"

backup_create() {
    printf '%s\n' "${TEST_ROOT}/backups/mock"
}

sync_server_wireguard() {
    local config_file="$1"
    grep -q '^PrivateKey = __SERVER_PRIVATE_KEY__$' "${config_file}"
    cp "${config_file}" "${TEST_ROOT}/last-sync.conf"
}

wg genkey | wg pubkey >"${STATE_DIR}/server-public.key"
chmod 0600 "${STATE_DIR}/server-public.key"

peer_add windows
[[ -f "${STATE_DIR}/peers/windows.meta" ]]
[[ -f "${SECRETS_DIR}/peers/windows/client.conf" ]]
[[ ! -e "${STATE_DIR}/pending/windows.meta" ]]
grep -q '^Address = 10.66.0.10/32, fd66:66:66::10/128$' \
    "${SECRETS_DIR}/peers/windows/client.conf"
grep -q '^AllowedIPs = 0.0.0.0/0, ::/0$' \
    "${SECRETS_DIR}/peers/windows/client.conf"

old_public_key="$(<"${SECRETS_DIR}/peers/windows/public.key")"
peer_rotate_prepare windows
pending_public_key="$(<"${SECRETS_DIR}/pending/windows/public.key")"
[[ "${old_public_key}" != "${pending_public_key}" ]]

peer_export windows --file >/dev/null
cmp \
    "${SECRETS_DIR}/pending/windows/client.conf" \
    "${EXPORT_DIR}/windows.conf"

peer_rotate_activate windows --yes
active_public_key="$(<"${SECRETS_DIR}/peers/windows/public.key")"
[[ "${active_public_key}" == "${pending_public_key}" ]]
[[ ! -e "${SECRETS_DIR}/pending/windows" ]]
grep -q "PublicKey = ${active_public_key}" "${TEST_ROOT}/last-sync.conf"

peer_revoke windows --yes
[[ ! -e "${STATE_DIR}/peers/windows.meta" ]]
[[ ! -e "${SECRETS_DIR}/peers/windows" ]]
if grep -q '^# peer: windows$' "${TEST_ROOT}/last-sync.conf"; then
    printf 'Revoked peer remained in rendered config\n' >&2
    exit 1
fi

printf 'peer lifecycle transaction flow: ok\n'
