#!/usr/bin/env bash

set -Eeuo pipefail

REGION_CODE="${1:?region code is required}"
NODE_ID="${2:?node ID is required}"
MODE="${3:?enabled mode true or false is required}"
SOCKET_PATH="${4:-/run/personal-vpn/manager.sock}"
DATABASE_PATH="${5:-/var/lib/personal-vpn/manager.db}"

[[ "${EUID}" -eq 0 ]] || {
    printf 'must run as root\n' >&2
    exit 1
}
[[ "${REGION_CODE}" =~ ^[A-Z]{2,8}$ ]] || {
    printf 'invalid region code\n' >&2
    exit 1
}
[[ "${NODE_ID}" =~ ^[a-z0-9][a-z0-9_-]{1,63}$ ]] || {
    printf 'invalid node ID\n' >&2
    exit 1
}
[[ "${MODE}" == "true" || "${MODE}" == "false" ]] || {
    printf 'enabled mode must be true or false\n' >&2
    exit 1
}
[[ -S "${SOCKET_PATH}" && -f "${DATABASE_PATH}" ]] || {
    printf 'manager socket or database is unavailable\n' >&2
    exit 1
}
for command in curl jq sqlite3; do
    command -v "${command}" >/dev/null || {
        printf 'missing command: %s\n' "${command}" >&2
        exit 1
    }
done

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="/var/backups/personal-vpn/region-${REGION_CODE}-${timestamp}"
region_original="${backup_dir}/region.json"
node_original="${backup_dir}/node.json"
region_payload="${backup_dir}/region-updated.json"
node_payload="${backup_dir}/node-updated.json"
region_changed=false
node_changed=false

install -d -m 0700 "${backup_dir}"
sqlite3 "${DATABASE_PATH}" ".backup '${backup_dir}/manager.db'"
chmod 0600 "${backup_dir}/manager.db"

api_get() {
    curl --fail --silent --show-error --unix-socket "${SOCKET_PATH}" \
        "http://manager${1}"
}

api_put_file() {
    curl --fail --silent --show-error --unix-socket "${SOCKET_PATH}" \
        --request PUT --header 'Content-Type: application/json' \
        --data-binary "@${2}" "http://manager${1}"
}

api_get '/v2/regions?all=1' |
    jq -ce --arg code "${REGION_CODE}" \
        '.regions[] | select(.code == $code)' >"${region_original}"
api_get "/v2/nodes?all=1&region=${REGION_CODE}" |
    jq -ce --arg id "${NODE_ID}" \
        '.nodes[] | select(.id == $id)' >"${node_original}"

if [[ "${MODE}" == "true" ]]; then
    jq -e '.health == "healthy" and (.server_public_key | length > 0)' \
        "${node_original}" >/dev/null || {
        printf 'node is not healthy or lacks a public key\n' >&2
        exit 1
    }
    last_report="$(jq -r '.last_report_at // empty' "${node_original}")"
    [[ -n "${last_report}" ]] || {
        printf 'node has never reported\n' >&2
        exit 1
    }
    report_epoch="$(date -d "${last_report}" +%s)"
    now_epoch="$(date -u +%s)"
    (( now_epoch - report_epoch <= 300 )) || {
        printf 'node report is stale\n' >&2
        exit 1
    }
fi

jq --argjson enabled "${MODE}" '.enabled = $enabled' \
    "${region_original}" >"${region_payload}"
jq --argjson enabled "${MODE}" '.enabled = $enabled' \
    "${node_original}" >"${node_payload}"

rollback() {
    local exit_code=$?
    trap - EXIT
    if [[ "${exit_code}" -ne 0 ]]; then
        if [[ "${region_changed}" == "true" ]]; then
            api_put_file "/v2/regions/${REGION_CODE}" "${region_original}" || true
        fi
        if [[ "${node_changed}" == "true" ]]; then
            api_put_file "/v2/nodes/${NODE_ID}" "${node_original}" || true
        fi
        printf 'REGION_ENABLE=rolled_back\n' >&2
        printf 'BACKUP_DIR=%s\n' "${backup_dir}" >&2
    fi
    exit "${exit_code}"
}
trap rollback EXIT

if [[ "${MODE}" == "true" ]]; then
    api_put_file "/v2/nodes/${NODE_ID}" "${node_payload}"
    node_changed=true
    api_put_file "/v2/regions/${REGION_CODE}" "${region_payload}"
    region_changed=true
else
    api_put_file "/v2/regions/${REGION_CODE}" "${region_payload}"
    region_changed=true
    api_put_file "/v2/nodes/${NODE_ID}" "${node_payload}"
    node_changed=true
fi

audit_payload="$(
    jq -cn \
        --arg actor 'codex-deploy' \
        --arg action 'region.activation' \
        --arg detail "${REGION_CODE}/${NODE_ID} enabled=${MODE}" \
        '{actor:$actor,action:$action,detail:$detail}'
)"
curl --fail --silent --show-error --unix-socket "${SOCKET_PATH}" \
    --request POST --header 'Content-Type: application/json' \
    --data-binary "${audit_payload}" 'http://manager/v1/audit'

trap - EXIT
printf 'REGION_ENABLE=%s\n' "${MODE}"
printf 'BACKUP_DIR=%s\n' "${backup_dir}"
