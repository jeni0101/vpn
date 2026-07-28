#!/usr/bin/env bash

set -Eeuo pipefail

STAGE_DIR="${1:?stage directory is required}"
VPN_INTERFACE="${2:?VPN interface is required}"
STAMP="${3:?deployment stamp is required}"
ROLLBACK_UNIT="personal-vpn-rollback-${STAMP,,}"
BACKUP_DIR="/var/backups/personal-vpn/${STAMP}"

PROJECT_PATHS=(
    /etc/wireguard/wg0.key
    /etc/wireguard/wg0.conf
    /etc/nftables.d/personal-vpn.nft
    /etc/sysctl.d/70-personal-vpn.conf
    /etc/systemd/system/personal-vpn-firewall.service
    /usr/local/sbin/personal-vpn-firewall
    /usr/local/sbin/personal-vpn-rollback
)

required_stage_files=(
    wg0.conf
    personal-vpn.nft
    70-personal-vpn.conf
    personal-vpn-firewall.service
    personal-vpn-firewall
    personal-vpn-rollback
)

for file in "${required_stage_files[@]}"; do
    [[ -f "${STAGE_DIR}/${file}" ]] || {
        printf 'Missing staged file: %s\n' "${file}" >&2
        exit 1
    }
done

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
    wireguard \
    wireguard-tools \
    nftables \
    unattended-upgrades

install -d -m 0700 /etc/wireguard
install -d -m 0755 /etc/nftables.d /usr/local/sbin
install -d -m 0700 "${BACKUP_DIR}/root"
: >"${BACKUP_DIR}/existing-paths"
chmod 0600 "${BACKUP_DIR}/existing-paths"

for path in "${PROJECT_PATHS[@]}"; do
    if [[ -f "${path}" ]]; then
        printf '%s\n' "${path}" >>"${BACKUP_DIR}/existing-paths"
        install -D -m "$(stat -c '%a' "${path}")" \
            "${path}" "${BACKUP_DIR}/root${path}"
    fi
done

if [[ ! -f /etc/wireguard/wg0.key ]]; then
    umask 077
    wg genkey >/etc/wireguard/wg0.key
fi
chmod 0600 /etc/wireguard/wg0.key
server_private_key="$(</etc/wireguard/wg0.key)"

rendered_wg="${STAGE_DIR}/wg0.rendered.conf"
while IFS= read -r line || [[ -n "${line}" ]]; do
    if [[ "${line}" == "PrivateKey = __SERVER_PRIVATE_KEY__" ]]; then
        printf 'PrivateKey = %s\n' "${server_private_key}"
    else
        printf '%s\n' "${line}"
    fi
done <"${STAGE_DIR}/wg0.conf" >"${rendered_wg}"
chmod 0600 "${rendered_wg}"

wg-quick strip "${rendered_wg}" >/dev/null

check_nft="${STAGE_DIR}/personal-vpn.check.nft"
sed \
    -e 's/personal_vpn_filter/personal_vpn_filter_check/g' \
    -e 's/personal_vpn_nat4/personal_vpn_nat4_check/g' \
    -e 's/personal_vpn_nat6/personal_vpn_nat6_check/g' \
    "${STAGE_DIR}/personal-vpn.nft" >"${check_nft}"
nft -c -f "${check_nft}"

install -m 0755 "${STAGE_DIR}/personal-vpn-rollback" \
    /usr/local/sbin/personal-vpn-rollback

systemd-run \
    --quiet \
    --unit="${ROLLBACK_UNIT}" \
    --on-active=2m \
    /usr/local/sbin/personal-vpn-rollback "${BACKUP_DIR}"

install -m 0600 "${rendered_wg}" /etc/wireguard/wg0.conf
install -m 0644 "${STAGE_DIR}/personal-vpn.nft" \
    /etc/nftables.d/personal-vpn.nft
install -m 0644 "${STAGE_DIR}/70-personal-vpn.conf" \
    /etc/sysctl.d/70-personal-vpn.conf
install -m 0644 "${STAGE_DIR}/personal-vpn-firewall.service" \
    /etc/systemd/system/personal-vpn-firewall.service
install -m 0755 "${STAGE_DIR}/personal-vpn-firewall" \
    /usr/local/sbin/personal-vpn-firewall

systemctl daemon-reload
sysctl --system >/dev/null
systemctl enable unattended-upgrades.service >/dev/null 2>&1 || true
systemctl start unattended-upgrades.service >/dev/null 2>&1 || true

systemctl enable --now personal-vpn-firewall.service
systemctl reload personal-vpn-firewall.service

if systemctl is-active --quiet "wg-quick@${VPN_INTERFACE}.service"; then
    wg syncconf "${VPN_INTERFACE}" <(wg-quick strip "${VPN_INTERFACE}")
else
    systemctl enable --now "wg-quick@${VPN_INTERFACE}.service"
fi

server_public_key="$(wg pubkey </etc/wireguard/wg0.key)"
printf 'ROLLBACK_UNIT=%s\n' "${ROLLBACK_UNIT}"
printf 'BACKUP_DIR=%s\n' "${BACKUP_DIR}"
printf 'SERVER_PUBLIC_KEY=%s\n' "${server_public_key}"
printf 'DEPLOY=ok\n'
