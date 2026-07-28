#!/usr/bin/env bash

set -Eeuo pipefail

[[ "${EUID}" -eq 0 ]] || {
    printf 'The network namespace lab must run as root\n' >&2
    exit 1
}

for command_name in ip ip6tables iptables wg nft nc ping python3 sysctl; do
    command -v "${command_name}" >/dev/null 2>&1 || {
        printf 'Missing lab command: %s\n' "${command_name}" >&2
        exit 1
    }
done

RUN_ID="$$"
NS_WAN="pvpn-wan-${RUN_ID}"
NS_SERVER="pvpn-srv-${RUN_ID}"
NS_C1="pvpn-c1-${RUN_ID}"
NS_C2="pvpn-c2-${RUN_ID}"
TEMP_DIR="$(mktemp -d /tmp/personal-vpn-lab.XXXXXXXX)"
WEB_PID=""

cleanup() {
    local namespace
    if [[ -n "${WEB_PID}" ]]; then
        kill "${WEB_PID}" >/dev/null 2>&1 || true
        wait "${WEB_PID}" 2>/dev/null || true
    fi
    for namespace in "${NS_C2}" "${NS_C1}" "${NS_SERVER}" "${NS_WAN}"; do
        ip netns delete "${namespace}" >/dev/null 2>&1 || true
    done
    rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT

connect_namespace() {
    local namespace="$1"
    local wan_interface="$2"
    local ipv4="$3"
    local ipv6="$4"

    ip link add "${wan_interface}" type veth peer name eth0 netns "${namespace}"
    ip link set "${wan_interface}" netns "${NS_WAN}"
    ip -n "${NS_WAN}" link set "${wan_interface}" master br0
    ip -n "${NS_WAN}" link set "${wan_interface}" up

    ip -n "${namespace}" link set lo up
    ip -n "${namespace}" link set eth0 up
    ip -n "${namespace}" address add "${ipv4}/24" dev eth0
    ip -n "${namespace}" -6 address add "${ipv6}/64" dev eth0
    ip -n "${namespace}" route add default via 192.0.2.1
    ip -n "${namespace}" -6 route add default via 2001:db8:1::1
}

assert_reachable() {
    local namespace="$1"
    local family="$2"
    local target="$3"
    local attempt
    local -a family_args=()

    if [[ "${family}" == "ipv6" ]]; then
        family_args=(-6)
    fi
    for ((attempt = 1; attempt <= 10; attempt++)); do
        if ip netns exec "${namespace}" \
            ping "${family_args[@]}" -c 1 -W 1 "${target}" >/dev/null 2>&1; then
            return 0
        fi
        sleep 0.2
    done
    printf '%s could not reach %s over %s\n' \
        "${namespace}" "${target}" "${family}" >&2
    return 1
}

for namespace in "${NS_WAN}" "${NS_SERVER}" "${NS_C1}" "${NS_C2}"; do
    ip netns add "${namespace}"
done

ip -n "${NS_WAN}" link set lo up
ip -n "${NS_WAN}" link add br0 type bridge
ip -n "${NS_WAN}" address add 192.0.2.1/24 dev br0
ip -n "${NS_WAN}" -6 address add 2001:db8:1::1/64 dev br0
ip -n "${NS_WAN}" link set br0 up
ip -n "${NS_WAN}" address add 198.51.100.1/32 dev lo
ip -n "${NS_WAN}" -6 address add 2001:db8:ffff::1/128 dev lo

connect_namespace "${NS_SERVER}" ws0 192.0.2.2 2001:db8:1::2
connect_namespace "${NS_C1}" wc1 192.0.2.10 2001:db8:1::10
connect_namespace "${NS_C2}" wc2 192.0.2.11 2001:db8:1::11

ip netns exec "${NS_SERVER}" \
    python3 -m http.server 80 --bind 0.0.0.0 \
    >"${TEMP_DIR}/website.log" 2>&1 &
WEB_PID="$!"
for ((attempt = 1; attempt <= 20; attempt++)); do
    if ip netns exec "${NS_WAN}" \
        nc -z -w 1 192.0.2.2 80 >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
ip netns exec "${NS_WAN}" nc -z -w 1 192.0.2.2 80

umask 077
wg genkey >"${TEMP_DIR}/server.key"
wg pubkey <"${TEMP_DIR}/server.key" >"${TEMP_DIR}/server.pub"
wg genkey >"${TEMP_DIR}/c1.key"
wg pubkey <"${TEMP_DIR}/c1.key" >"${TEMP_DIR}/c1.pub"
wg genpsk >"${TEMP_DIR}/c1.psk"
wg genkey >"${TEMP_DIR}/c2.key"
wg pubkey <"${TEMP_DIR}/c2.key" >"${TEMP_DIR}/c2.pub"
wg genpsk >"${TEMP_DIR}/c2.psk"

for namespace in "${NS_SERVER}" "${NS_C1}" "${NS_C2}"; do
    ip -n "${namespace}" link add wg0 type wireguard
done

ip netns exec "${NS_SERVER}" wg set wg0 \
    private-key "${TEMP_DIR}/server.key" \
    listen-port 51999 \
    peer "$(<"${TEMP_DIR}/c1.pub")" \
    preshared-key "${TEMP_DIR}/c1.psk" \
    allowed-ips 10.66.0.10/32,fd66:66:66::10/128 \
    peer "$(<"${TEMP_DIR}/c2.pub")" \
    preshared-key "${TEMP_DIR}/c2.psk" \
    allowed-ips 10.66.0.11/32,fd66:66:66::11/128
ip -n "${NS_SERVER}" address add 10.66.0.1/24 dev wg0
ip -n "${NS_SERVER}" -6 address add fd66:66:66::1/64 dev wg0
ip -n "${NS_SERVER}" link set wg0 up

configure_client() {
    local namespace="$1"
    local private_key="$2"
    local psk="$3"
    local ipv4="$4"
    local ipv6="$5"

    ip netns exec "${namespace}" wg set wg0 \
        private-key "${private_key}" \
        peer "$(<"${TEMP_DIR}/server.pub")" \
        preshared-key "${psk}" \
        endpoint 192.0.2.2:51999 \
        allowed-ips \
            10.66.0.0/24,fd66:66:66::/64,198.51.100.1/32,2001:db8:ffff::1/128 \
        persistent-keepalive 25
    ip -n "${namespace}" address add "${ipv4}/32" dev wg0
    ip -n "${namespace}" -6 address add "${ipv6}/128" dev wg0
    ip -n "${namespace}" link set wg0 up
    ip -n "${namespace}" route add 198.51.100.1/32 dev wg0
    ip -n "${namespace}" -6 route add 2001:db8:ffff::1/128 dev wg0
}

configure_client \
    "${NS_C1}" "${TEMP_DIR}/c1.key" "${TEMP_DIR}/c1.psk" \
    10.66.0.10 fd66:66:66::10
configure_client \
    "${NS_C2}" "${TEMP_DIR}/c2.key" "${TEMP_DIR}/c2.psk" \
    10.66.0.11 fd66:66:66::11

ip netns exec "${NS_SERVER}" sysctl -qw net.ipv4.ip_forward=1
ip netns exec "${NS_SERVER}" sysctl -qw net.ipv6.conf.all.forwarding=1

# Simulate Docker's FORWARD policy and its supported user extension point.
for command_name in iptables ip6tables; do
    ip netns exec "${NS_SERVER}" "${command_name}" -w -P FORWARD DROP
    ip netns exec "${NS_SERVER}" "${command_name}" -w -N DOCKER-USER
    ip netns exec "${NS_SERVER}" "${command_name}" -w \
        -I FORWARD 1 -j DOCKER-USER
    ip netns exec "${NS_SERVER}" "${command_name}" -w \
        -A DOCKER-USER \
        -i wg0 -o wg0 \
        -m comment --comment "personal-vpn:peer-isolation" \
        -j DROP
    ip netns exec "${NS_SERVER}" "${command_name}" -w \
        -A DOCKER-USER \
        -i wg0 -o eth0 \
        -m comment --comment "personal-vpn:internet-egress" \
        -j ACCEPT
    ip netns exec "${NS_SERVER}" "${command_name}" -w \
        -A DOCKER-USER \
        -o wg0 \
        -m conntrack --ctstate ESTABLISHED,RELATED \
        -m comment --comment "personal-vpn:peer-return" \
        -j ACCEPT
    ip netns exec "${NS_SERVER}" "${command_name}" -w \
        -A DOCKER-USER \
        -o wg0 \
        -m comment --comment "personal-vpn:peer-ingress-drop" \
        -j DROP
done

cat >"${TEMP_DIR}/lab.nft" <<'EOF'
table inet personal_vpn_filter {
    chain input {
        type filter hook input priority filter; policy drop;

        iifname "lo" accept
        ct state invalid drop
        ct state established,related accept
        ip protocol icmp accept
        ip6 nexthdr ipv6-icmp accept
        tcp dport { 22, 80, 443 } accept
        udp dport { 443, 51999 } accept
    }

    chain forward {
        type filter hook forward priority filter; policy accept;
        iifname "wg0" oifname "wg0" drop
        iifname "wg0" oifname "eth0" accept
        iifname "wg0" drop
        oifname "wg0" ct state established,related accept
        oifname "wg0" drop
    }
}
table ip personal_vpn_nat4 {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        ip saddr 10.66.0.0/24 oifname "eth0" masquerade
    }
}
table ip6 personal_vpn_nat6 {
    chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        ip6 saddr fd66:66:66::/64 oifname "eth0" masquerade
    }
}
EOF

ip netns exec "${NS_SERVER}" nft -c -f "${TEMP_DIR}/lab.nft"
ip netns exec "${NS_SERVER}" nft -f "${TEMP_DIR}/lab.nft"
for spec in \
    "inet personal_vpn_filter" \
    "ip personal_vpn_nat4" \
    "ip6 personal_vpn_nat6"; do
    read -r family table_name <<<"${spec}"
    ip netns exec "${NS_SERVER}" nft delete table "${family}" "${table_name}"
done
ip netns exec "${NS_SERVER}" nft -f "${TEMP_DIR}/lab.nft"
ip netns exec "${NS_WAN}" nc -z -w 1 192.0.2.2 80

assert_reachable "${NS_C1}" ipv4 198.51.100.1
assert_reachable "${NS_C1}" ipv6 2001:db8:ffff::1
assert_reachable "${NS_C2}" ipv4 198.51.100.1
assert_reachable "${NS_C2}" ipv6 2001:db8:ffff::1

if ip netns exec "${NS_C1}" ping -c 1 -W 1 10.66.0.11 >/dev/null 2>&1; then
    printf 'Peer isolation test failed for IPv4\n' >&2
    exit 1
fi
if ip netns exec "${NS_C1}" ping -6 -c 1 -W 1 fd66:66:66::11 >/dev/null 2>&1; then
    printf 'Peer isolation test failed for IPv6\n' >&2
    exit 1
fi

ip netns exec "${NS_SERVER}" wg set wg0 \
    peer "$(<"${TEMP_DIR}/c2.pub")" remove
if ip netns exec "${NS_C2}" ping -c 1 -W 1 198.51.100.1 >/dev/null 2>&1; then
    printf 'Peer revocation test failed for IPv4\n' >&2
    exit 1
fi
if ip netns exec "${NS_C2}" ping -6 -c 1 -W 1 2001:db8:ffff::1 >/dev/null 2>&1; then
    printf 'Peer revocation test failed for IPv6\n' >&2
    exit 1
fi

printf 'dual-stack namespace lab: ok\n'
