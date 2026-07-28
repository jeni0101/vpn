#!/usr/bin/env bash

set -Eeuo pipefail

: "${ROOT_DIR:?ROOT_DIR must be set before loading common.sh}"

# shellcheck disable=SC2034 # Used by other files after this library is sourced.
CONFIG_FILE="${VPNCTL_CONFIG:-${ROOT_DIR}/config/local.env}"
STATE_DIR="${ROOT_DIR}/state"
SECRETS_DIR="${ROOT_DIR}/secrets"
BACKUP_DIR="${ROOT_DIR}/backups"
EXPORT_DIR="${ROOT_DIR}/exports"

log() {
    printf '[personal-vpn] %s\n' "$*" >&2
}

warn() {
    printf '[personal-vpn] WARNING: %s\n' "$*" >&2
}

die() {
    printf '[personal-vpn] ERROR: %s\n' "$*" >&2
    exit 1
}

require_command() {
    local command_name
    for command_name in "$@"; do
        command -v "${command_name}" >/dev/null 2>&1 ||
            die "Required command not found: ${command_name}"
    done
}

utc_timestamp() {
    date -u '+%Y%m%dT%H%M%SZ'
}

make_temp_dir() {
    mktemp -d "${TMPDIR:-/tmp}/personal-vpn.XXXXXXXX"
}

ensure_runtime_dirs() {
    umask 077
    mkdir -p \
        "${STATE_DIR}/peers" \
        "${STATE_DIR}/pending" \
        "${SECRETS_DIR}/peers" \
        "${SECRETS_DIR}/pending" \
        "${BACKUP_DIR}" \
        "${EXPORT_DIR}"
    chmod 0700 "${STATE_DIR}" "${SECRETS_DIR}" "${BACKUP_DIR}" "${EXPORT_DIR}"
    chmod 0700 \
        "${STATE_DIR}/peers" \
        "${STATE_DIR}/pending" \
        "${SECRETS_DIR}/peers" \
        "${SECRETS_DIR}/pending"
}

is_wireguard_key() {
    [[ "${1:-}" =~ ^[A-Za-z0-9+/]{43}=$ ]]
}

assert_wireguard_key() {
    local label="$1"
    local value="$2"
    is_wireguard_key "${value}" || die "${label} is not a valid WireGuard key"
}

assert_private_file_mode() {
    local file="$1"
    local label="$2"
    local mode

    [[ -f "${file}" ]] || die "${label} does not exist: ${file}"
    mode="$(stat -c '%a' "${file}")"
    if ((8#${mode} & 077)); then
        die "${label} must not be readable or writable by group/others: ${file}"
    fi
}

is_ipv4_literal() {
    local value="$1"
    local octet
    local -a octets

    IFS='.' read -r -a octets <<<"${value}"
    [[ "${#octets[@]}" -eq 4 ]] || return 1
    for octet in "${octets[@]}"; do
        [[ "${octet}" =~ ^[0-9]{1,3}$ ]] || return 1
        ((10#${octet} <= 255)) || return 1
    done
}

canonical_global_ip() {
    local value="$1"
    local family="$2"

    require_command python3
    python3 - "${value}" "${family}" <<'PY'
import ipaddress
import sys

try:
    address = ipaddress.ip_address(sys.argv[1])
except ValueError:
    raise SystemExit(1)

expected_version = int(sys.argv[2])
if address.version != expected_version or not address.is_global:
    raise SystemExit(1)

print(address.compressed)
PY
}

is_global_ipv4() {
    canonical_global_ip "${1:-}" 4 >/dev/null 2>&1
}

is_global_ipv6() {
    canonical_global_ip "${1:-}" 6 >/dev/null 2>&1
}

is_ipv6_literal() {
    local value="$1"

    require_command python3
    python3 - "${value}" <<'PY'
import ipaddress
import sys

try:
    address = ipaddress.ip_address(sys.argv[1])
except ValueError:
    raise SystemExit(1)

raise SystemExit(0 if address.version == 6 else 1)
PY
}

assert_safe_peer_name() {
    case "${1:-}" in
        windows | macos | ios | android) ;;
        *) die "Peer must be one of: windows, macos, ios, android" ;;
    esac
}

meta_get() {
    local file="$1"
    local key="$2"
    awk -F= -v wanted="${key}" '
        $1 == wanted {
            sub(/^[^=]*=/, "")
            print
            found = 1
            exit
        }
        END {
            if (!found) {
                exit 1
            }
        }
    ' "${file}"
}

output_get() {
    local output="$1"
    local key="$2"

    awk -v wanted="${key}" '
        index($0, wanted "=") == 1 {
            sub(/^[^=]*=/, "")
            print
            found = 1
            exit
        }
        END {
            if (!found) {
                exit 1
            }
        }
    ' <<<"${output}"
}

write_meta() {
    local file="$1"
    local name="$2"
    local ipv4="$3"
    local ipv6="$4"
    local public_key="$5"
    local status="$6"
    local created_at="$7"

    umask 077
    {
        printf 'name=%s\n' "${name}"
        printf 'ipv4=%s\n' "${ipv4}"
        printf 'ipv6=%s\n' "${ipv6}"
        printf 'public_key=%s\n' "${public_key}"
        printf 'status=%s\n' "${status}"
        printf 'created_at=%s\n' "${created_at}"
    } >"${file}"
    chmod 0600 "${file}"
}

secure_remove_peer_dir() {
    local target="$1"
    local expected_parent="$2"

    [[ -n "${target}" && -n "${expected_parent}" ]] ||
        die "Refusing to remove an unresolved path"
    [[ "${target}" == "${expected_parent}/"* ]] ||
        die "Refusing to remove path outside ${expected_parent}"
    [[ "${target}" != "${expected_parent}" ]] ||
        die "Refusing to remove the peer parent directory"

    if [[ -d "${target}" ]]; then
        find "${target}" -maxdepth 1 -type f -delete
        rmdir "${target}"
    fi
}
