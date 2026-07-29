#!/usr/bin/env bash

set -Eeuo pipefail

MODE="${1:?apply mode true or false is required}"
ENV_FILE="${2:-/etc/personal-vpn-node/node.env}"

[[ "${EUID}" -eq 0 ]] || {
    printf 'must run as root\n' >&2
    exit 1
}
[[ "${MODE}" == "true" || "${MODE}" == "false" ]] || {
    printf 'apply mode must be true or false\n' >&2
    exit 1
}
[[ -f "${ENV_FILE}" ]] || {
    printf 'missing node environment file\n' >&2
    exit 1
}
[[ "$(grep -c '^TNEST_NODE_APPLY_CHANGES=' "${ENV_FILE}")" -eq 1 ]] || {
    printf 'node apply setting must occur exactly once\n' >&2
    exit 1
}

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="/var/backups/personal-vpn/node-apply-${timestamp}"
temporary="$(mktemp)"
changed=false

install -d -m 0700 "${backup_dir}"
cp -a "${ENV_FILE}" "${backup_dir}/node.env"

rollback() {
    local exit_code=$?
    trap - EXIT
    rm -f "${temporary}"
    if [[ "${exit_code}" -ne 0 && "${changed}" == "true" ]]; then
        cp -a "${backup_dir}/node.env" "${ENV_FILE}"
        systemctl restart personal-vpn-node-agent || true
        printf 'NODE_APPLY_MODE=rolled_back\n' >&2
        printf 'BACKUP_DIR=%s\n' "${backup_dir}" >&2
    fi
    exit "${exit_code}"
}
trap rollback EXIT

sed "s/^TNEST_NODE_APPLY_CHANGES=.*/TNEST_NODE_APPLY_CHANGES=${MODE}/" \
    "${ENV_FILE}" >"${temporary}"
chown --reference="${ENV_FILE}" "${temporary}"
chmod --reference="${ENV_FILE}" "${temporary}"
mv -f "${temporary}" "${ENV_FILE}"
changed=true

systemctl restart personal-vpn-node-agent
systemctl is-active --quiet personal-vpn-node-agent
grep -q "^TNEST_NODE_APPLY_CHANGES=${MODE}$" "${ENV_FILE}"

trap - EXIT
printf 'NODE_APPLY_MODE=%s\n' "${MODE}"
printf 'BACKUP_DIR=%s\n' "${backup_dir}"
