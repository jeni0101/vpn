#!/usr/bin/env bash

set -Eeuo pipefail

SOURCE_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d /tmp/personal-vpn-config-prepare.XXXXXXXX)"
trap 'rm -rf "${TEST_ROOT}"' EXIT

mkdir -p "${TEST_ROOT}/config"
printf 'test-only ssh identity\n' >"${TEST_ROOT}/ssh.key"
chmod 0600 "${TEST_ROOT}/ssh.key"

ROOT_DIR="${TEST_ROOT}"
export ROOT_DIR
VPNCTL_CONFIG="${TEST_ROOT}/config/local.env"
export VPNCTL_CONFIG
VPNCTL_SSH_IDENTITY_FILE="${TEST_ROOT}/ssh.key"
export VPNCTL_SSH_IDENTITY_FILE

# shellcheck source=scripts/lib/common.sh
source "${SOURCE_ROOT}/scripts/lib/common.sh"
# shellcheck source=scripts/lib/config.sh
source "${SOURCE_ROOT}/scripts/lib/config.sh"

config_prepare \
    8.8.8.8 \
    2606:4700:4700::1111 \
    --cloud-firewall-ready \
    --recovery-ready >/dev/null

grep -qx 'SSH_USER="vpnadmin"' "${CONFIG_FILE}"
grep -qx 'SSH_PORT="22"' "${CONFIG_FILE}"
grep -qx 'SERVER_PUBLIC_IPV4="8.8.8.8"' "${CONFIG_FILE}"
grep -qx 'SERVER_PUBLIC_IPV6="2606:4700:4700::1111"' "${CONFIG_FILE}"
grep -qx 'VPN_ENDPOINT_IPV4="8.8.8.8"' "${CONFIG_FILE}"
grep -qx 'VPN_PORT="51999"' "${CONFIG_FILE}"
grep -qx 'VPN_WEB_DOMAIN="vpn.example.com"' "${CONFIG_FILE}"
grep -qx 'CLOUD_FIREWALL_CONFIRMED="yes"' "${CONFIG_FILE}"
grep -qx 'CLOUD_RECOVERY_CONFIRMED="yes"' "${CONFIG_FILE}"
[[ "$(stat -c '%a' "${CONFIG_FILE}")" == "600" ]]
[[ "$(stat -c '%a' "${SECRETS_DIR}/backup-age-identity.txt")" == "600" ]]
age-keygen -y "${SECRETS_DIR}/backup-age-identity.txt" |
    grep -qx 'age1[a-z0-9]*'

first_checksum="$(sha256sum "${CONFIG_FILE}" | awk '{print $1}')"
config_prepare \
    8.8.8.8 \
    2606:4700:4700::1111 \
    --cloud-firewall-ready \
    --recovery-ready >/dev/null
second_checksum="$(sha256sum "${CONFIG_FILE}" | awk '{print $1}')"
[[ "${first_checksum}" == "${second_checksum}" ]]

if (
    config_prepare \
        8.8.4.4 \
        2606:4700:4700::1111 \
        --cloud-firewall-ready \
        --recovery-ready
) >/dev/null 2>&1; then
    printf 'Configuration target replacement was not rejected\n' >&2
    exit 1
fi

if (
    config_prepare \
        10.0.0.1 \
        2606:4700:4700::1111 \
        --cloud-firewall-ready \
        --recovery-ready
) >/dev/null 2>&1; then
    printf 'Private IPv4 was not rejected\n' >&2
    exit 1
fi

grep -q 'secrets/peers' "${SOURCE_ROOT}/scripts/lib/backup.sh"
grep -q 'secrets/pending' "${SOURCE_ROOT}/scripts/lib/backup.sh"
if grep -Eq '^[[:space:]]*secrets[[:space:]]*\\?$' \
    "${SOURCE_ROOT}/scripts/lib/backup.sh"; then
    printf 'Backup still archives the entire secrets directory\n' >&2
    exit 1
fi

printf 'config prepare and age identity: ok\n'
