#!/usr/bin/env bash

set -Eeuo pipefail

backup_create() {
    local stamp
    local destination
    local local_archive
    local server_archive

    require_remote_config
    require_age_create_config
    ensure_runtime_dirs

    stamp="$(utc_timestamp)-$$-${RANDOM}"
    destination="${BACKUP_DIR}/${stamp}"
    local_archive="${destination}/local.tar.gz.age"
    server_archive="${destination}/server.tar.gz.age"

    mkdir -m 0700 "${destination}"

    if ! tar -C "${ROOT_DIR}" -czf - \
        state \
        secrets \
        config/local.env |
        age -r "${AGE_RECIPIENT}" -o "${local_archive}"; then
        rm -f "${local_archive}" "${server_archive}"
        rmdir "${destination}" 2>/dev/null || true
        die "Failed to create encrypted local backup"
    fi

    ssh_args
    if ! ssh "${SSH_ARGS[@]}" "$(remote_target)" \
        "sudo -n tar -C / -czf - \
            etc/wireguard/wg0.key \
            etc/wireguard/wg0.conf \
            etc/nftables.d/personal-vpn.nft \
            etc/sysctl.d/70-personal-vpn.conf \
            etc/systemd/system/personal-vpn-firewall.service \
            usr/local/sbin/personal-vpn-firewall \
            usr/local/sbin/personal-vpn-rollback" |
        age -r "${AGE_RECIPIENT}" -o "${server_archive}"; then
        rm -f "${local_archive}" "${server_archive}"
        rmdir "${destination}" 2>/dev/null || true
        die "Failed to create encrypted server backup"
    fi

    (
        cd "${destination}"
        sha256sum local.tar.gz.age server.tar.gz.age >SHA256SUMS
    )
    {
        printf 'format=personal-vpn-backup-v1\n'
        printf 'created_at=%s\n' "${stamp}"
        printf 'endpoint=%s\n' "${VPN_ENDPOINT_IPV4}"
        printf 'vpn_port=%s\n' "${VPN_PORT}"
    } >"${destination}/manifest"
    chmod 0600 "${destination}/"*

    printf '%s\n' "${destination}"
}

backup_verify() {
    local archive_dir="$1"

    require_age_identity_config
    [[ -d "${archive_dir}" ]] || die "Backup directory does not exist: ${archive_dir}"
    [[ -f "${archive_dir}/manifest" &&
       -f "${archive_dir}/SHA256SUMS" &&
       -f "${archive_dir}/local.tar.gz.age" &&
       -f "${archive_dir}/server.tar.gz.age" ]] ||
        die "Backup directory is incomplete: ${archive_dir}"
    grep -qx 'format=personal-vpn-backup-v1' "${archive_dir}/manifest" ||
        die "Unsupported backup format"

    (
        cd "${archive_dir}"
        sha256sum -c SHA256SUMS
    )
    age -d -i "${AGE_IDENTITY_FILE}" \
        "${archive_dir}/local.tar.gz.age" |
        tar -tzf - >/dev/null
    age -d -i "${AGE_IDENTITY_FILE}" \
        "${archive_dir}/server.tar.gz.age" |
        tar -tzf - >/dev/null

    log "Backup verified: ${archive_dir}"
}

backup_restore() {
    local archive_dir="$1"
    local confirmation="$2"
    local pre_restore_backup
    local stamp
    local remote_stage
    local output
    local rollback_unit
    local server_public_key

    [[ "${confirmation}" == "--yes" ]] ||
        die "Restore is destructive; pass --yes"
    require_remote_config
    require_age_identity_config
    backup_verify "${archive_dir}"

    pre_restore_backup="$(backup_create)"
    log "Created pre-restore backup: ${pre_restore_backup}"

    stamp="$(utc_timestamp)"
    remote_stage="/tmp/personal-vpn-restore-${stamp,,}-$$"
    remote_exec "umask 077 && mkdir '${remote_stage}'"

    ssh_args
    # shellcheck disable=SC2029 # remote_stage is generated locally from safe characters.
    if ! age -d -i "${AGE_IDENTITY_FILE}" \
        "${archive_dir}/server.tar.gz.age" |
        ssh "${SSH_ARGS[@]}" "$(remote_target)" \
            "tar -xzf - -C '${remote_stage}'"; then
        remote_exec "rm -rf '${remote_stage}'" >/dev/null 2>&1 || true
        die "Failed to stream the server backup to the restore stage"
    fi

    if ! output="$(
        remote_sudo_script \
            "${ROOT_DIR}/scripts/remote/restore.sh" \
            "${remote_stage}" \
            "${VPN_INTERFACE}" \
            "${stamp}"
    )"; then
        warn "Remote restore failed. If firewall application began, wait two minutes for rollback."
        remote_exec "rm -rf '${remote_stage}'" >/dev/null 2>&1 || true
        return 1
    fi
    printf '%s\n' "${output}"

    rollback_unit="$(
        awk -F= '$1 == "ROLLBACK_UNIT" {print $2}' <<<"${output}"
    )"
    server_public_key="$(
        awk -F= '$1 == "SERVER_PUBLIC_KEY" {print $2}' <<<"${output}"
    )"
    assert_wireguard_key "Restored server public key" "${server_public_key}"

    if ! remote_exec "sudo -n wg show '${VPN_INTERFACE}' >/dev/null"; then
        warn "Fresh SSH verification failed; automatic rollback remains armed."
        return 1
    fi
    cancel_remote_rollback "${rollback_unit}"

    age -d -i "${AGE_IDENTITY_FILE}" \
        "${archive_dir}/local.tar.gz.age" |
        tar -xzf - -C "${ROOT_DIR}"
    printf '%s\n' "${server_public_key}" >"${STATE_DIR}/server-public.key"
    chmod 0600 "${STATE_DIR}/server-public.key"

    remote_exec "rm -rf '${remote_stage}'" >/dev/null
    log "Restore completed and rollback timer cancelled"
}
