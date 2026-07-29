#!/usr/bin/env bash

set -Eeuo pipefail

WEB_DOMAIN="${1:-vpn.example.com}"
[[ "${WEB_DOMAIN}" =~ ^[A-Za-z0-9.-]+$ && "${WEB_DOMAIN}" == *.* ]] ||
    { printf 'invalid management domain\n' >&2; exit 1; }

paths=(
    etc/wireguard/wg0.key
    etc/wireguard/wg0.conf
    etc/nftables.d/personal-vpn.nft
    etc/sysctl.d/70-personal-vpn.conf
    etc/systemd/system/personal-vpn-firewall.service
    usr/local/sbin/personal-vpn-firewall
    usr/local/sbin/personal-vpn-rollback
)
optional=(
    etc/personal-vpn
    etc/personal-vpn-web
    var/lib/personal-vpn/manager.db
    var/lib/personal-vpn/manager.db-wal
    var/lib/personal-vpn/manager.db-shm
    var/lib/personal-vpn-web
    etc/systemd/system/personal-vpn-managerd.service
    etc/systemd/system/personal-vpn-web.service
    usr/local/bin/personal-vpn-managerd
    usr/local/bin/personal-vpn-managerctl
    usr/local/bin/personal-vpn-web
    "etc/nginx/sites-available/${WEB_DOMAIN}"
    "etc/nginx/sites-enabled/${WEB_DOMAIN}"
)
restart_manager=no
restart_web=no
systemctl is-active --quiet personal-vpn-web && restart_web=yes
systemctl is-active --quiet personal-vpn-managerd && restart_manager=yes
cleanup() {
    if [[ "${restart_manager}" == yes ]]; then
        systemctl start personal-vpn-managerd >/dev/null 2>&1 || true
    fi
    if [[ "${restart_web}" == yes ]]; then
        systemctl start personal-vpn-web >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT
[[ "${restart_web}" == yes ]] && systemctl stop personal-vpn-web
[[ "${restart_manager}" == yes ]] && systemctl stop personal-vpn-managerd

for path in "${optional[@]}"; do
    [[ -e "/${path}" || -L "/${path}" ]] && paths+=("${path}")
done
tar -C / -czf - "${paths[@]}"
