#!/usr/bin/env bats

setup() {
    ROOT_DIR="$(cd -- "${BATS_TEST_DIRNAME}/.." && pwd)"
    export ROOT_DIR
    VPNCTL_CONFIG="${BATS_TEST_TMPDIR}/local.env"
    cp "${ROOT_DIR}/config/local.env.example" "${VPNCTL_CONFIG}"
    chmod 0600 "${VPNCTL_CONFIG}"
    export VPNCTL_CONFIG
    # shellcheck source=scripts/lib/common.sh
    source "${ROOT_DIR}/scripts/lib/common.sh"
    # shellcheck source=scripts/lib/config.sh
    source "${ROOT_DIR}/scripts/lib/config.sh"
}

@test "fixed peer IPv4 mapping is stable" {
    [ "$(peer_ipv4 windows)" = "10.66.0.10" ]
    [ "$(peer_ipv4 macos)" = "10.66.0.11" ]
    [ "$(peer_ipv4 ios)" = "10.66.0.12" ]
    [ "$(peer_ipv4 android)" = "10.66.0.13" ]
}

@test "fixed peer IPv6 mapping is stable" {
    [ "$(peer_ipv6 windows)" = "fd66:66:66::10" ]
    [ "$(peer_ipv6 macos)" = "fd66:66:66::11" ]
    [ "$(peer_ipv6 ios)" = "fd66:66:66::12" ]
    [ "$(peer_ipv6 android)" = "fd66:66:66::13" ]
}

@test "only the four planned peer names are accepted" {
    run assert_safe_peer_name linux
    [ "${status}" -ne 0 ]
    run assert_safe_peer_name ios
    [ "${status}" -eq 0 ]
}

@test "WireGuard key shape validation rejects placeholders" {
    run is_wireguard_key "__PRIVATE_KEY__"
    [ "${status}" -ne 0 ]
}

@test "vpnctl help documents the public interface" {
    run "${ROOT_DIR}/scripts/vpnctl" --help
    [ "${status}" -eq 0 ]
    [[ "${output}" == *"vpnctl server preflight"* ]]
    [[ "${output}" == *"vpnctl config prepare"* ]]
    [[ "${output}" == *"vpnctl server bootstrap"* ]]
    [[ "${output}" == *"vpnctl peer rotate activate"* ]]
}

@test "public IP validators distinguish families and private space" {
    run is_global_ipv4 8.8.8.8
    [ "${status}" -eq 0 ]
    run is_global_ipv4 10.0.0.1
    [ "${status}" -ne 0 ]
    run is_global_ipv6 2606:4700:4700::1111
    [ "${status}" -eq 0 ]
    run is_global_ipv6 fd66:66:66::1
    [ "${status}" -ne 0 ]
}

@test "server rendering keeps the private key on the server" {
    # shellcheck source=scripts/lib/render.sh
    source "${ROOT_DIR}/scripts/lib/render.sh"
    output="${BATS_TEST_TMPDIR}/wg0.conf"
    render_server_config "${output}"
    grep -q '^Address = 10.66.0.1/24, fd66:66:66::1/64$' "${output}"
    grep -q '^ListenPort = 51999$' "${output}"
    grep -q '^PrivateKey = __SERVER_PRIVATE_KEY__$' "${output}"
}

@test "nftables rendering includes dual-stack NAT and website ports" {
    # shellcheck source=scripts/lib/render.sh
    source "${ROOT_DIR}/scripts/lib/render.sh"
    output="${BATS_TEST_TMPDIR}/personal-vpn.nft"
    render_nftables_config "${output}" eth0
    grep -q '^table ip personal_vpn_nat4' "${output}"
    grep -q '^table ip6 personal_vpn_nat6' "${output}"
    grep -q 'tcp dport { 22, 80, 443 } accept' "${output}"
    grep -q 'udp dport { 443, 51999 } accept' "${output}"
}
