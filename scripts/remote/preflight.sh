#!/usr/bin/env bash

set -Eeuo pipefail

VPN_PORT="${1:?VPN port is required}"
VPN_INTERFACE="${2:?VPN interface is required}"
REQUESTED_WAN="${3:-}"
IPV6_PROBE="2606:4700:4700::1111"

fail() {
    printf 'PREFLIGHT_ERROR=%s\n' "$*" >&2
    exit 1
}

[[ -r /etc/os-release ]] || fail "/etc/os-release is missing"
# shellcheck source=/dev/null
source /etc/os-release
[[ "${ID:-}" == "ubuntu" && "${VERSION_ID:-}" == "24.04" ]] ||
    fail "Ubuntu 24.04 LTS is required; found ${ID:-unknown} ${VERSION_ID:-unknown}"

command -v ip >/dev/null 2>&1 || fail "iproute2 is required"
command -v ss >/dev/null 2>&1 || fail "ss is required"
command -v systemctl >/dev/null 2>&1 || fail "systemd is required"
sudo -n true >/dev/null 2>&1 ||
    fail "SSH user needs non-interactive sudo"

mapfile -t wan_candidates < <(
    ip -4 route show default |
        awk '{for (i = 1; i <= NF; i++) if ($i == "dev") print $(i + 1)}' |
        sort -u
)
[[ "${#wan_candidates[@]}" -eq 1 ]] ||
    fail "Expected exactly one IPv4 default-route interface"
detected_wan="${wan_candidates[0]}"

if [[ -n "${REQUESTED_WAN}" && "${REQUESTED_WAN}" != "${detected_wan}" ]]; then
    fail "Configured WAN interface ${REQUESTED_WAN} does not match ${detected_wan}"
fi

ip -6 route show default | grep -q . ||
    fail "A public IPv6 default route is required"
ip -6 -o addr show dev "${detected_wan}" scope global | grep -q . ||
    fail "A global IPv6 address is required on ${detected_wan}"
ip -6 route get "${IPV6_PROBE}" >/dev/null 2>&1 ||
    fail "No IPv6 route to the public Internet"
ping -6 -c 1 -W 5 "${IPV6_PROBE}" >/dev/null 2>&1 ||
    fail "IPv6 Internet probe failed; ICMPv6 must work"

if ss -H -lun "sport = :${VPN_PORT}" | grep -q .; then
    current_port=""
    if command -v wg >/dev/null 2>&1; then
        current_port="$(sudo -n wg show "${VPN_INTERFACE}" listen-port 2>/dev/null || true)"
    fi
    [[ "${current_port}" == "${VPN_PORT}" ]] ||
        fail "UDP port ${VPN_PORT} is already occupied"
fi

if command -v ufw >/dev/null 2>&1 &&
    sudo -n ufw status 2>/dev/null | grep -q '^Status: active'; then
    fail "Active UFW detected; disable or migrate it before deployment"
fi

if systemctl is-active --quiet firewalld.service 2>/dev/null; then
    fail "Active firewalld detected; disable or migrate it before deployment"
fi

if command -v nft >/dev/null 2>&1; then
    unexpected_tables="$(
        sudo -n nft list tables 2>/dev/null |
            awk '
                $1 == "table" {
                    id = $2 " " $3
                    if (id != "inet personal_vpn_filter" &&
                        id != "ip personal_vpn_nat4" &&
                        id != "ip6 personal_vpn_nat6") {
                        print id
                    }
                }
            '
    )"
    [[ -z "${unexpected_tables}" ]] ||
        fail "Existing nftables tables require manual review: ${unexpected_tables//$'\n'/, }"
fi

printf 'OS=ubuntu-24.04\n'
printf 'WAN_INTERFACE=%s\n' "${detected_wan}"
printf 'GLOBAL_IPV6=%s\n' "$(
    ip -6 -o addr show dev "${detected_wan}" scope global |
        awk 'NR == 1 {print $4}'
)"
printf 'VPN_PORT=%s\n' "${VPN_PORT}"
printf 'PREFLIGHT=ok\n'
