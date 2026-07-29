#!/usr/bin/env bash

set -Eeuo pipefail

EXPECTED_IPV4="${1:?expected IPv4 is required}"
EXPECTED_IPV6="${2:?expected IPv6 is required}"
DOMAIN="${3:?domain is required}"

# shellcheck disable=SC1091 # /etc/os-release is guaranteed by the target OS.
[[ "$(. /etc/os-release && printf '%s' "${ID}:${VERSION_ID}")" == "ubuntu:24.04" ]] ||
    { printf 'Ubuntu 24.04 is required\n' >&2; exit 1; }
command -v nginx >/dev/null || { printf 'Nginx is not installed\n' >&2; exit 1; }
systemctl is-active --quiet nginx || { printf 'Nginx is not active\n' >&2; exit 1; }
systemctl is-active --quiet wg-quick@wg0 ||
    { printf 'wg-quick@wg0 is not active\n' >&2; exit 1; }
sudo -n true

if ss -H -ltn 'sport = :8787' | grep -q . &&
   ! systemctl is-active --quiet personal-vpn-web; then
    printf 'TCP 8787 is occupied by an unknown service\n' >&2
    exit 1
fi

python3 - "${DOMAIN}" "${EXPECTED_IPV4}" "${EXPECTED_IPV6}" <<'PY'
import ipaddress
import socket
import sys

domain, expected4, expected6 = sys.argv[1:]
records = socket.getaddrinfo(domain, 443, type=socket.SOCK_STREAM)
v4 = {item[4][0] for item in records if item[0] == socket.AF_INET}
v6 = {ipaddress.ip_address(item[4][0]).compressed for item in records if item[0] == socket.AF_INET6}
if expected4 not in v4:
    raise SystemExit(f"{domain} A does not resolve to {expected4}")
if ipaddress.ip_address(expected6).compressed not in v6:
    raise SystemExit(f"{domain} AAAA does not resolve to {expected6}")
PY

sudo -n nginx -T >/dev/null
printf 'WEB_PREFLIGHT=ok\n'
