#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

mapfile -t shell_files < <(
    find scripts tests config \
        -type f \
        \( -name '*.sh' -o -name 'vpnctl' \) \
        -print |
        sort
)

for file in "${shell_files[@]}"; do
    bash -n "${file}"
done
printf 'bash syntax: ok (%s files)\n' "${#shell_files[@]}"

if command -v shellcheck >/dev/null 2>&1; then
    shellcheck -x "${shell_files[@]}"
    printf 'shellcheck: ok\n'
else
    printf 'shellcheck: skipped (not installed)\n' >&2
fi

if rg -n \
    '^(PrivateKey|PresharedKey)[[:space:]]*=[[:space:]]*[A-Za-z0-9+/]{43}=$' \
    --glob '!secrets/**' \
    --glob '!backups/**' \
    .; then
    printf 'Possible committed WireGuard secret found\n' >&2
    exit 1
fi
printf 'WireGuard secret scan: ok\n'

if rg -n -i \
    '(@cloudflare/speedtest|speedtest|iperf|downloadTest|uploadTest|[0-9][[:space:]]*Mbps)' \
    web/src clients server config web/package.json web/package-lock.json; then
    printf 'Forbidden bandwidth-test implementation found\n' >&2
    exit 1
fi
printf 'bandwidth-test feature scan: ok\n'

for token in \
    '@@VPN_SERVER_IPV4@@' \
    '@@VPN_SERVER_IPV6@@' \
    '@@VPN_PORT@@' \
    '@@SERVER_PRIVATE_KEY@@' \
    '@@PEERS@@'; do
    grep -q "${token}" config/wg0.conf.tpl ||
        {
            printf 'Missing template token: %s\n' "${token}" >&2
            exit 1
        }
done
printf 'template tokens: ok\n'

if [[ -f config/local.env ]]; then
    mode="$(stat -c '%a' config/local.env)"
    [[ "${mode}" == "600" ]] ||
        {
            printf 'config/local.env must be mode 600, found %s\n' "${mode}" >&2
            exit 1
        }
fi

for directory in state secrets backups; do
    mode="$(stat -c '%a' "${directory}")"
    [[ "${mode}" == "700" ]] ||
        {
            printf '%s must be mode 700, found %s\n' "${directory}" "${mode}" >&2
            exit 1
        }
done
printf 'local permissions: ok\n'
