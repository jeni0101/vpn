#!/usr/bin/env bash

set -Eeuo pipefail

SOURCE_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d /tmp/personal-vpn-backup-flow.XXXXXXXX)"
MOCK_BIN="${TEST_ROOT}/bin"
MOCK_SERVER_ROOT="${TEST_ROOT}/server"
trap 'rm -rf "${TEST_ROOT}"' EXIT

mkdir -p "${TEST_ROOT}/config" "${MOCK_BIN}"
printf 'test-only ssh identity\n' >"${TEST_ROOT}/ssh.key"
chmod 0600 "${TEST_ROOT}/ssh.key"

ROOT_DIR="${TEST_ROOT}"
export ROOT_DIR
VPNCTL_CONFIG="${TEST_ROOT}/config/local.env"
export VPNCTL_CONFIG
VPNCTL_SSH_IDENTITY_FILE="${TEST_ROOT}/ssh.key"
export VPNCTL_SSH_IDENTITY_FILE
export MOCK_SERVER_ROOT

# shellcheck source=scripts/lib/common.sh
source "${SOURCE_ROOT}/scripts/lib/common.sh"
# shellcheck source=scripts/lib/config.sh
source "${SOURCE_ROOT}/scripts/lib/config.sh"
# shellcheck source=scripts/lib/remote.sh
source "${SOURCE_ROOT}/scripts/lib/remote.sh"
# shellcheck source=scripts/lib/backup.sh
source "${SOURCE_ROOT}/scripts/lib/backup.sh"

config_prepare \
    8.8.8.8 \
    2606:4700:4700::1111 \
    --cloud-firewall-ready \
    --recovery-ready >/dev/null

mkdir -p \
    "${STATE_DIR}/peers" \
    "${SECRETS_DIR}/peers/windows" \
    "${SECRETS_DIR}/pending" \
    "${MOCK_SERVER_ROOT}/etc/wireguard" \
    "${MOCK_SERVER_ROOT}/etc/nftables.d" \
    "${MOCK_SERVER_ROOT}/etc/sysctl.d" \
    "${MOCK_SERVER_ROOT}/etc/systemd/system" \
    "${MOCK_SERVER_ROOT}/usr/local/sbin"
printf 'name=windows\n' >"${STATE_DIR}/peers/windows.meta"
printf 'peer private material\n' >"${SECRETS_DIR}/peers/windows/private.key"
printf 'must not be archived\n' >"${SECRETS_DIR}/github-vpn-deploy-ed25519"
chmod 0600 "${SECRETS_DIR}/github-vpn-deploy-ed25519"

for path in \
    etc/wireguard/wg0.key \
    etc/wireguard/wg0.conf \
    etc/nftables.d/personal-vpn.nft \
    etc/sysctl.d/70-personal-vpn.conf \
    etc/systemd/system/personal-vpn-firewall.service \
    usr/local/sbin/personal-vpn-firewall \
    usr/local/sbin/personal-vpn-rollback; do
    printf 'mock server file: %s\n' "${path}" \
        >"${MOCK_SERVER_ROOT}/${path}"
done

cat >"${MOCK_BIN}/ssh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
tar -C "${MOCK_SERVER_ROOT}" -czf - \
    etc/wireguard/wg0.key \
    etc/wireguard/wg0.conf \
    etc/nftables.d/personal-vpn.nft \
    etc/sysctl.d/70-personal-vpn.conf \
    etc/systemd/system/personal-vpn-firewall.service \
    usr/local/sbin/personal-vpn-firewall \
    usr/local/sbin/personal-vpn-rollback
EOF
chmod 0755 "${MOCK_BIN}/ssh"
PATH="${MOCK_BIN}:${PATH}"
export PATH

archive_dir="$(backup_create)"
backup_verify "${archive_dir}"

local_contents="$(
    age -d -i "${AGE_IDENTITY_FILE}" \
        "${archive_dir}/local.tar.gz.age" |
        tar -tzf -
)"
grep -qx 'secrets/peers/windows/private.key' <<<"${local_contents}"
if grep -Eq 'backup-age-identity|github-vpn-deploy' <<<"${local_contents}"; then
    printf 'Unrelated identity was included in the local backup\n' >&2
    exit 1
fi

printf 'encrypted backup scope and verification: ok\n'
