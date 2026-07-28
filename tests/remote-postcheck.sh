#!/usr/bin/env bash

set -Eeuo pipefail

SOURCE_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d /tmp/personal-vpn-postcheck.XXXXXXXX)"
MOCK_BIN="${TEST_ROOT}/bin"
trap 'rm -rf "${TEST_ROOT}"' EXIT
mkdir -p "${MOCK_BIN}"

cat >"${MOCK_BIN}/sysctl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "$*" == *"net.ipv4.ip_forward"* &&
      "${MOCK_IPV4_FORWARDING:-yes}" == "no" ]]; then
    printf '0\n'
else
    printf '1\n'
fi
EOF

cat >"${MOCK_BIN}/systemctl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${MOCK_SERVICE_FAILURE:-no}" == "yes" ]]; then
    exit 1
fi
exit 0
EOF

cat >"${MOCK_BIN}/wg" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF

cat >"${MOCK_BIN}/nft" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${MOCK_NFT_MISSING:-no}" == "yes" ]]; then
    exit 1
fi
exit 0
EOF

cat >"${MOCK_BIN}/ss" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "$*" == *"-ltn"* ]]; then
    printf 'LISTEN 0 4096 0.0.0.0:80 0.0.0.0:*\n'
    if [[ "${MOCK_HTTPS_MISSING:-no}" != "yes" ]]; then
        printf 'LISTEN 0 4096 [::]:443 [::]:*\n'
    fi
elif [[ "$*" == *"-lun"* ]]; then
    printf 'UNCONN 0 0 0.0.0.0:443 0.0.0.0:*\n'
fi
EOF

for command_name in iptables ip6tables; do
    cat >"${MOCK_BIN}/${command_name}" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${MOCK_DOCKER_RULE_MISSING:-no}" == "yes" &&
      "$*" == *"personal-vpn:internet-egress"* ]]; then
    exit 1
fi
exit 0
EOF
done

chmod 0755 "${MOCK_BIN}/"*

run_postcheck() {
    PATH="${MOCK_BIN}:/usr/bin:/bin" \
        "${SOURCE_ROOT}/scripts/remote/postcheck.sh" \
        wg0 \
        eth0 \
        "${MOCK_DOCKER_INTEGRATION:-no}" \
        80,443 \
        443
}

output="$(run_postcheck)"
grep -qx 'POSTCHECK=ok' <<<"${output}"

if MOCK_HTTPS_MISSING=yes \
    run_postcheck >"${TEST_ROOT}/https.out" 2>&1; then
    printf 'Missing HTTPS listener was not rejected\n' >&2
    exit 1
fi
grep -q 'Website tcp port 443 stopped listening' "${TEST_ROOT}/https.out"

if MOCK_IPV4_FORWARDING=no \
    run_postcheck >"${TEST_ROOT}/forwarding.out" 2>&1; then
    printf 'Disabled IPv4 forwarding was not rejected\n' >&2
    exit 1
fi
grep -q 'IPv4 forwarding is disabled' "${TEST_ROOT}/forwarding.out"

if MOCK_NFT_MISSING=yes \
    run_postcheck >"${TEST_ROOT}/nft.out" 2>&1; then
    printf 'Missing nftables table was not rejected\n' >&2
    exit 1
fi
grep -q 'Missing nftables table' "${TEST_ROOT}/nft.out"

MOCK_DOCKER_INTEGRATION=yes run_postcheck >/dev/null
if MOCK_DOCKER_INTEGRATION=yes \
    MOCK_DOCKER_RULE_MISSING=yes \
    run_postcheck >"${TEST_ROOT}/docker.out" 2>&1; then
    printf 'Missing Docker compatibility rule was not rejected\n' >&2
    exit 1
fi
grep -q 'Missing Docker compatibility rule' "${TEST_ROOT}/docker.out"

printf 'remote post-deploy verification paths: ok\n'
