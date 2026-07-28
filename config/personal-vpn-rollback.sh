#!/usr/bin/env bash

set -Eeuo pipefail

BACKUP_DIR="${1:?backup directory is required}"
MANIFEST="${BACKUP_DIR}/existing-paths"
BACKUP_ROOT="${BACKUP_DIR}/root"

PROJECT_PATHS=(
    /etc/wireguard/wg0.key
    /etc/wireguard/wg0.conf
    /etc/nftables.d/personal-vpn.nft
    /etc/sysctl.d/70-personal-vpn.conf
    /etc/systemd/system/personal-vpn-firewall.service
    /usr/local/sbin/personal-vpn-firewall
    /usr/local/sbin/personal-vpn-rollback
)

[[ -d "${BACKUP_DIR}" && -f "${MANIFEST}" ]] || {
    printf 'Invalid personal-vpn backup: %s\n' "${BACKUP_DIR}" >&2
    exit 1
}

if [[ -x /usr/local/sbin/personal-vpn-firewall ]]; then
    /usr/local/sbin/personal-vpn-firewall remove || true
fi

for path in "${PROJECT_PATHS[@]}"; do
    rm -f -- "${path}"
done

while IFS= read -r path; do
    [[ -n "${path}" ]] || continue
    source_path="${BACKUP_ROOT}${path}"
    if [[ -f "${source_path}" ]]; then
        install -D -m "$(stat -c '%a' "${source_path}")" "${source_path}" "${path}"
    fi
done <"${MANIFEST}"

systemctl daemon-reload
sysctl --system >/dev/null

if [[ -x /usr/local/sbin/personal-vpn-firewall &&
      -f /etc/systemd/system/personal-vpn-firewall.service ]]; then
    systemctl enable --now personal-vpn-firewall.service
else
    for spec in \
        "inet personal_vpn_filter" \
        "ip personal_vpn_nat4" \
        "ip6 personal_vpn_nat6"; do
        read -r family table_name <<<"${spec}"
        if nft list table "${family}" "${table_name}" >/dev/null 2>&1; then
            nft delete table "${family}" "${table_name}"
        fi
    done
fi

if [[ -f /etc/wireguard/wg0.conf ]]; then
    systemctl enable --now wg-quick@wg0.service
    systemctl restart wg-quick@wg0.service
else
    systemctl disable --now wg-quick@wg0.service >/dev/null 2>&1 || true
fi

printf 'personal-vpn rollback restored %s\n' "${BACKUP_DIR}"
