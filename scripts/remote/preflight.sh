#!/usr/bin/env bash

set -Eeuo pipefail

VPN_PORT="${1:?VPN port is required}"
VPN_INTERFACE="${2:?VPN interface is required}"
REQUESTED_WAN="${3:-}"
EXPECTED_IPV4="${4:?expected public IPv4 is required}"
EXPECTED_IPV6="${5:?expected public IPv6 is required}"
IPV4_PROBE="1.1.1.1"
IPV6_PROBE="2606:4700:4700::1111"
OS_RELEASE_FILE="${PERSONAL_VPN_OS_RELEASE_FILE:-/etc/os-release}"

fail() {
    printf 'PREFLIGHT_ERROR=%s\n' "$*" >&2
    exit 1
}

canonical_global_ip() {
    local value="$1"
    local family="$2"

    python3 - "${value}" "${family}" <<'PY'
import ipaddress
import sys

try:
    address = ipaddress.ip_address(sys.argv[1])
except ValueError:
    raise SystemExit(1)

if address.version != int(sys.argv[2]) or not address.is_global:
    raise SystemExit(1)

print(address.compressed)
PY
}

probe_public_ip() {
    local family="$1"
    local curl_family
    local candidate
    local canonical
    local url
    local -a urls

    if [[ "${family}" == "4" ]]; then
        curl_family="-4"
        urls=(
            "https://api.ipify.org"
            "https://ipv4.icanhazip.com"
        )
    else
        curl_family="-6"
        urls=(
            "https://api6.ipify.org"
            "https://ipv6.icanhazip.com"
        )
    fi

    for url in "${urls[@]}"; do
        candidate="$(
            curl "${curl_family}" -fsS --connect-timeout 5 --max-time 10 \
                "${url}" 2>/dev/null |
                tr -d '[:space:]'
        )" || continue
        canonical="$(canonical_global_ip "${candidate}" "${family}" 2>/dev/null)" ||
            continue
        printf '%s\n' "${canonical}"
        return 0
    done
    return 1
}

validate_docker_user_rules() {
    local command_name="$1"
    local unexpected_rules

    unexpected_rules="$(
        sudo -n "${command_name}" -w -S DOCKER-USER |
            awk '
                $1 == "-A" &&
                $0 !~ /--comment "?personal-vpn:/ &&
                $0 != "-A DOCKER-USER -j RETURN" {
                    print
                }
            '
    )"
    [[ -z "${unexpected_rules}" ]] ||
        fail "Unsupported existing ${command_name} DOCKER-USER rules: ${unexpected_rules//$'\n'/, }"
}

list_web_ports() {
    local protocol="$1"
    local ss_flag

    if [[ "${protocol}" == "tcp" ]]; then
        ss_flag="-ltn"
    else
        ss_flag="-lun"
    fi

    ss -H "${ss_flag}" |
        awk -v protocol="${protocol}" '
            {
                endpoint = $4
                sub(/^.*:/, "", endpoint)
                if ((protocol == "tcp" &&
                     (endpoint == "80" || endpoint == "443")) ||
                    (protocol == "udp" && endpoint == "443")) {
                    print endpoint
                }
            }
        ' |
        sort -nu |
        paste -sd, -
}

[[ -r "${OS_RELEASE_FILE}" ]] || fail "${OS_RELEASE_FILE} is missing"
# shellcheck source=/dev/null
source "${OS_RELEASE_FILE}"
[[ "${ID:-}" == "ubuntu" && "${VERSION_ID:-}" == "24.04" ]] ||
    fail "Ubuntu 24.04 LTS is required; found ${ID:-unknown} ${VERSION_ID:-unknown}"

for required_command in curl ip ping python3 ss systemctl; do
    command -v "${required_command}" >/dev/null 2>&1 ||
        fail "${required_command} is required"
done
sudo -n true >/dev/null 2>&1 ||
    fail "SSH user needs non-interactive sudo"

expected_ipv4="$(
    canonical_global_ip "${EXPECTED_IPV4}" 4
)" || fail "Expected IPv4 is not globally routable"
expected_ipv6="$(
    canonical_global_ip "${EXPECTED_IPV6}" 6
)" || fail "Expected IPv6 is not globally routable"

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
ip -6 -o addr show dev "${detected_wan}" scope global |
    awk '{print $4}' |
    grep -Fqx "${expected_ipv6}/128" ||
    python3 - "${expected_ipv6}" "$(
        ip -6 -o addr show dev "${detected_wan}" scope global |
            awk '{print $4}' |
            paste -sd, -
    )" <<'PY' ||
import ipaddress
import sys

expected = ipaddress.ip_address(sys.argv[1])
configured = [
    ipaddress.ip_interface(value).ip
    for value in sys.argv[2].split(",")
    if value
]
raise SystemExit(0 if expected in configured else 1)
PY
    fail "Expected public IPv6 ${expected_ipv6} is not configured on ${detected_wan}"

ipv6_route="$(
    ip -6 route get "${IPV6_PROBE}" 2>/dev/null
)" || fail "No IPv6 route to the public Internet"
grep -Eq "(^|[[:space:]])dev[[:space:]]+${detected_wan}([[:space:]]|$)" \
    <<<"${ipv6_route}" ||
    fail "IPv6 Internet route does not use ${detected_wan}"

ping -4 -c 1 -W 5 "${IPV4_PROBE}" >/dev/null 2>&1 ||
    fail "IPv4 Internet probe failed"
ping -6 -c 1 -W 5 "${IPV6_PROBE}" >/dev/null 2>&1 ||
    fail "IPv6 Internet probe failed; ICMPv6 must work"

observed_ipv4="$(probe_public_ip 4)" ||
    fail "Could not determine the public IPv4 egress"
[[ "${observed_ipv4}" == "${expected_ipv4}" ]] ||
    fail "Public IPv4 egress is ${observed_ipv4}, expected ${expected_ipv4}"

observed_ipv6="$(probe_public_ip 6)" ||
    fail "Could not determine the public IPv6 egress"
[[ "${observed_ipv6}" == "${expected_ipv6}" ]] ||
    fail "Public IPv6 egress is ${observed_ipv6}, expected ${expected_ipv6}"

if systemctl is-active --quiet podman.service 2>/dev/null ||
    [[ -d /opt/1panel || -d /www/server/panel ]]; then
    fail "Podman, 1Panel or BT panel detected; this firewall layout is unsupported"
fi

docker_integration="no"
if systemctl is-active --quiet docker.service 2>/dev/null; then
    docker_integration="yes"
    for docker_command in docker iptables ip6tables; do
        command -v "${docker_command}" >/dev/null 2>&1 ||
            fail "Active Docker requires ${docker_command}"
    done
    sudo -n docker info >/dev/null 2>&1 ||
        fail "Active Docker is not accessible through sudo"
    sudo -n iptables -w -S DOCKER-USER >/dev/null 2>&1 ||
        fail "Docker IPv4 DOCKER-USER chain is missing"
    sudo -n ip6tables -w -S DOCKER-USER >/dev/null 2>&1 ||
        fail "Docker IPv6 DOCKER-USER chain is missing"
    validate_docker_user_rules iptables
    validate_docker_user_rules ip6tables
    sudo -n iptables -w -C FORWARD -j DOCKER-USER >/dev/null 2>&1 ||
        fail "Docker IPv4 FORWARD chain does not call DOCKER-USER"
    sudo -n ip6tables -w -C FORWARD -j DOCKER-USER >/dev/null 2>&1 ||
        fail "Docker IPv6 FORWARD chain does not call DOCKER-USER"
fi

web_tcp_ports="$(list_web_ports tcp)"
web_udp_ports="$(list_web_ports udp)"
[[ -n "${web_tcp_ports}" ]] ||
    fail "No native website listener was found on TCP 80 or 443"

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
            awk -v docker_integration="${docker_integration}" '
                $1 == "table" {
                    id = $2 " " $3
                    if (id != "inet personal_vpn_filter" &&
                        id != "ip personal_vpn_nat4" &&
                        id != "ip6 personal_vpn_nat6" &&
                        !(docker_integration == "yes" &&
                          (id == "ip nat" ||
                           id == "ip filter" ||
                           id == "ip6 nat" ||
                           id == "ip6 filter" ||
                           id == "ip raw"))) {
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
printf 'PUBLIC_IPV4=%s\n' "${observed_ipv4}"
printf 'PUBLIC_IPV6=%s\n' "${observed_ipv6}"
printf 'WEB_TCP_PORTS=%s\n' "${web_tcp_ports}"
printf 'WEB_UDP_PORTS=%s\n' "${web_udp_ports}"
printf 'DOCKER_INTEGRATION=%s\n' "${docker_integration}"
printf 'VPN_PORT=%s\n' "${VPN_PORT}"
printf 'PREFLIGHT=ok\n'
