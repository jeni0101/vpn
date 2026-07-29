package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jeni0101/vpn/server/internal/model"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
var ErrExpired = errors.New("expired")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			name_norm TEXT NOT NULL UNIQUE,
			platform TEXT NOT NULL,
			slot INTEGER NOT NULL UNIQUE,
			ipv4 TEXT NOT NULL UNIQUE,
			ipv6 TEXT NOT NULL UNIQUE,
			public_key TEXT NOT NULL DEFAULT '',
			psk_sealed BLOB,
			status TEXT NOT NULL,
			external_private INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			revoked_at TEXT,
			quarantine_until TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS devices_public_key_active
		 ON devices(public_key) WHERE public_key != '' AND status != 'revoked'`,
		`CREATE TABLE IF NOT EXISTS enrollments (
			id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			token_hash BLOB NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			used_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS rotations (
			id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			token_hash BLOB NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			used_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS usage_state (
			device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
			last_rx INTEGER NOT NULL DEFAULT 0,
			last_tx INTEGER NOT NULL DEFAULT 0,
			total_upload INTEGER NOT NULL DEFAULT 0,
			total_download INTEGER NOT NULL DEFAULT 0,
			last_handshake TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS usage_hourly (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			bucket TEXT NOT NULL,
			upload INTEGER NOT NULL DEFAULT 0,
			download INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(device_id, bucket)
		)`,
		`CREATE TABLE IF NOT EXISTS usage_daily (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			bucket TEXT NOT NULL,
			upload INTEGER NOT NULL DEFAULT 0,
			download INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(device_id, bucket)
		)`,
		`CREATE TABLE IF NOT EXISTS usage_monthly (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			bucket TEXT NOT NULL,
			upload INTEGER NOT NULL DEFAULT 0,
			download INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(device_id, bucket)
		)`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			at TEXT NOT NULL,
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			device_id TEXT,
			remote_ip TEXT,
			detail TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS regions (
			code TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			exit_mode TEXT NOT NULL,
			ipv4_network TEXT NOT NULL,
			ipv6_network TEXT NOT NULL,
			dns_json TEXT NOT NULL DEFAULT '[]',
			mtu INTEGER NOT NULL DEFAULT 1420,
			enabled INTEGER NOT NULL DEFAULT 0,
			config_version INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			region_code TEXT NOT NULL REFERENCES regions(code) ON DELETE RESTRICT,
			endpoint TEXT NOT NULL,
			probe_url TEXT NOT NULL,
			server_public_key TEXT NOT NULL DEFAULT '',
			priority INTEGER NOT NULL DEFAULT 100,
			enabled INTEGER NOT NULL DEFAULT 0,
			health TEXT NOT NULL DEFAULT 'unknown',
			version TEXT NOT NULL DEFAULT '',
			last_report_at TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS nodes_region_priority
		 ON nodes(region_code, enabled, priority, id)`,
		`CREATE TABLE IF NOT EXISTS device_region_credentials (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			region_code TEXT NOT NULL REFERENCES regions(code) ON DELETE RESTRICT,
			ipv4 TEXT NOT NULL,
			ipv6 TEXT NOT NULL,
			public_key TEXT NOT NULL,
			psk_sealed BLOB,
			private_key_sealed BLOB,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(device_id, region_code)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS device_region_ipv4_active
		 ON device_region_credentials(region_code, ipv4)
		 WHERE status != 'revoked'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS device_region_ipv6_active
		 ON device_region_credentials(region_code, ipv6)
		 WHERE status != 'revoked'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS device_region_public_key_active
		 ON device_region_credentials(region_code, public_key)
		 WHERE public_key != '' AND status != 'revoked'`,
		`CREATE TABLE IF NOT EXISTS device_tokens (
			token_hash BLOB PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS node_reports (
			node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
			version TEXT NOT NULL,
			healthy INTEGER NOT NULL,
			peer_count INTEGER NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			usage_sequence INTEGER NOT NULL DEFAULT 0,
			reported_at TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO regions(
			code,display_name,sort_order,exit_mode,ipv4_network,ipv6_network,
			dns_json,mtu,enabled,config_version,updated_at
		 ) VALUES(
			'SG','Singapore',10,'dual_stack','10.66.0.0/24','fd66:66:66::/64',
			'["1.1.1.1","2606:4700:4700::1111"]',1420,1,1,
			strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 )`,
		`INSERT OR IGNORE INTO regions(
			code,display_name,sort_order,exit_mode,ipv4_network,ipv6_network,
			dns_json,mtu,enabled,config_version,updated_at
		 ) VALUES(
			'MY','Malaysia (Kuala Lumpur)',20,'ipv4_exit_ipv6_blocked',
			'10.67.0.0/24','fd67:67:67::/64','["1.1.1.1","1.0.0.1"]',1420,0,1,
			strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 )`,
		`INSERT OR IGNORE INTO nodes(
			id,region_code,endpoint,probe_url,server_public_key,priority,enabled,
			health,version,updated_at
		 ) VALUES(
			'sg-sin-01','SG','','https://vpn.tnestai.asia/latency','',10,1,
			'unknown','',strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 )`,
		`INSERT OR IGNORE INTO nodes(
			id,region_code,endpoint,probe_url,server_public_key,priority,enabled,
			health,version,updated_at
		 ) VALUES(
			'my-kul-01','MY','47.250.164.136:53147',
			'https://my-kul-01.vpn.tnestai.asia/latency','',10,0,
			'unknown','',strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 )`,
		`INSERT OR IGNORE INTO device_region_credentials(
			device_id,region_code,ipv4,ipv6,public_key,psk_sealed,status,created_at,updated_at
		 )
		 SELECT id,'SG',ipv4,ipv6,public_key,psk_sealed,status,created_at,
		        strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 FROM devices`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("database migration: %w", err)
		}
	}
	return nil
}

func (s *Store) ListDevices(ctx context.Context) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id,d.name,d.platform,d.ipv4,d.ipv6,d.public_key,d.status,
		       d.external_private,d.created_at,d.revoked_at,d.quarantine_until,
		       u.last_handshake,u.updated_at,
		       COALESCE(u.total_upload,0),COALESCE(u.total_download,0)
		FROM devices d LEFT JOIN usage_state u ON u.device_id=d.id
		ORDER BY d.slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, device)
	}
	return result, rows.Err()
}

func (s *Store) Device(ctx context.Context, id string) (model.Device, []byte, int, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT d.id,d.name,d.platform,d.ipv4,d.ipv6,d.public_key,d.status,
		       d.external_private,d.created_at,d.revoked_at,d.quarantine_until,
		       u.last_handshake,u.updated_at,
		       COALESCE(u.total_upload,0),COALESCE(u.total_download,0),
		       d.psk_sealed,d.slot
		FROM devices d LEFT JOIN usage_state u ON u.device_id=d.id WHERE d.id=?`, id)
	var device model.Device
	var created, revoked, quarantine, handshake, statsUpdated sql.NullString
	var external int
	var sealed []byte
	var slot int
	err := row.Scan(&device.ID, &device.Name, &device.Platform, &device.IPv4, &device.IPv6,
		&device.PublicKey, &device.Status, &external, &created, &revoked, &quarantine,
		&handshake, &statsUpdated, &device.UploadBytes, &device.DownloadBytes,
		&sealed, &slot)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, nil, 0, ErrNotFound
	}
	if err != nil {
		return model.Device{}, nil, 0, err
	}
	device.ExternalPrivate = external != 0
	parseTimes(&device, created, revoked, quarantine, handshake, statsUpdated)
	return device, sealed, slot, nil
}

func scanDevice(scanner interface{ Scan(...any) error }) (model.Device, error) {
	var device model.Device
	var created, revoked, quarantine, handshake, statsUpdated sql.NullString
	var external int
	err := scanner.Scan(&device.ID, &device.Name, &device.Platform, &device.IPv4,
		&device.IPv6, &device.PublicKey, &device.Status, &external, &created,
		&revoked, &quarantine, &handshake, &statsUpdated,
		&device.UploadBytes, &device.DownloadBytes)
	if err != nil {
		return model.Device{}, err
	}
	device.ExternalPrivate = external != 0
	parseTimes(&device, created, revoked, quarantine, handshake, statsUpdated)
	return device, nil
}

func parseTimes(
	device *model.Device,
	created, revoked, quarantine, handshake, statsUpdated sql.NullString,
) {
	device.CreatedAt, _ = time.Parse(time.RFC3339Nano, created.String)
	if revoked.Valid {
		value, _ := time.Parse(time.RFC3339Nano, revoked.String)
		device.RevokedAt = &value
	}
	if quarantine.Valid {
		value, _ := time.Parse(time.RFC3339Nano, quarantine.String)
		device.QuarantineUntil = &value
	}
	if handshake.Valid && handshake.String != "" {
		value, _ := time.Parse(time.RFC3339Nano, handshake.String)
		device.LastHandshake = &value
	}
	if statsUpdated.Valid && statsUpdated.String != "" {
		value, _ := time.Parse(time.RFC3339Nano, statsUpdated.String)
		device.StatsUpdatedAt = &value
	}
}

func (s *Store) NextSlot(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT slot,status,quarantine_until FROM devices WHERE slot BETWEEN 14 AND 254`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	used := make(map[int]bool)
	for rows.Next() {
		var slot int
		var status string
		var quarantine sql.NullString
		if err := rows.Scan(&slot, &status, &quarantine); err != nil {
			return 0, err
		}
		if status != model.StatusRevoked {
			used[slot] = true
			continue
		}
		if quarantine.Valid {
			until, _ := time.Parse(time.RFC3339Nano, quarantine.String)
			if until.After(now) {
				used[slot] = true
			}
		}
	}
	for slot := 14; slot <= 254; slot++ {
		if !used[slot] {
			return slot, nil
		}
	}
	return 0, errors.New("address pool is exhausted")
}

func (s *Store) CreateDevice(ctx context.Context, device model.Device, slot int, sealedPSK []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO devices(id,name,name_norm,platform,slot,ipv4,ipv6,public_key,
		                    psk_sealed,status,external_private,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		device.ID, device.Name, strings.ToLower(device.Name), device.Platform, slot,
		device.IPv4, device.IPv6, device.PublicKey, sealedPSK, device.Status,
		boolInt(device.ExternalPrivate), device.CreatedAt.UTC().Format(time.RFC3339Nano))
	if isConstraint(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) DeletePendingDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id=? AND status=?`,
		id, model.StatusPending)
	return err
}

func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id=?`, id)
	return err
}

func (s *Store) ImportDevice(ctx context.Context, device model.Device, slot int, sealedPSK []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO devices(id,name,name_norm,platform,slot,ipv4,ipv6,public_key,
		                    psk_sealed,status,external_private,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(name_norm) DO UPDATE SET
		  platform=excluded.platform,slot=excluded.slot,ipv4=excluded.ipv4,
		  ipv6=excluded.ipv6,public_key=excluded.public_key,
		  psk_sealed=excluded.psk_sealed,status=excluded.status,
		  external_private=excluded.external_private`,
		device.ID, device.Name, strings.ToLower(device.Name), device.Platform, slot,
		device.IPv4, device.IPv6, device.PublicKey, sealedPSK, device.Status,
		boolInt(device.ExternalPrivate), device.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) CreateEnrollment(ctx context.Context, enrollment model.Enrollment) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO enrollments(id,device_id,token_hash,expires_at)
		VALUES(?,?,?,?)`, enrollment.ID, enrollment.DeviceID, enrollment.TokenHash,
		enrollment.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if isConstraint(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) CreateRotation(ctx context.Context, rotation model.Enrollment) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rotations(id,device_id,token_hash,expires_at)
		VALUES(?,?,?,?)`, rotation.ID, rotation.DeviceID, rotation.TokenHash,
		rotation.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if isConstraint(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) RotationDevice(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
) (model.Device, error) {
	var deviceID, expires string
	var used sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT device_id,expires_at,used_at FROM rotations WHERE token_hash=?`,
		tokenHash).Scan(&deviceID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, ErrNotFound
	}
	if err != nil {
		return model.Device{}, err
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return model.Device{}, err
	}
	if used.Valid || !expiry.After(now) {
		return model.Device{}, ErrExpired
	}
	device, _, _, err := s.Device(ctx, deviceID)
	return device, err
}

func (s *Store) FinalizeRotation(
	ctx context.Context,
	tokenHash []byte,
	publicKey string,
	sealedPSK []byte,
	now time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rotationID, deviceID, expires string
	var used sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id,device_id,expires_at,used_at FROM rotations WHERE token_hash=?`,
		tokenHash).Scan(&rotationID, &deviceID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return err
	}
	if used.Valid || !expiry.After(now) {
		return ErrExpired
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE devices SET public_key=?,psk_sealed=? WHERE id=? AND status=?`,
		publicKey, sealedPSK, deviceID, model.StatusActive); err != nil {
		if isConstraint(err) {
			return ErrConflict
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE rotations SET used_at=? WHERE id=? AND used_at IS NULL`,
		now.UTC().Format(time.RFC3339Nano), rotationID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

func (s *Store) ClaimEnrollment(
	ctx context.Context,
	tokenHash []byte,
	publicKey string,
	sealedPSK []byte,
	now time.Time,
) (model.Device, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Device{}, err
	}
	defer tx.Rollback()
	var enrollmentID, deviceID, expires string
	var used sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id,device_id,expires_at,used_at FROM enrollments WHERE token_hash=?`,
		tokenHash).Scan(&enrollmentID, &deviceID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, ErrNotFound
	}
	if err != nil {
		return model.Device{}, err
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return model.Device{}, err
	}
	if used.Valid || !expiry.After(now) {
		return model.Device{}, ErrExpired
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE enrollments SET used_at=? WHERE id=? AND used_at IS NULL`,
		now.UTC().Format(time.RFC3339Nano), enrollmentID)
	if err != nil {
		return model.Device{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return model.Device{}, ErrConflict
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE devices SET public_key=?,psk_sealed=?,status=? WHERE id=? AND status=?`,
		publicKey, sealedPSK, model.StatusActive, deviceID, model.StatusPending)
	if isConstraint(err) {
		return model.Device{}, ErrConflict
	}
	if err != nil {
		return model.Device{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Device{}, err
	}
	device, _, _, err := s.Device(ctx, deviceID)
	return device, err
}

func (s *Store) UndoClaim(ctx context.Context, deviceID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE devices SET public_key='',psk_sealed=NULL,status=? WHERE id=?`,
		model.StatusPending, deviceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE enrollments SET used_at=NULL WHERE device_id=?`, deviceID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ActivateDevice(ctx context.Context, id, publicKey string, sealedPSK []byte) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE devices SET public_key=?,psk_sealed=?,status=?
		WHERE id=? AND status=?`, publicKey, sealedPSK, model.StatusActive, id, model.StatusPending)
	if isConstraint(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeDevice(ctx context.Context, id string, now, quarantine time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE devices SET status=?,revoked_at=?,quarantine_until=?
		WHERE id=? AND status!=?`, model.StatusRevoked,
		now.UTC().Format(time.RFC3339Nano), quarantine.UTC().Format(time.RFC3339Nano),
		id, model.StatusRevoked)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecordStats(
	ctx context.Context,
	deviceID string,
	rx, tx int64,
	handshake time.Time,
	now time.Time,
) error {
	txDB, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer txDB.Rollback()
	var lastRX, lastTX int64
	var exists bool
	err = txDB.QueryRowContext(ctx, `SELECT last_rx,last_tx FROM usage_state WHERE device_id=?`,
		deviceID).Scan(&lastRX, &lastTX)
	if err == nil {
		exists = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	uploadDelta, downloadDelta := int64(0), int64(0)
	if exists {
		if rx >= lastRX {
			uploadDelta = rx - lastRX
		} else {
			uploadDelta = rx
		}
		if tx >= lastTX {
			downloadDelta = tx - lastTX
		} else {
			downloadDelta = tx
		}
	}
	handshakeValue := any(nil)
	if !handshake.IsZero() {
		handshakeValue = handshake.UTC().Format(time.RFC3339Nano)
	}
	_, err = txDB.ExecContext(ctx, `
		INSERT INTO usage_state(device_id,last_rx,last_tx,total_upload,total_download,last_handshake,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(device_id) DO UPDATE SET
		  last_rx=excluded.last_rx,last_tx=excluded.last_tx,
		  total_upload=usage_state.total_upload+?,
		  total_download=usage_state.total_download+?,
		  last_handshake=COALESCE(excluded.last_handshake,usage_state.last_handshake),
		  updated_at=excluded.updated_at`,
		deviceID, rx, tx, uploadDelta, downloadDelta, handshakeValue,
		now.UTC().Format(time.RFC3339Nano), uploadDelta, downloadDelta)
	if err != nil {
		return err
	}
	if exists && (uploadDelta > 0 || downloadDelta > 0) {
		for _, bucket := range []struct {
			table string
			value time.Time
		}{
			{"usage_hourly", now.UTC().Truncate(time.Hour)},
			{"usage_daily", time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)},
			{"usage_monthly", time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)},
		} {
			query := fmt.Sprintf(`
				INSERT INTO %s(device_id,bucket,upload,download) VALUES(?,?,?,?)
				ON CONFLICT(device_id,bucket) DO UPDATE SET
				  upload=upload+excluded.upload,download=download+excluded.download`, bucket.table)
			if _, err := txDB.ExecContext(ctx, query, deviceID,
				bucket.value.Format(time.RFC3339), uploadDelta, downloadDelta); err != nil {
				return err
			}
		}
	}
	return txDB.Commit()
}

func (s *Store) Usage(
	ctx context.Context,
	deviceID, bucket string,
	from, to time.Time,
) ([]model.UsagePoint, error) {
	table := map[string]string{"hour": "usage_hourly", "day": "usage_daily", "month": "usage_monthly"}[bucket]
	if table == "" {
		return nil, errors.New("invalid usage bucket")
	}
	query := fmt.Sprintf(`
		SELECT device_id,bucket,upload,download FROM %s
		WHERE (?='' OR device_id=?) AND bucket>=? AND bucket<?
		ORDER BY bucket,device_id`, table)
	rows, err := s.db.QueryContext(ctx, query, deviceID, deviceID,
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make([]model.UsagePoint, 0)
	for rows.Next() {
		var point model.UsagePoint
		var bucketValue string
		if err := rows.Scan(&point.DeviceID, &bucketValue, &point.UploadBytes,
			&point.DownloadBytes); err != nil {
			return nil, err
		}
		point.Bucket, _ = time.Parse(time.RFC3339, bucketValue)
		points = append(points, point)
	}
	return points, rows.Err()
}

func (s *Store) AddAudit(ctx context.Context, event model.AuditEvent) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events(at,actor,action,device_id,remote_ip,detail)
		VALUES(?,?,?,?,?,?)`, event.At.UTC().Format(time.RFC3339Nano), event.Actor,
		event.Action, nullable(event.DeviceID), nullable(event.RemoteIP), event.Detail)
	return err
}

func (s *Store) Audit(ctx context.Context, limit int) ([]model.AuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,at,actor,action,COALESCE(device_id,''),COALESCE(remote_ip,''),detail
		FROM audit_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.AuditEvent, 0)
	for rows.Next() {
		var event model.AuditEvent
		var at string
		if err := rows.Scan(&event.ID, &at, &event.Actor, &event.Action,
			&event.DeviceID, &event.RemoteIP, &event.Detail); err != nil {
			return nil, err
		}
		event.At, _ = time.Parse(time.RFC3339Nano, at)
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *Store) Prune(ctx context.Context, now time.Time) error {
	hourCutoff := now.UTC().Add(-400 * 24 * time.Hour).Format(time.RFC3339)
	dayCutoff := now.UTC().AddDate(-5, 0, 0).Format(time.RFC3339)
	expiredInvite := now.UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []struct {
		query string
		value string
	}{
		{`DELETE FROM usage_hourly WHERE bucket < ?`, hourCutoff},
		{`DELETE FROM usage_daily WHERE bucket < ?`, dayCutoff},
		{`DELETE FROM devices WHERE status='pending' AND id IN
			(SELECT device_id FROM enrollments WHERE expires_at < ? AND used_at IS NULL)`, expiredInvite},
		{`DELETE FROM enrollments WHERE expires_at < ?`, expiredInvite},
		{`DELETE FROM rotations WHERE expires_at < ?`, expiredInvite},
		{`DELETE FROM audit_events WHERE at < ?`, now.UTC().AddDate(-1, 0, 0).Format(time.RFC3339Nano)},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func isConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "constraint")
}
