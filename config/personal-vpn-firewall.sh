#!/usr/bin/env bash

set -Eeuo pipefail

RULES_FILE="/etc/nftables.d/personal-vpn.nft"

remove_tables() {
    local family
    local table_name
    while read -r family table_name; do
        if nft list table "${family}" "${table_name}" >/dev/null 2>&1; then
            nft delete table "${family}" "${table_name}"
        fi
    done <<'EOF'
inet personal_vpn_filter
ip personal_vpn_nat4
ip6 personal_vpn_nat6
EOF
}

case "${1:-}" in
    apply)
        [[ -r "${RULES_FILE}" ]] || {
            printf 'Missing rules file: %s\n' "${RULES_FILE}" >&2
            exit 1
        }
        remove_tables
        nft -f "${RULES_FILE}"
        ;;
    remove)
        remove_tables
        ;;
    *)
        printf 'Usage: %s <apply|remove>\n' "$0" >&2
        exit 2
        ;;
esac
