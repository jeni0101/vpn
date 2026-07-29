#!/usr/bin/env bash

set -Eeuo pipefail

generate_peer_material() {
    local name="$1"
    local secret_dir="$2"
    local meta_file="$3"
    local status="$4"
    local private_key
    local public_key
    local ipv4
    local ipv6

    require_command wg
    assert_safe_peer_name "${name}"
    ensure_runtime_dirs
    [[ ! -e "${secret_dir}" ]] ||
        die "Peer secret directory already exists: ${secret_dir}"
    [[ ! -e "${meta_file}" ]] ||
        die "Peer metadata already exists: ${meta_file}"

    mkdir -m 0700 "${secret_dir}"
    umask 077
    wg genkey >"${secret_dir}/private.key"
    wg pubkey <"${secret_dir}/private.key" >"${secret_dir}/public.key"
    wg genpsk >"${secret_dir}/psk"
    chmod 0600 "${secret_dir}/"*

    private_key="$(<"${secret_dir}/private.key")"
    public_key="$(<"${secret_dir}/public.key")"
    assert_wireguard_key "Generated private key" "${private_key}"
    assert_wireguard_key "Generated public key" "${public_key}"
    assert_wireguard_key "Generated PreSharedKey" "$(<"${secret_dir}/psk")"

    ipv4="$(peer_ipv4 "${name}")"
    ipv6="$(peer_ipv6 "${name}")"
    write_meta \
        "${meta_file}" \
        "${name}" \
        "${ipv4}" \
        "${ipv6}" \
        "${public_key}" \
        "${status}" \
        "$(utc_timestamp)"
    render_client_config "${name}" "${secret_dir}" "${secret_dir}/client.conf"
}

assert_unique_peer_material() {
    local candidate_meta="$1"
    local replacement_name="${2:-}"
    local candidate_name
    local candidate_ipv4
    local candidate_ipv6
    local candidate_public_key
    local meta_file
    local value

    candidate_name="$(meta_get "${candidate_meta}" name)"
    candidate_ipv4="$(meta_get "${candidate_meta}" ipv4)"
    candidate_ipv6="$(meta_get "${candidate_meta}" ipv6)"
    candidate_public_key="$(meta_get "${candidate_meta}" public_key)"

    while IFS= read -r meta_file; do
        [[ -n "${meta_file}" && "${meta_file}" != "${candidate_meta}" ]] || continue
        value="$(meta_get "${meta_file}" name)"
        if [[ -n "${replacement_name}" && "${value}" == "${replacement_name}" ]]; then
            continue
        fi
        [[ "${value}" != "${candidate_name}" ]] ||
            die "Duplicate peer name: ${candidate_name}"
        value="$(meta_get "${meta_file}" ipv4)"
        [[ "${value}" != "${candidate_ipv4}" ]] ||
            die "Duplicate peer IPv4 address: ${candidate_ipv4}"
        value="$(meta_get "${meta_file}" ipv6)"
        [[ "${value}" != "${candidate_ipv6}" ]] ||
            die "Duplicate peer IPv6 address: ${candidate_ipv6}"
        value="$(meta_get "${meta_file}" public_key)"
        [[ "${value}" != "${candidate_public_key}" ]] ||
            die "Duplicate peer public key"
    done < <(
        find "${STATE_DIR}/peers" "${STATE_DIR}/pending" \
            -maxdepth 1 -type f -name '*.meta' -print | sort
    )
}

peer_add() {
    local name="$1"
    local pending_secret
    local pending_meta
    local active_secret
    local active_meta
    local temp_dir
    local backup_path
    local resume_pending="no"

    load_config
    web_assert_legacy_mutation_allowed
    assert_safe_peer_name "${name}"
    ensure_runtime_dirs
    [[ -f "${STATE_DIR}/server-public.key" ]] ||
        die "Deploy the server before adding peers"

    pending_secret="${SECRETS_DIR}/pending/${name}"
    pending_meta="${STATE_DIR}/pending/${name}.meta"
    active_secret="${SECRETS_DIR}/peers/${name}"
    active_meta="${STATE_DIR}/peers/${name}.meta"
    [[ ! -e "${active_secret}" && ! -e "${active_meta}" ]] ||
        die "Peer is already active: ${name}"

    if [[ -e "${pending_secret}" || -e "${pending_meta}" ]]; then
        if peer_pending_material_complete "${name}" &&
            [[ "$(meta_get "${pending_meta}" status)" == "pending-add" ]]; then
            resume_pending="yes"
            log "Resuming complete pending peer add: ${name}"
        else
            die "Incomplete or non-add pending material exists for ${name}"
        fi
    fi

    backup_path="$(backup_create)"
    log "Pre-change backup: ${backup_path}"

    if [[ "${resume_pending}" == "no" ]]; then
        generate_peer_material \
            "${name}" "${pending_secret}" "${pending_meta}" "pending-add"
    fi
    assert_unique_peer_material "${pending_meta}"

    temp_dir="$(make_temp_dir)"
    if ! render_server_config \
        "${temp_dir}/wg0.conf" \
        "${name}" \
        "${pending_meta}" \
        "${pending_secret}" ||
        ! sync_server_wireguard "${temp_dir}/wg0.conf"; then
        rm -f "${temp_dir}/wg0.conf"
        rmdir "${temp_dir}" 2>/dev/null || true
        warn "Peer remains pending locally and is not marked active: ${name}"
        return 1
    fi
    rm -f "${temp_dir}/wg0.conf"
    rmdir "${temp_dir}" 2>/dev/null || true

    mv "${pending_secret}" "${active_secret}"
    mv "${pending_meta}" "${active_meta}"
    write_meta \
        "${active_meta}" \
        "${name}" \
        "$(peer_ipv4 "${name}")" \
        "$(peer_ipv6 "${name}")" \
        "$(<"${active_secret}/public.key")" \
        "active" \
        "$(utc_timestamp)"
    log "Peer is active: ${name}"
    log "Export with: scripts/vpnctl peer export ${name} --file"
}

peer_export() {
    local name="$1"
    local mode="$2"
    local source_config
    local pending_meta
    local pending_status=""
    local destination

    load_config
    assert_safe_peer_name "${name}"
    source_config="${SECRETS_DIR}/peers/${name}/client.conf"
    pending_meta="${STATE_DIR}/pending/${name}.meta"
    if [[ -f "${pending_meta}" ]]; then
        pending_status="$(meta_get "${pending_meta}" status)"
    fi
    if [[ "${pending_status}" == "pending-rotation" &&
          -f "${SECRETS_DIR}/pending/${name}/client.conf" ]]; then
        source_config="${SECRETS_DIR}/pending/${name}/client.conf"
        warn "Exporting the prepared rotation config for ${name}"
    fi
    [[ -f "${source_config}" ]] || die "Active peer config not found: ${name}"

    case "${mode}" in
        --file)
            ensure_runtime_dirs
            destination="${EXPORT_DIR}/${name}.conf"
            install -m 0600 "${source_config}" "${destination}"
            printf '%s\n' "${destination}"
            warn "The exported file contains a private key and PSK"
            ;;
        --qr)
            require_command qrencode
            warn "The QR code contains a private key and PSK; close the terminal after import"
            qrencode -t ansiutf8 <"${source_config}"
            ;;
        *)
            die "Use --file or --qr"
            ;;
    esac
}

peer_list() {
    local meta_file
    local name
    local public_key
    local handshake_data=""
    local last_handshake="-"

    load_config
    ensure_runtime_dirs
    if [[ -f "${STATE_DIR}/server-public.key" ]]; then
        handshake_data="$(
            remote_exec "sudo -n wg show '${VPN_INTERFACE}' latest-handshakes" \
                2>/dev/null || true
        )"
    fi

    printf '%-9s %-15s %-22s %-10s %s\n' \
        "NAME" "IPV4" "IPV6" "STATUS" "LAST_HANDSHAKE"
    while IFS= read -r meta_file; do
        [[ -n "${meta_file}" ]] || continue
        name="$(meta_get "${meta_file}" name)"
        public_key="$(meta_get "${meta_file}" public_key)"
        last_handshake="$(
            awk -v key="${public_key}" '$1 == key {print $2}' <<<"${handshake_data}"
        )"
        [[ -n "${last_handshake}" && "${last_handshake}" != "0" ]] ||
            last_handshake="-"
        printf '%-9s %-15s %-22s %-10s %s\n' \
            "${name}" \
            "$(meta_get "${meta_file}" ipv4)" \
            "$(meta_get "${meta_file}" ipv6)" \
            "$(meta_get "${meta_file}" status)" \
            "${last_handshake}"
    done < <(find "${STATE_DIR}/peers" -maxdepth 1 -type f -name '*.meta' -print | sort)
}

peer_revoke() {
    local name="$1"
    local confirmation="$2"
    local active_meta
    local active_secret
    local temp_dir
    local backup_path

    [[ "${confirmation}" == "--yes" ]] ||
        die "Revocation is destructive; pass --yes"
    load_config
    web_assert_legacy_mutation_allowed
    assert_safe_peer_name "${name}"
    active_meta="${STATE_DIR}/peers/${name}.meta"
    active_secret="${SECRETS_DIR}/peers/${name}"
    [[ -f "${active_meta}" && -d "${active_secret}" ]] ||
        die "Peer is not active: ${name}"

    backup_path="$(backup_create)"
    log "Pre-change backup: ${backup_path}"
    temp_dir="$(make_temp_dir)"
    render_server_config "${temp_dir}/wg0.conf" "${name}"
    if ! sync_server_wireguard "${temp_dir}/wg0.conf"; then
        rm -f "${temp_dir}/wg0.conf"
        rmdir "${temp_dir}" 2>/dev/null || true
        die "Server rejected revocation; local peer material was preserved"
    fi
    rm -f "${temp_dir}/wg0.conf"
    rmdir "${temp_dir}" 2>/dev/null || true

    rm -f "${active_meta}"
    secure_remove_peer_dir "${active_secret}" "${SECRETS_DIR}/peers"
    rm -f "${EXPORT_DIR}/${name}.conf"
    log "Peer revoked: ${name}"
}

peer_rotate_prepare() {
    local name="$1"
    local active_meta
    local pending_meta
    local pending_secret

    load_config
    web_assert_legacy_mutation_allowed
    assert_safe_peer_name "${name}"
    active_meta="${STATE_DIR}/peers/${name}.meta"
    pending_meta="${STATE_DIR}/pending/${name}.meta"
    pending_secret="${SECRETS_DIR}/pending/${name}"
    [[ -f "${active_meta}" ]] || die "Peer is not active: ${name}"
    [[ ! -e "${pending_meta}" && ! -e "${pending_secret}" ]] ||
        die "A pending rotation already exists: ${name}"

    generate_peer_material \
        "${name}" "${pending_secret}" "${pending_meta}" "pending-rotation"
    assert_unique_peer_material "${pending_meta}" "${name}"
    log "Prepared rotation for ${name}"
    log "Review/import ${pending_secret}/client.conf before activation"
}

peer_rotate_activate() {
    local name="$1"
    local confirmation="$2"
    local active_meta
    local active_secret
    local pending_meta
    local pending_secret
    local old_meta
    local old_secret
    local temp_dir
    local backup_path

    [[ "${confirmation}" == "--yes" ]] ||
        die "Rotation activation invalidates the old config; pass --yes"
    load_config
    web_assert_legacy_mutation_allowed
    assert_safe_peer_name "${name}"
    active_meta="${STATE_DIR}/peers/${name}.meta"
    active_secret="${SECRETS_DIR}/peers/${name}"
    pending_meta="${STATE_DIR}/pending/${name}.meta"
    pending_secret="${SECRETS_DIR}/pending/${name}"
    [[ -f "${active_meta}" && -d "${active_secret}" ]] ||
        die "Peer is not active: ${name}"
    [[ -f "${pending_meta}" && -d "${pending_secret}" ]] ||
        die "No prepared rotation exists: ${name}"

    backup_path="$(backup_create)"
    log "Pre-change backup: ${backup_path}"
    temp_dir="$(make_temp_dir)"
    render_server_config \
        "${temp_dir}/wg0.conf" \
        "${name}" \
        "${pending_meta}" \
        "${pending_secret}"
    if ! sync_server_wireguard "${temp_dir}/wg0.conf"; then
        rm -f "${temp_dir}/wg0.conf"
        rmdir "${temp_dir}" 2>/dev/null || true
        die "Server rejected rotation; old peer remains active"
    fi
    rm -f "${temp_dir}/wg0.conf"
    rmdir "${temp_dir}" 2>/dev/null || true

    old_meta="${STATE_DIR}/peers/.${name}.old.meta"
    old_secret="${SECRETS_DIR}/peers/.${name}.old"
    mv "${active_meta}" "${old_meta}"
    mv "${active_secret}" "${old_secret}"
    mv "${pending_meta}" "${active_meta}"
    mv "${pending_secret}" "${active_secret}"
    write_meta \
        "${active_meta}" \
        "${name}" \
        "$(peer_ipv4 "${name}")" \
        "$(peer_ipv6 "${name}")" \
        "$(<"${active_secret}/public.key")" \
        "active" \
        "$(utc_timestamp)"
    rm -f "${old_meta}"
    secure_remove_peer_dir "${old_secret}" "${SECRETS_DIR}/peers"
    rm -f "${EXPORT_DIR}/${name}.conf"
    log "Rotation activated: ${name}"
}

peer_active_material_complete() {
    local name="$1"
    local secret_dir="${SECRETS_DIR}/peers/${name}"
    local meta_file="${STATE_DIR}/peers/${name}.meta"

    [[ -f "${meta_file}" &&
       -f "${secret_dir}/private.key" &&
       -f "${secret_dir}/public.key" &&
       -f "${secret_dir}/psk" &&
       -f "${secret_dir}/client.conf" ]]
}

peer_pending_material_complete() {
    local name="$1"
    local secret_dir="${SECRETS_DIR}/pending/${name}"
    local meta_file="${STATE_DIR}/pending/${name}.meta"

    [[ -f "${meta_file}" &&
       -f "${secret_dir}/private.key" &&
       -f "${secret_dir}/public.key" &&
       -f "${secret_dir}/psk" &&
       -f "${secret_dir}/client.conf" ]]
}

peer_assert_bootstrap_ready() {
    local name
    local active_meta
    local active_secret
    local pending_meta
    local pending_secret

    load_config
    ensure_runtime_dirs
    for name in windows ios macos android; do
        active_meta="${STATE_DIR}/peers/${name}.meta"
        active_secret="${SECRETS_DIR}/peers/${name}"
        pending_meta="${STATE_DIR}/pending/${name}.meta"
        pending_secret="${SECRETS_DIR}/pending/${name}"

        if peer_active_material_complete "${name}" &&
            [[ ! -e "${pending_meta}" && ! -e "${pending_secret}" ]]; then
            continue
        fi
        if [[ ! -e "${active_meta}" && ! -e "${active_secret}" ]] &&
            peer_pending_material_complete "${name}" &&
            [[ "$(meta_get "${pending_meta}" status)" == "pending-add" ]]; then
            continue
        fi
        if [[ ! -e "${active_meta}" &&
              ! -e "${active_secret}" &&
              ! -e "${pending_meta}" &&
              ! -e "${pending_secret}" ]]; then
            continue
        fi
        die "Incomplete or non-resumable peer state blocks bootstrap: ${name}"
    done
}

peer_bootstrap_all() {
    local name

    peer_assert_bootstrap_ready
    for name in windows ios macos android; do
        if peer_active_material_complete "${name}"; then
            log "Peer is already active; skipping creation: ${name}"
        else
            peer_add "${name}"
        fi
        peer_export "${name}" --file >/dev/null
        log "Exported ${EXPORT_DIR}/${name}.conf"
    done
}
