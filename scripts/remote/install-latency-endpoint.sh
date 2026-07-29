#!/usr/bin/env bash

set -Eeuo pipefail

DOMAIN="${1:?latency domain is required}"
FINAL_CONFIG="${2:?final nginx config is required}"

[[ "${EUID}" -eq 0 ]] || {
    printf 'must run as root\n' >&2
    exit 1
}
[[ "${DOMAIN}" =~ ^[A-Za-z0-9.-]+$ && "${DOMAIN}" == *.* ]] || {
    printf 'invalid latency domain\n' >&2
    exit 1
}
[[ -f "${FINAL_CONFIG}" ]] || {
    printf 'missing nginx config: %s\n' "${FINAL_CONFIG}" >&2
    exit 1
}
grep -Fq "server_name ${DOMAIN};" "${FINAL_CONFIG}" || {
    printf 'nginx config does not contain the requested server_name\n' >&2
    exit 1
}

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="/var/backups/personal-vpn/latency-${DOMAIN}-${timestamp}"
site_available="/etc/nginx/sites-available/${DOMAIN}"
site_enabled="/etc/nginx/sites-enabled/${DOMAIN}"
bootstrap="$(mktemp)"
had_available=false
had_enabled=false

install -d -m 0700 "${backup_dir}"

rollback() {
    local exit_code=$?
    rm -f "${bootstrap}"
    if [[ "${exit_code}" -eq 0 ]]; then
        return
    fi

    rm -f "${site_available}" "${site_enabled}"
    if [[ "${had_available}" == "true" ]]; then
        cp -a "${backup_dir}/site-available" "${site_available}"
    fi
    if [[ "${had_enabled}" == "true" ]]; then
        cp -a "${backup_dir}/site-enabled" "${site_enabled}"
    fi
    if command -v nginx >/dev/null 2>&1 && nginx -t >/dev/null 2>&1; then
        systemctl reload nginx || true
    fi
    printf 'LATENCY_INSTALL=rolled_back\n' >&2
    printf 'BACKUP_DIR=%s\n' "${backup_dir}" >&2
    exit "${exit_code}"
}
trap rollback EXIT

if [[ -e "${site_available}" || -L "${site_available}" ]]; then
    cp -a "${site_available}" "${backup_dir}/site-available"
    had_available=true
fi
if [[ -e "${site_enabled}" || -L "${site_enabled}" ]]; then
    cp -a "${site_enabled}" "${backup_dir}/site-enabled"
    had_enabled=true
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
    nginx certbot python3-certbot-nginx ca-certificates curl

install -d -m 0755 /var/www/html
cat >"${bootstrap}" <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name ${DOMAIN};

    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }

    location / {
        return 404;
    }
}
EOF

install -m 0644 "${bootstrap}" "${site_available}"
ln -sfn "${site_available}" "${site_enabled}"
nginx -t
systemctl enable --now nginx
systemctl reload nginx

if [[ ! -f "/etc/letsencrypt/live/${DOMAIN}/fullchain.pem" ]]; then
    certbot certonly --webroot -w /var/www/html \
        --non-interactive --agree-tos --register-unsafely-without-email \
        -d "${DOMAIN}"
fi

install -m 0644 "${FINAL_CONFIG}" "${site_available}"
nginx -t
systemctl reload nginx

status="$(
    curl --fail --silent --show-error --noproxy '*' \
        --resolve "${DOMAIN}:443:127.0.0.1" \
        --head --output /dev/null --write-out '%{http_code}' \
        "https://${DOMAIN}/latency"
)"
[[ "${status}" == "204" ]] || {
    printf 'unexpected latency status: %s\n' "${status}" >&2
    exit 1
}

rm -f "${bootstrap}"
trap - EXIT
printf 'LATENCY_INSTALL=ok\n'
printf 'BACKUP_DIR=%s\n' "${backup_dir}"
