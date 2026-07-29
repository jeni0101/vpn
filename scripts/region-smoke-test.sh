#!/usr/bin/env bash

set -Eeuo pipefail

NODE_SSH="${1:?node SSH alias is required}"
ENDPOINT_IP="${2:?endpoint IPv4 is required}"
ENDPOINT_PORT="${3:?endpoint UDP port is required}"
SERVER_PUBLIC_KEY="${4:?server WireGuard public key is required}"
PEER_IPV4="${5:?test peer IPv4 is required}"
PEER_IPV6="${6:?test peer IPv6 is required}"
DNS_SERVER="${7:?tunnel DNS server is required}"
EXPECTED_EGRESS="${8:?expected public IPv4 is required}"

[[ "${EUID}" -ne 0 ]] || {
    printf 'run as the management user with passwordless sudo, not as root\n' >&2
    exit 1
}
[[ "${NODE_SSH}" =~ ^[A-Za-z0-9._-]+$ ]] || {
    printf 'invalid node SSH alias\n' >&2
    exit 1
}
[[ "${ENDPOINT_IP}" =~ ^[0-9.]+$ ]] || {
    printf 'endpoint must be IPv4\n' >&2
    exit 1
}
[[ "${ENDPOINT_PORT}" =~ ^[0-9]+$ ]] || {
    printf 'invalid endpoint port\n' >&2
    exit 1
}
[[ "${SERVER_PUBLIC_KEY}" =~ ^[A-Za-z0-9+/]{43}=$ ]] || {
    printf 'invalid WireGuard server public key\n' >&2
    exit 1
}
for command in ip nft scp ssh sudo wg curl getent; do
    command -v "${command}" >/dev/null || {
        printf 'missing command: %s\n' "${command}" >&2
        exit 1
    }
done

test_id="tnestmy$$"
namespace="${test_id}"
veth_host="tmh$$"
veth_namespace="tmn$$"
nft_table="tnest_smoke_$$"
local_stage="$(mktemp -d /tmp/tnest-region-smoke.XXXXXX)"
remote_stage=""
server_peer_added=false
namespace_added=false
nft_added=false
original_forward="$(sysctl -n net.ipv4.ip_forward)"
forward_changed=false

cleanup() {
    local exit_code=$?
    trap - EXIT
    set +e
    if [[ "${server_peer_added}" == "true" ]]; then
        public_key="$(<"${local_stage}/peer.pub")"
        ssh "${NODE_SSH}" sudo wg set wg0 peer "${public_key}" remove
    fi
    if [[ -n "${remote_stage}" &&
          "${remote_stage}" == /tmp/tnest-region-smoke.* ]]; then
        ssh "${NODE_SSH}" sudo rm -rf -- "${remote_stage}"
    fi
    if [[ "${namespace_added}" == "true" ]]; then
        sudo ip netns delete "${namespace}"
        sudo rm -rf -- "/etc/netns/${namespace}"
    fi
    if [[ "${nft_added}" == "true" ]]; then
        sudo nft delete table ip "${nft_table}"
    fi
    if [[ "${forward_changed}" == "true" ]]; then
        sudo sysctl -q -w "net.ipv4.ip_forward=${original_forward}"
    fi
    rm -rf -- "${local_stage}"
    if [[ "${exit_code}" -ne 0 ]]; then
        printf 'REGION_SMOKE=failed_and_cleaned\n' >&2
    fi
    exit "${exit_code}"
}
trap cleanup EXIT

umask 077
wg genkey >"${local_stage}/peer.key"
wg pubkey <"${local_stage}/peer.key" >"${local_stage}/peer.pub"
wg genpsk >"${local_stage}/peer.psk"

remote_stage="$(
    ssh "${NODE_SSH}" mktemp -d /tmp/tnest-region-smoke.XXXXXX
)"
[[ "${remote_stage}" == /tmp/tnest-region-smoke.* ]] || {
    printf 'unexpected remote stage path\n' >&2
    exit 1
}
scp -q "${local_stage}/peer.psk" "${NODE_SSH}:${remote_stage}/peer.psk"
# The validated remote path is intentionally expanded by this management host.
# shellcheck disable=SC2029
ssh "${NODE_SSH}" chmod 0600 "${remote_stage}/peer.psk"

public_key="$(<"${local_stage}/peer.pub")"
# All validated peer arguments are intentionally expanded by this management host.
# shellcheck disable=SC2029
ssh "${NODE_SSH}" sudo wg set wg0 \
    peer "${public_key}" \
    preshared-key "${remote_stage}/peer.psk" \
    allowed-ips "${PEER_IPV4}/32,${PEER_IPV6}/128"
server_peer_added=true

if [[ "${original_forward}" != "1" ]]; then
    sudo sysctl -q -w net.ipv4.ip_forward=1
    forward_changed=true
fi

uplink="$(
    ip -4 route show default |
        awk 'NR == 1 { for (i = 1; i <= NF; i++) if ($i == "dev") print $(i+1) }'
)"
[[ -n "${uplink}" ]] || {
    printf 'cannot determine management uplink\n' >&2
    exit 1
}

sudo nft add table ip "${nft_table}"
nft_added=true
sudo nft "add chain ip ${nft_table} postrouting { type nat hook postrouting priority srcnat; policy accept; }"
sudo nft add rule ip "${nft_table}" postrouting \
    ip saddr 192.0.2.0/30 oifname "${uplink}" masquerade

sudo install -d -m 0755 "/etc/netns/${namespace}"
printf 'nameserver %s\n' "${DNS_SERVER}" |
    sudo tee "/etc/netns/${namespace}/resolv.conf" >/dev/null

sudo ip netns add "${namespace}"
namespace_added=true
sudo ip link add "${veth_host}" type veth peer name "${veth_namespace}"
sudo ip link set "${veth_namespace}" netns "${namespace}"
sudo ip address add 192.0.2.1/30 dev "${veth_host}"
sudo ip link set "${veth_host}" up
sudo ip netns exec "${namespace}" ip link set lo up
sudo ip netns exec "${namespace}" ip address add 192.0.2.2/30 dev "${veth_namespace}"
sudo ip netns exec "${namespace}" ip link set "${veth_namespace}" up
sudo ip netns exec "${namespace}" ip route add \
    "${ENDPOINT_IP}/32" via 192.0.2.1 dev "${veth_namespace}"

sudo ip netns exec "${namespace}" ip link add wgtest type wireguard
sudo ip netns exec "${namespace}" wg set wgtest \
    private-key "${local_stage}/peer.key" \
    peer "${SERVER_PUBLIC_KEY}" \
    preshared-key "${local_stage}/peer.psk" \
    endpoint "${ENDPOINT_IP}:${ENDPOINT_PORT}" \
    allowed-ips '0.0.0.0/0,::/0' \
    persistent-keepalive 25
sudo ip netns exec "${namespace}" ip address add "${PEER_IPV4}/32" dev wgtest
sudo ip netns exec "${namespace}" ip -6 address add "${PEER_IPV6}/128" dev wgtest
sudo ip netns exec "${namespace}" ip link set wgtest mtu 1420 up
sudo ip netns exec "${namespace}" ip route add default dev wgtest
sudo ip netns exec "${namespace}" ip -6 route add default dev wgtest

handshake=0
for _ in {1..15}; do
    handshake="$(
        sudo ip netns exec "${namespace}" wg show wgtest latest-handshakes |
            awk 'NR == 1 { print $2 }'
    )"
    [[ "${handshake}" =~ ^[0-9]+$ && "${handshake}" -gt 0 ]] && break
    sleep 1
done
[[ "${handshake}" =~ ^[0-9]+$ && "${handshake}" -gt 0 ]] || {
    printf 'WireGuard handshake timed out\n' >&2
    exit 1
}

egress="$(
    sudo ip netns exec "${namespace}" \
        curl --fail --silent --show-error --max-time 10 \
        https://api.ipify.org
)"
[[ "${egress}" == "${EXPECTED_EGRESS}" ]] || {
    printf 'unexpected public IPv4: %s\n' "${egress}" >&2
    exit 1
}

sudo ip netns exec "${namespace}" \
    getent ahostsv4 example.com >/dev/null

if sudo ip netns exec "${namespace}" \
    curl --fail --silent --show-error --max-time 5 -6 \
    https://api64.ipify.org >/dev/null 2>&1; then
    printf 'public IPv6 unexpectedly succeeded\n' >&2
    exit 1
fi

transfer="$(
    sudo ip netns exec "${namespace}" wg show wgtest transfer |
        awk 'NR == 1 { print $2 "/" $3 }'
)"
printf 'REGION_SMOKE=ok\n'
printf 'HANDSHAKE=ok\n'
printf 'IPV4_EGRESS=%s\n' "${egress}"
printf 'DNS=ok\n'
printf 'PUBLIC_IPV6=blocked\n'
printf 'TRANSFER=%s\n' "${transfer}"
