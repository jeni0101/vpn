#!/usr/bin/env bash

set -Eeuo pipefail

STAGE_ROOT="${1:?restore stage is required}"
VPN_INTERFACE="${2:?VPN interface is required}"
STAMP="${3:?restore stamp is required}"
WEB_DOMAIN="${4:-vpn.example.com}"
ROLLBACK_UNIT="personal-vpn-rollback-${STAMP,,}"
BACKUP_DIR="/var/backups/personal-vpn/${STAMP}"
[[ "${WEB_DOMAIN}" =~ ^[A-Za-z0-9.-]+$ && "${WEB_DOMAIN}" == *.* ]] || {
    printf 'invalid management domain\n' >&2
    exit 1
}

PROJECT_PATHS=(
    /etc/wireguard/wg0.key
    /etc/wireguard/wg0.conf
    /etc/nftables.d/personal-vpn.nft
    /etc/sysctl.d/70-personal-vpn.conf
    /etc/systemd/system/personal-vpn-firewall.service
    /usr/local/sbin/personal-vpn-firewall
    /usr/local/sbin/personal-vpn-rollback
)

for path in "${PROJECT_PATHS[@]}"; do
    [[ -f "${STAGE_ROOT}${path}" ]] || {
        printf 'Restore archive is missing %s\n' "${path}" >&2
        exit 1
    }
done

restored_private_key="$(<"${STAGE_ROOT}/etc/wireguard/wg0.key")"
configured_private_key="$(
    awk -F' = ' '$1 == "PrivateKey" {print $2; exit}' \
        "${STAGE_ROOT}/etc/wireguard/wg0.conf"
)"
[[ "${restored_private_key}" == "${configured_private_key}" ]] || {
    printf 'Restored server key does not match wg0.conf\n' >&2
    exit 1
}

wg-quick strip "${STAGE_ROOT}/etc/wireguard/wg0.conf" >/dev/null

check_nft="${STAGE_ROOT}/personal-vpn.restore-check.nft"
sed \
    -e 's/personal_vpn_filter/personal_vpn_filter_restore_check/g' \
    -e 's/personal_vpn_nat4/personal_vpn_nat4_restore_check/g' \
    -e 's/personal_vpn_nat6/personal_vpn_nat6_restore_check/g' \
    "${STAGE_ROOT}/etc/nftables.d/personal-vpn.nft" >"${check_nft}"
nft -c -f "${check_nft}"

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

systemd-run \
    --quiet \
    --unit="${ROLLBACK_UNIT}" \
    --on-active=2m \
    /usr/local/sbin/personal-vpn-rollback "${BACKUP_DIR}"

for path in "${PROJECT_PATHS[@]}"; do
    mode="0644"
    case "${path}" in
        /etc/wireguard/*) mode="0600" ;;
        /usr/local/sbin/*) mode="0755" ;;
    esac
    install -D -m "${mode}" "${STAGE_ROOT}${path}" "${path}"
done

systemctl daemon-reload
sysctl --system >/dev/null

systemctl enable --now personal-vpn-firewall.service
systemctl restart personal-vpn-firewall.service
systemctl enable --now "wg-quick@${VPN_INTERFACE}.service"
systemctl restart "wg-quick@${VPN_INTERFACE}.service"

if [[ -d "${STAGE_ROOT}/etc/personal-vpn" ]]; then
    systemctl stop personal-vpn-web personal-vpn-managerd 2>/dev/null || true
    install -d -m 0750 /etc/personal-vpn /etc/personal-vpn-web
    install -d -m 0700 /var/lib/personal-vpn /var/lib/personal-vpn-web
    cp -a "${STAGE_ROOT}/etc/personal-vpn/." /etc/personal-vpn/
    cp -a "${STAGE_ROOT}/etc/personal-vpn-web/." /etc/personal-vpn-web/
    cp -a "${STAGE_ROOT}/var/lib/personal-vpn/." /var/lib/personal-vpn/
    cp -a "${STAGE_ROOT}/var/lib/personal-vpn-web/." /var/lib/personal-vpn-web/
    for path in \
        etc/systemd/system/personal-vpn-managerd.service \
        etc/systemd/system/personal-vpn-web.service \
        usr/local/bin/personal-vpn-managerd \
        usr/local/bin/personal-vpn-managerctl \
        usr/local/bin/personal-vpn-web; do
        [[ -f "${STAGE_ROOT}/${path}" ]] &&
            install -D -m "$([[ "${path}" == usr/local/bin/* ]] && printf 0755 || printf 0644)" \
                "${STAGE_ROOT}/${path}" "/${path}"
    done
    if [[ -f "${STAGE_ROOT}/etc/nginx/sites-available/${WEB_DOMAIN}" ]]; then
        install -m 0644 \
            "${STAGE_ROOT}/etc/nginx/sites-available/${WEB_DOMAIN}" \
            "/etc/nginx/sites-available/${WEB_DOMAIN}"
        ln -sfn "/etc/nginx/sites-available/${WEB_DOMAIN}" \
            "/etc/nginx/sites-enabled/${WEB_DOMAIN}"
        nginx -t
        systemctl reload nginx
    fi
    chown -R root:root /var/lib/personal-vpn /etc/personal-vpn
    chown -R tnest-vpn-web:tnest-vpn-web /var/lib/personal-vpn-web
    systemctl daemon-reload
    systemctl enable --now personal-vpn-managerd personal-vpn-web
fi

printf 'ROLLBACK_UNIT=%s\n' "${ROLLBACK_UNIT}"
printf 'BACKUP_DIR=%s\n' "${BACKUP_DIR}"
printf 'SERVER_PUBLIC_KEY=%s\n' "$(wg pubkey </etc/wireguard/wg0.key)"
printf 'RESTORE=ok\n'
