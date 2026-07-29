#!/usr/bin/env bash

set -Eeuo pipefail

STAGE="${1:?stage directory is required}"
APPLY_CHANGES="${2:-false}"
DOMAIN="${3:-vpn.example.com}"
ENDPOINT="${4:?WireGuard endpoint is required}"
AGE_RECIPIENT="${5:?age recipient is required}"

for file in personal-vpn-managerd personal-vpn-managerctl personal-vpn-web \
    personal-vpn-managerd.service personal-vpn-web.service nginx.conf nginx-bootstrap.conf; do
    [[ -f "${STAGE}/${file}" ]] || { printf 'missing %s\n' "${file}" >&2; exit 1; }
done
[[ "${APPLY_CHANGES}" == "true" || "${APPLY_CHANGES}" == "false" ]] ||
    { printf 'invalid apply mode\n' >&2; exit 1; }
[[ "${DOMAIN}" =~ ^[A-Za-z0-9.-]+$ && "${DOMAIN}" == *.* ]] ||
    { printf 'invalid management domain\n' >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
    nginx certbot python3-certbot-nginx ca-certificates openssl

getent group tnest-vpn-web >/dev/null ||
    groupadd --system tnest-vpn-web
id -u tnest-vpn-web >/dev/null 2>&1 ||
    useradd --system --gid tnest-vpn-web --home-dir /var/lib/personal-vpn-web \
        --shell /usr/sbin/nologin tnest-vpn-web

install -d -m 0750 -o root -g tnest-vpn-web /etc/personal-vpn
install -d -m 0750 -o root -g tnest-vpn-web /etc/personal-vpn-web
install -d -m 0700 -o root -g root /var/lib/personal-vpn
install -d -m 0700 -o tnest-vpn-web -g tnest-vpn-web /var/lib/personal-vpn-web
install -d -m 0700 -o root -g root /var/lib/personal-vpn/prechange

if [[ ! -f /etc/personal-vpn-web/node-ca.key ||
      ! -f /etc/personal-vpn-web/node-ca.crt ]]; then
    umask 077
    openssl genpkey -algorithm ED25519 \
        -out /etc/personal-vpn-web/node-ca.key
    openssl req -new -x509 \
        -key /etc/personal-vpn-web/node-ca.key \
        -out /etc/personal-vpn-web/node-ca.crt \
        -days 3650 -subj '/CN=TNest VPN Node CA'
fi
if [[ ! -f /etc/personal-vpn-web/node-api.token ]]; then
    umask 077
    openssl rand -base64 48 | tr -d '\n' \
        >/etc/personal-vpn-web/node-api.token
fi
chown root:root \
    /etc/personal-vpn-web/node-ca.key \
    /etc/personal-vpn-web/node-ca.crt
chmod 0600 /etc/personal-vpn-web/node-ca.key
chmod 0644 /etc/personal-vpn-web/node-ca.crt
chown root:tnest-vpn-web /etc/personal-vpn-web/node-api.token
chmod 0640 /etc/personal-vpn-web/node-api.token
node_api_token="$(</etc/personal-vpn-web/node-api.token)"
[[ "${#node_api_token}" -ge 32 ]] || {
    printf 'invalid node API token\n' >&2
    exit 1
}

install -m 0755 "${STAGE}/personal-vpn-managerd" /usr/local/bin/
install -m 0755 "${STAGE}/personal-vpn-managerctl" /usr/local/bin/
install -m 0755 "${STAGE}/personal-vpn-web" /usr/local/bin/
install -m 0644 "${STAGE}/personal-vpn-managerd.service" /etc/systemd/system/
install -m 0644 "${STAGE}/personal-vpn-web.service" /etc/systemd/system/

if [[ ! -f /etc/personal-vpn/manager.key ]]; then
    umask 077
    head -c 32 /dev/urandom | base64 -w0 | tr -d '=' >/etc/personal-vpn/manager.key
fi
if [[ ! -f /etc/personal-vpn-web/auth.key ]]; then
    umask 077
    head -c 32 /dev/urandom | base64 -w0 | tr -d '=' >/etc/personal-vpn-web/auth.key
fi
chown root:root /etc/personal-vpn/manager.key
chmod 0600 /etc/personal-vpn/manager.key
chown root:tnest-vpn-web /etc/personal-vpn-web/auth.key
chmod 0640 /etc/personal-vpn-web/auth.key

cat >/etc/personal-vpn/manager.env <<EOF
TNEST_MANAGER_SOCKET=/run/personal-vpn/manager.sock
TNEST_MANAGER_DB=/var/lib/personal-vpn/manager.db
TNEST_MANAGER_KEY=/etc/personal-vpn/manager.key
TNEST_WG_CONFIG=/etc/wireguard/wg0.conf
TNEST_WG_INTERFACE=wg0
TNEST_MANAGEMENT_URL=https://${DOMAIN}
TNEST_WG_ENDPOINT=${ENDPOINT}
TNEST_CATALOG_SIGNING_KEY=/etc/personal-vpn/catalog-signing.key
TNEST_REGION_CODE=SG
TNEST_NODE_ID=sg-sin-01
TNEST_NODE_PROBE_URL=https://${DOMAIN}/latency
TNEST_EXIT_MODE=dual_stack
TNEST_APPLY_CHANGES=${APPLY_CHANGES}
TNEST_AGE_RECIPIENT=${AGE_RECIPIENT}
TNEST_POLL_INTERVAL=30s
EOF
chmod 0600 /etc/personal-vpn/manager.env

cat >/etc/personal-vpn-web/web.env <<EOF
TNEST_WEB_LISTEN=127.0.0.1:8787
TNEST_MANAGER_SOCKET=/run/personal-vpn/manager.sock
TNEST_AUTH_DB=/var/lib/personal-vpn-web/auth.db
TNEST_AUTH_KEY=/etc/personal-vpn-web/auth.key
TNEST_RELEASES_FILE=/var/lib/personal-vpn-web/releases.json
TNEST_NODE_API_TOKEN=${node_api_token}
TNEST_SECURE_COOKIES=true
EOF
node_api_token=""
chown root:tnest-vpn-web /etc/personal-vpn-web/web.env
chmod 0640 /etc/personal-vpn-web/web.env

systemctl daemon-reload
systemctl enable personal-vpn-managerd
systemctl restart personal-vpn-managerd
for _ in {1..20}; do
    /usr/local/bin/personal-vpn-managerctl health >/dev/null 2>&1 && break
    sleep 0.25
done
/usr/local/bin/personal-vpn-managerctl health >/dev/null
systemctl enable personal-vpn-web
systemctl restart personal-vpn-web

rendered_bootstrap="$(mktemp)"
rendered_nginx="$(mktemp)"
trap 'rm -f "${rendered_bootstrap}" "${rendered_nginx}"' EXIT
sed "s/__VPN_WEB_DOMAIN__/${DOMAIN}/g" \
    "${STAGE}/nginx-bootstrap.conf" >"${rendered_bootstrap}"
sed "s/__VPN_WEB_DOMAIN__/${DOMAIN}/g" \
    "${STAGE}/nginx.conf" >"${rendered_nginx}"

install -m 0644 "${rendered_bootstrap}" "/etc/nginx/sites-available/${DOMAIN}"
ln -sfn "/etc/nginx/sites-available/${DOMAIN}" "/etc/nginx/sites-enabled/${DOMAIN}"
nginx -t
systemctl reload nginx

if [[ ! -f "/etc/letsencrypt/live/${DOMAIN}/fullchain.pem" ]]; then
    certbot certonly --webroot -w /var/www/html --non-interactive --agree-tos \
        --register-unsafely-without-email -d "${DOMAIN}"
fi
install -m 0644 "${rendered_nginx}" "/etc/nginx/sites-available/${DOMAIN}"
nginx -t
systemctl reload nginx
curl --fail --silent --show-error --noproxy '*' \
    --resolve "${DOMAIN}:443:127.0.0.1" \
    "https://${DOMAIN}/api/v1/health" >/dev/null

printf 'WEB_INSTALL=ok\n'
printf 'APPLY_CHANGES=%s\n' "${APPLY_CHANGES}"
