#!/usr/bin/env bash

set -Eeuo pipefail

WEB_DOMAIN="vpn.example.com"
WEB_DIST_DIR="${ROOT_DIR}/dist/control-plane"

web_build() (
    local go_bin="${GO:-go}"

    require_command npm "${go_bin}"
    rm -rf "${WEB_DIST_DIR}"
    mkdir -p "${WEB_DIST_DIR}"
    (
        cd "${ROOT_DIR}/web"
        npm ci
        npm run build
    )
    (
        cd "${ROOT_DIR}/server"
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            "${go_bin}" build -trimpath -ldflags='-s -w' \
            -o "${WEB_DIST_DIR}/personal-vpn-managerd" \
            ./cmd/personal-vpn-managerd
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            "${go_bin}" build -trimpath -ldflags='-s -w' \
            -o "${WEB_DIST_DIR}/personal-vpn-managerctl" \
            ./cmd/personal-vpn-managerctl
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            "${go_bin}" build -trimpath -ldflags='-s -w' \
            -o "${WEB_DIST_DIR}/personal-vpn-web" \
            ./cmd/personal-vpn-web
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            "${go_bin}" build -trimpath -ldflags='-s -w' \
            -o "${WEB_DIST_DIR}/personal-vpn-release-tool" \
            ./cmd/personal-vpn-release-tool
    )
    sha256sum "${WEB_DIST_DIR}/"* >"${WEB_DIST_DIR}/SHA256SUMS"
    log "Control-plane binaries built in ${WEB_DIST_DIR}"
)

web_dns_preflight() {
    require_command python3
    python3 - "${WEB_DOMAIN}" "${SERVER_PUBLIC_IPV4}" "${SERVER_PUBLIC_IPV6}" <<'PY'
import ipaddress
import socket
import sys

domain, expected4, expected6 = sys.argv[1:]
try:
    records = socket.getaddrinfo(domain, 443, type=socket.SOCK_STREAM)
except socket.gaierror as exc:
    raise SystemExit(f"{domain} DNS is not ready: {exc}")
v4 = {item[4][0] for item in records if item[0] == socket.AF_INET}
v6 = {ipaddress.ip_address(item[4][0]).compressed for item in records if item[0] == socket.AF_INET6}
if expected4 not in v4:
    raise SystemExit(f"{domain} A must be {expected4}; got {sorted(v4)}")
expected6 = ipaddress.ip_address(expected6).compressed
if expected6 not in v6:
    raise SystemExit(f"{domain} AAAA must be {expected6}; got {sorted(v6)}")
PY
}

web_preflight() {
    require_remote_config
    web_dns_preflight
    remote_exec_script \
        "${ROOT_DIR}/scripts/remote/web-preflight.sh" \
        "${SERVER_PUBLIC_IPV4}" \
        "${SERVER_PUBLIC_IPV6}" \
        "${WEB_DOMAIN}"
}

web_stage_and_install() (
    local apply="$1"
    local stage
    local file
    stage="/tmp/tnest-vpn-web-$(utc_timestamp)-$$"

    [[ -x "${WEB_DIST_DIR}/personal-vpn-managerd" &&
       -x "${WEB_DIST_DIR}/personal-vpn-managerctl" &&
       -x "${WEB_DIST_DIR}/personal-vpn-web" ]] ||
        die "Build control-plane binaries first: scripts/vpnctl web build"

    remote_exec "umask 077 && mkdir '${stage}'"
    for file in personal-vpn-managerd personal-vpn-managerctl personal-vpn-web; do
        copy_to_remote "${WEB_DIST_DIR}/${file}" "${stage}/${file}"
    done
    copy_to_remote "${ROOT_DIR}/config/personal-vpn-managerd.service" \
        "${stage}/personal-vpn-managerd.service"
    copy_to_remote "${ROOT_DIR}/config/personal-vpn-web.service" \
        "${stage}/personal-vpn-web.service"
    copy_to_remote "${ROOT_DIR}/config/nginx-vpn.example.com.conf" \
        "${stage}/nginx.conf"
    copy_to_remote "${ROOT_DIR}/config/nginx-vpn.example.com-bootstrap.conf" \
        "${stage}/nginx-bootstrap.conf"
    if ! remote_sudo_script \
        "${ROOT_DIR}/scripts/remote/install-web.sh" \
        "${stage}" "${apply}" "${WEB_DOMAIN}" \
        "${VPN_ENDPOINT_IPV4}:${VPN_PORT}" "${AGE_RECIPIENT}"; then
        remote_exec "rm -rf '${stage}'" >/dev/null 2>&1 || true
        return 1
    fi
    remote_exec "rm -rf '${stage}'"
)

web_compare_import() {
    local local_keys
    local remote_keys

    local_keys="$(
        python3 "${ROOT_DIR}/scripts/build-manager-import.py" |
            python3 -c 'import json,sys; print("\n".join(sorted(x["public_key"] for x in json.load(sys.stdin)["devices"])))'
    )"
    remote_keys="$(
        remote_exec "sudo -n /usr/local/bin/personal-vpn-managerctl devices" |
            python3 -c 'import json,sys; print("\n".join(sorted(x["public_key"] for x in json.load(sys.stdin)["devices"] if x["status"]=="active")))'
    )"
    [[ "${local_keys}" == "${remote_keys}" ]] ||
        die "Imported manager public keys do not exactly match local active peers"
    log "Imported peers match all existing public keys"
}

web_deploy() {
    require_remote_config
    require_command python3
    web_preflight
    backup_create >/dev/stderr
    web_build

    if remote_exec \
        "systemctl is-active --quiet personal-vpn-managerd && \
         sudo -n grep -qx 'TNEST_APPLY_CHANGES=true' /etc/personal-vpn/manager.env && \
         sudo -n /usr/local/bin/personal-vpn-managerctl devices | \
         python3 -c 'import json,sys; raise SystemExit(0 if json.load(sys.stdin)[\"devices\"] else 1)'" \
        >/dev/null 2>&1; then
        log "Existing manager database detected; preserving it and refreshing binaries"
        web_stage_and_install true
        web_status
        return
    fi

    log "Installing manager in read-only mode"
    web_stage_and_install false
    if ! python3 "${ROOT_DIR}/scripts/build-manager-import.py" |
        remote_exec "sudo -n /usr/local/bin/personal-vpn-managerctl verify-import"; then
        die "Live WireGuard peers do not exactly match migration input"
    fi
    if ! python3 "${ROOT_DIR}/scripts/build-manager-import.py" |
        remote_exec "sudo -n /usr/local/bin/personal-vpn-managerctl import --file -"; then
        die "Existing peer import failed; manager remains read-only"
    fi
    web_compare_import

    log "Enabling manager writes after import verification"
    web_stage_and_install true
    web_status
    warn "Initialize the administrator next: scripts/vpnctl web init-admin"
}

web_status() {
    require_remote_config
    remote_exec \
        "sudo -n systemctl --no-pager --full status personal-vpn-managerd personal-vpn-web; \
         sudo -n /usr/local/bin/personal-vpn-managerctl health; \
         curl --fail --silent --show-error https://${WEB_DOMAIN}/api/v1/health"
}

web_init_admin() {
    local username="${1:-admin}"
    local password
    local password_again

    require_remote_config
    [[ "${username}" =~ ^[A-Za-z0-9_.-]{1,64}$ ]] ||
        die "Administrator username contains unsafe characters"
    read -r -s -p "TNest VPN 管理员密码: " password
    printf '\n' >&2
    read -r -s -p "再次输入密码: " password_again
    printf '\n' >&2
    [[ "${password}" == "${password_again}" ]] ||
        die "Passwords do not match"
    ((${#password} >= 14)) ||
        die "Administrator password must be at least 14 characters"
    ssh_args
    # shellcheck disable=SC2029 # username is restricted to a safe ASCII allowlist above.
    if ! printf '%s\n' "${password}" |
        ssh "${SSH_ARGS[@]}" "$(remote_target)" \
            "sudo -n -u tnest-vpn-web /usr/local/bin/personal-vpn-web init-admin --username '${username}' --password-stdin"; then
        password=""
        password_again=""
        die "Administrator initialization failed"
    fi
    password=""
    password_again=""
    warn "Save the TOTP URI and all recovery codes offline; they are shown only once"
}
