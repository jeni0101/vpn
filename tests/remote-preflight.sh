#!/usr/bin/env bash

set -Eeuo pipefail

SOURCE_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d /tmp/personal-vpn-preflight.XXXXXXXX)"
MOCK_BIN="${TEST_ROOT}/bin"
trap 'rm -rf "${TEST_ROOT}"' EXIT
mkdir -p "${MOCK_BIN}"

cat >"${MOCK_BIN}/ip" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
case "$*" in
    "-4 route show default")
        printf 'default via 192.0.2.1 dev eth0\n'
        ;;
    "-6 route show default")
        printf 'default via fe80::1 dev eth0\n'
        ;;
    "-6 -o addr show dev eth0 scope global")
        printf '2: eth0 inet6 2606:4700:4700::1111/64 scope global\n'
        ;;
    "-6 route get 2606:4700:4700::1111")
        printf '2606:4700:4700::1111 from :: dev eth0 src 2606:4700:4700::1111\n'
        ;;
    *)
        printf 'Unexpected mock ip arguments: %s\n' "$*" >&2
        exit 2
        ;;
esac
EOF

cat >"${MOCK_BIN}/ss" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "$*" == *"sport = :51999"* ]]; then
    if [[ "${MOCK_PORT_OCCUPIED:-no}" == "yes" ]]; then
        printf 'UNCONN 0 0 0.0.0.0:51999 0.0.0.0:*\n'
    fi
elif [[ "$*" == *"-ltn"* ]]; then
    if [[ "${MOCK_NO_WEB:-no}" != "yes" ]]; then
        printf 'LISTEN 0 4096 0.0.0.0:80 0.0.0.0:*\n'
        printf 'LISTEN 0 4096 [::]:443 [::]:*\n'
    fi
elif [[ "$*" == *"-lun"* ]]; then
    printf 'UNCONN 0 0 0.0.0.0:443 0.0.0.0:*\n'
fi
EOF

cat >"${MOCK_BIN}/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ " $* " == *" -4 "* ]]; then
    printf '%s\n' "${MOCK_PUBLIC_IPV4:-8.8.8.8}"
else
    printf '%s\n' "${MOCK_PUBLIC_IPV6:-2606:4700:4700::1111}"
fi
EOF

cat >"${MOCK_BIN}/ping" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF

cat >"${MOCK_BIN}/systemctl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "$*" == *"is-active"* &&
      "$*" == *"docker.service"* &&
      "${MOCK_DOCKER:-no}" == "yes" ]]; then
    exit 0
fi
exit 1
EOF

cat >"${MOCK_BIN}/sudo" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${MOCK_SUDO_FAIL:-no}" == "yes" ]]; then
    exit 1
fi
if [[ "${1:-}" == "-n" ]]; then
    shift
fi
if [[ "${1:-}" == "true" ]]; then
    exit 0
fi
exec "$@"
EOF

cat >"${MOCK_BIN}/nft" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${MOCK_FOREIGN_NFT:-no}" == "yes" ]]; then
    printf 'table inet foreign_filter\n'
fi
EOF

cat >"${MOCK_BIN}/ufw" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${MOCK_UFW_ACTIVE:-no}" == "yes" ]]; then
    printf 'Status: active\n'
else
    printf 'Status: inactive\n'
fi
EOF

chmod 0755 "${MOCK_BIN}/"*

run_preflight() {
    PATH="${MOCK_BIN}:/usr/bin:/bin" \
        "${SOURCE_ROOT}/scripts/remote/preflight.sh" \
        51999 \
        wg0 \
        "" \
        8.8.8.8 \
        2606:4700:4700::1111
}

output="$(run_preflight)"
grep -qx 'PREFLIGHT=ok' <<<"${output}"
grep -qx 'WAN_INTERFACE=eth0' <<<"${output}"
grep -qx 'PUBLIC_IPV4=8.8.8.8' <<<"${output}"
grep -qx 'PUBLIC_IPV6=2606:4700:4700::1111' <<<"${output}"
grep -qx 'WEB_TCP_PORTS=80,443' <<<"${output}"
grep -qx 'WEB_UDP_PORTS=443' <<<"${output}"

if MOCK_NO_WEB=yes run_preflight >"${TEST_ROOT}/no-web.out" 2>&1; then
    printf 'Missing website listener was not rejected\n' >&2
    exit 1
fi
grep -q 'No native website listener' "${TEST_ROOT}/no-web.out"

if MOCK_PUBLIC_IPV6=2606:4700:4700::1001 \
    run_preflight >"${TEST_ROOT}/wrong-ipv6.out" 2>&1; then
    printf 'Wrong public IPv6 egress was not rejected\n' >&2
    exit 1
fi
grep -q 'Public IPv6 egress' "${TEST_ROOT}/wrong-ipv6.out"

if MOCK_FOREIGN_NFT=yes run_preflight >"${TEST_ROOT}/nft.out" 2>&1; then
    printf 'Foreign nftables table was not rejected\n' >&2
    exit 1
fi
grep -q 'Existing nftables tables require manual review' "${TEST_ROOT}/nft.out"

if MOCK_DOCKER=yes run_preflight >"${TEST_ROOT}/docker.out" 2>&1; then
    printf 'Docker was not rejected\n' >&2
    exit 1
fi
grep -q 'Docker, Podman, 1Panel or BT panel detected' "${TEST_ROOT}/docker.out"

if MOCK_SUDO_FAIL=yes run_preflight >"${TEST_ROOT}/sudo.out" 2>&1; then
    printf 'Missing non-interactive sudo was not rejected\n' >&2
    exit 1
fi
grep -q 'needs non-interactive sudo' "${TEST_ROOT}/sudo.out"

if MOCK_PORT_OCCUPIED=yes run_preflight >"${TEST_ROOT}/port.out" 2>&1; then
    printf 'Occupied WireGuard port was not rejected\n' >&2
    exit 1
fi
grep -q 'UDP port 51999 is already occupied' "${TEST_ROOT}/port.out"

printf 'ID=debian\nVERSION_ID=12\n' >"${TEST_ROOT}/os-release"
if PERSONAL_VPN_OS_RELEASE_FILE="${TEST_ROOT}/os-release" \
    run_preflight >"${TEST_ROOT}/os.out" 2>&1; then
    printf 'Wrong operating system was not rejected\n' >&2
    exit 1
fi
grep -q 'Ubuntu 24.04 LTS is required' "${TEST_ROOT}/os.out"

printf 'remote preflight success and failure paths: ok\n'
