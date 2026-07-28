#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

"${ROOT_DIR}/tests/static.sh"
"${ROOT_DIR}/tests/config-prepare.sh"
"${ROOT_DIR}/tests/backup-flow.sh"
"${ROOT_DIR}/tests/remote-preflight.sh"
"${ROOT_DIR}/tests/remote-postcheck.sh"
"${ROOT_DIR}/tests/peer-flow.sh"

if command -v bats >/dev/null 2>&1; then
    bats "${ROOT_DIR}/tests/unit.bats"
else
    printf 'bats: skipped (not installed)\n' >&2
fi

if [[ "${1:-}" == "--lab" ]]; then
    sudo -n "${ROOT_DIR}/tests/lab.sh"
fi
