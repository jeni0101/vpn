package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jeni0101/vpn/server/internal/model"
)

func (s *Store) Regions(ctx context.Context, includeDisabled bool) ([]model.Region, error) {
	query := `SELECT code,display_name,sort_order,exit_mode,ipv4_network,
	                 ipv6_network,dns_json,mtu,enabled,config_version
		FROM regions`
	if !includeDisabled {
		query += ` WHERE enabled=1`
	}
	query += ` ORDER BY sort_order,code`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.Region, 0)
	for rows.Next() {
		var region model.Region
		var enabled int
		var dnsJSON string
		if err := rows.Scan(&region.Code, &region.DisplayName, &region.SortOrder,
			&region.ExitMode, &region.IPv4Network, &region.IPv6Network,
			&dnsJSON, &region.MTU, &enabled, &region.ConfigVersion); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(dnsJSON), &region.DNS); err != nil {
			return nil, err
		}
		region.Enabled = enabled != 0
		result = append(result, region)
	}
	return result, rows.Err()
}

func (s *Store) UpsertRegion(ctx context.Context, region model.Region) error {
	region.Code = strings.ToUpper(strings.TrimSpace(region.Code))
	if region.Code == "" || region.DisplayName == "" ||
		(region.ExitMode != model.ExitModeDualStack &&
			region.ExitMode != model.ExitModeIPv4BlockIPv6) {
		return errors.New("invalid region")
	}
	if region.ConfigVersion < 1 {
		region.ConfigVersion = 1
	}
	if region.MTU == 0 {
		region.MTU = 1420
	}
	dnsJSON, err := json.Marshal(region.DNS)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO regions(
		code,display_name,sort_order,exit_mode,ipv4_network,ipv6_network,
		dns_json,mtu,enabled,config_version,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(code) DO UPDATE SET
		display_name=excluded.display_name,
		sort_order=excluded.sort_order,
		exit_mode=excluded.exit_mode,
		ipv4_network=excluded.ipv4_network,
		ipv6_network=excluded.ipv6_network,
		dns_json=excluded.dns_json,
		mtu=excluded.mtu,
		enabled=excluded.enabled,
		config_version=excluded.config_version,
		updated_at=excluded.updated_at`,
		region.Code, strings.TrimSpace(region.DisplayName), region.SortOrder,
		region.ExitMode, region.IPv4Network, region.IPv6Network, string(dnsJSON),
		region.MTU, boolInt(region.Enabled), region.ConfigVersion,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) Nodes(ctx context.Context, regionCode string, includeDisabled bool) ([]model.Node, error) {
	query := `SELECT id,region_code,endpoint,probe_url,server_public_key,priority,
	                 enabled,health,version,last_report_at
		FROM nodes WHERE 1=1`
	args := make([]any, 0, 1)
	if regionCode != "" {
		query += ` AND region_code=?`
		args = append(args, strings.ToUpper(strings.TrimSpace(regionCode)))
	}
	if !includeDisabled {
		query += ` AND enabled=1`
	}
	query += ` ORDER BY region_code,priority,id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.Node, 0)
	for rows.Next() {
		var node model.Node
		var enabled int
		var lastReport sql.NullString
		if err := rows.Scan(&node.ID, &node.RegionCode, &node.Endpoint, &node.ProbeURL,
			&node.ServerPublicKey, &node.Priority, &enabled, &node.Health,
			&node.Version, &lastReport); err != nil {
			return nil, err
		}
		node.Enabled = enabled != 0
		if lastReport.Valid {
			value, _ := time.Parse(time.RFC3339Nano, lastReport.String)
			node.LastReportAt = &value
		}
		result = append(result, node)
	}
	return result, rows.Err()
}

func (s *Store) UpsertNode(ctx context.Context, node model.Node) error {
	node.ID = strings.ToLower(strings.TrimSpace(node.ID))
	node.RegionCode = strings.ToUpper(strings.TrimSpace(node.RegionCode))
	if node.ID == "" || node.RegionCode == "" || node.Priority < 0 {
		return errors.New("invalid node")
	}
	if node.Health == "" {
		node.Health = model.NodeHealthUnknown
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO nodes(
		id,region_code,endpoint,probe_url,server_public_key,priority,enabled,
		health,version,last_report_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		region_code=excluded.region_code,
		endpoint=excluded.endpoint,
		probe_url=excluded.probe_url,
		server_public_key=excluded.server_public_key,
		priority=excluded.priority,
		enabled=excluded.enabled,
		health=excluded.health,
		version=excluded.version,
		last_report_at=COALESCE(excluded.last_report_at,nodes.last_report_at),
		updated_at=excluded.updated_at`,
		node.ID, node.RegionCode, strings.TrimSpace(node.Endpoint),
		strings.TrimSpace(node.ProbeURL), strings.TrimSpace(node.ServerPublicKey),
		node.Priority, boolInt(node.Enabled), node.Health, node.Version,
		nullableTime(node.LastReportAt), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) Catalog(ctx context.Context, now time.Time) (model.Catalog, error) {
	regions, err := s.Regions(ctx, false)
	if err != nil {
		return model.Catalog{}, err
	}
	result := model.Catalog{
		Version:   now.UTC().Unix(),
		IssuedAt:  now.UTC(),
		ExpiresAt: now.UTC().Add(24 * time.Hour),
		Regions:   make([]model.CatalogRegion, 0, len(regions)),
	}
	for _, region := range regions {
		nodes, err := s.Nodes(ctx, region.Code, false)
		if err != nil {
			return model.Catalog{}, err
		}
		catalogRegion := model.CatalogRegion{
			Code: region.Code, DisplayName: region.DisplayName,
			SortOrder: region.SortOrder, ExitMode: region.ExitMode,
			IPv4Network: region.IPv4Network, IPv6Network: region.IPv6Network,
			DNS: region.DNS, MTU: region.MTU,
			ConfigVersion: region.ConfigVersion,
			Nodes:         make([]model.CatalogNode, 0, len(nodes)),
		}
		for _, node := range nodes {
			if node.Endpoint == "" || node.ProbeURL == "" || node.ServerPublicKey == "" {
				continue
			}
			catalogRegion.Nodes = append(catalogRegion.Nodes, model.CatalogNode{
				ID: node.ID, Endpoint: node.Endpoint, ProbeURL: node.ProbeURL,
				ServerPublicKey: node.ServerPublicKey, Priority: node.Priority,
			})
		}
		if len(catalogRegion.Nodes) > 0 {
			result.Regions = append(result.Regions, catalogRegion)
		}
	}
	return result, nil
}

func (s *Store) UpsertDeviceRegion(
	ctx context.Context,
	credential model.DeviceRegionCredential,
	sealedPSK []byte,
	now time.Time,
) error {
	credential.RegionCode = strings.ToUpper(strings.TrimSpace(credential.RegionCode))
	if credential.DeviceID == "" || credential.RegionCode == "" ||
		credential.IPv4 == "" || credential.IPv6 == "" || credential.PublicKey == "" {
		return errors.New("invalid device region credential")
	}
	if credential.Status == "" {
		credential.Status = model.RegionStatusPending
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO device_region_credentials(
		device_id,region_code,ipv4,ipv6,public_key,psk_sealed,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?)
	ON CONFLICT(device_id,region_code) DO UPDATE SET
		ipv4=excluded.ipv4,ipv6=excluded.ipv6,public_key=excluded.public_key,
		psk_sealed=COALESCE(excluded.psk_sealed,device_region_credentials.psk_sealed),
		status=excluded.status,updated_at=excluded.updated_at`,
		credential.DeviceID, credential.RegionCode, credential.IPv4, credential.IPv6,
		credential.PublicKey, sealedPSK, credential.Status,
		now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	if isConstraint(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) Region(ctx context.Context, code string, requireEnabled bool) (model.Region, error) {
	query := `SELECT code,display_name,sort_order,exit_mode,ipv4_network,
	                 ipv6_network,dns_json,mtu,enabled,config_version
		FROM regions WHERE code=?`
	if requireEnabled {
		query += ` AND enabled=1`
	}
	var region model.Region
	var enabled int
	var dnsJSON string
	err := s.db.QueryRowContext(
		ctx, query, strings.ToUpper(strings.TrimSpace(code)),
	).Scan(&region.Code, &region.DisplayName, &region.SortOrder, &region.ExitMode,
		&region.IPv4Network, &region.IPv6Network, &dnsJSON, &region.MTU,
		&enabled, &region.ConfigVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Region{}, ErrNotFound
	}
	if err != nil {
		return model.Region{}, err
	}
	if err := json.Unmarshal([]byte(dnsJSON), &region.DNS); err != nil {
		return model.Region{}, err
	}
	region.Enabled = enabled != 0
	return region, nil
}

func (s *Store) DeviceRegion(
	ctx context.Context,
	deviceID, regionCode string,
) (model.DeviceRegionCredential, []byte, error) {
	var value model.DeviceRegionCredential
	var sealed []byte
	err := s.db.QueryRowContext(ctx, `SELECT device_id,region_code,ipv4,ipv6,
		public_key,status,psk_sealed FROM device_region_credentials
		WHERE device_id=? AND region_code=?`,
		deviceID, strings.ToUpper(strings.TrimSpace(regionCode)),
	).Scan(&value.DeviceID, &value.RegionCode, &value.IPv4, &value.IPv6,
		&value.PublicKey, &value.Status, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeviceRegionCredential{}, nil, ErrNotFound
	}
	return value, sealed, err
}

func (s *Store) SetDeviceRegionPrivateKey(
	ctx context.Context,
	deviceID, regionCode string,
	sealed []byte,
) error {
	if len(sealed) == 0 {
		return errors.New("invalid sealed private key")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE device_region_credentials
		SET private_key_sealed=?,updated_at=?
		WHERE device_id=? AND region_code=?`,
		sealed, time.Now().UTC().Format(time.RFC3339Nano), deviceID,
		strings.ToUpper(strings.TrimSpace(regionCode)))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeviceRegionSecrets(
	ctx context.Context,
	deviceID, regionCode string,
) ([]byte, []byte, error) {
	var sealedPSK, sealedPrivate []byte
	err := s.db.QueryRowContext(ctx, `SELECT psk_sealed,private_key_sealed
		FROM device_region_credentials WHERE device_id=? AND region_code=?`,
		deviceID, strings.ToUpper(strings.TrimSpace(regionCode)),
	).Scan(&sealedPSK, &sealedPrivate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	return sealedPSK, sealedPrivate, err
}

func (s *Store) DeviceSlot(ctx context.Context, deviceID string) (int, error) {
	var slot int
	err := s.db.QueryRowContext(ctx, `SELECT slot FROM devices WHERE id=?`, deviceID).Scan(&slot)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return slot, err
}

type DesiredCredential struct {
	Device     model.Device
	Credential model.DeviceRegionCredential
	SealedPSK  []byte
}

func (s *Store) DesiredCredentials(
	ctx context.Context,
	regionCode string,
) ([]DesiredCredential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		d.id,d.name,d.platform,d.ipv4,d.ipv6,d.public_key,d.status,
		d.external_private,d.created_at,d.revoked_at,d.quarantine_until,
		u.last_handshake,u.updated_at,
		COALESCE(u.total_upload,0),COALESCE(u.total_download,0),
		c.device_id,c.region_code,c.ipv4,c.ipv6,c.public_key,c.status,c.psk_sealed
		FROM device_region_credentials c
		JOIN devices d ON d.id=c.device_id
		LEFT JOIN usage_state u ON u.device_id=d.id
		WHERE c.region_code=? AND c.status=? AND d.status=?
		ORDER BY d.slot`,
		strings.ToUpper(strings.TrimSpace(regionCode)),
		model.RegionStatusActive, model.StatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]DesiredCredential, 0)
	for rows.Next() {
		var item DesiredCredential
		var created, revoked, quarantine, handshake, statsUpdated sql.NullString
		var external int
		err := rows.Scan(
			&item.Device.ID, &item.Device.Name, &item.Device.Platform,
			&item.Device.IPv4, &item.Device.IPv6, &item.Device.PublicKey,
			&item.Device.Status, &external, &created, &revoked, &quarantine,
			&handshake, &statsUpdated, &item.Device.UploadBytes,
			&item.Device.DownloadBytes, &item.Credential.DeviceID,
			&item.Credential.RegionCode, &item.Credential.IPv4,
			&item.Credential.IPv6, &item.Credential.PublicKey,
			&item.Credential.Status, &item.SealedPSK,
		)
		if err != nil {
			return nil, err
		}
		item.Device.ExternalPrivate = external != 0
		parseTimes(&item.Device, created, revoked, quarantine, handshake, statsUpdated)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) DeviceRegions(ctx context.Context, deviceID string) ([]model.DeviceRegionCredential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,region_code,ipv4,ipv6,
		public_key,status FROM device_region_credentials
		WHERE device_id=? ORDER BY region_code`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.DeviceRegionCredential, 0)
	for rows.Next() {
		var item model.DeviceRegionCredential
		if err := rows.Scan(&item.DeviceID, &item.RegionCode, &item.IPv4, &item.IPv6,
			&item.PublicKey, &item.Status); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateDeviceToken(
	ctx context.Context,
	deviceID, token string,
	now, expiresAt time.Time,
) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, `INSERT INTO device_tokens(
		token_hash,device_id,created_at,expires_at
	) VALUES(?,?,?,?)`, hash[:], deviceID, now.UTC().Format(time.RFC3339Nano),
		expiresAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) DeviceForToken(ctx context.Context, token string, now time.Time) (model.Device, error) {
	hash := sha256.Sum256([]byte(token))
	var deviceID, expires string
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT device_id,expires_at,revoked_at
		FROM device_tokens WHERE token_hash=?`, hash[:]).Scan(&deviceID, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, ErrNotFound
	}
	if err != nil {
		return model.Device{}, err
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || revoked.Valid || !expiry.After(now) {
		return model.Device{}, ErrExpired
	}
	device, _, _, err := s.Device(ctx, deviceID)
	return device, err
}

func (s *Store) RevokeDeviceTokens(ctx context.Context, deviceID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE device_tokens SET revoked_at=?
		WHERE device_id=? AND revoked_at IS NULL`,
		now.UTC().Format(time.RFC3339Nano), deviceID)
	return err
}

func (s *Store) RevokeDeviceRegions(ctx context.Context, deviceID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE device_region_credentials
		SET status=?,updated_at=? WHERE device_id=? AND status!=?`,
		model.RegionStatusRevoked, now.UTC().Format(time.RFC3339Nano),
		deviceID, model.RegionStatusRevoked)
	return err
}

func (s *Store) SaveNodeReport(ctx context.Context, report model.NodeReport) error {
	if report.NodeID == "" {
		return errors.New("invalid node report")
	}
	health := model.NodeHealthDegraded
	if report.Healthy {
		health = model.NodeHealthHealthy
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO node_reports(
		node_id,version,healthy,peer_count,last_error,usage_sequence,reported_at
	) VALUES(?,?,?,?,?,?,?)
	ON CONFLICT(node_id) DO UPDATE SET
		version=excluded.version,healthy=excluded.healthy,
		peer_count=excluded.peer_count,last_error=excluded.last_error,
		usage_sequence=excluded.usage_sequence,reported_at=excluded.reported_at`,
		report.NodeID, report.Version, boolInt(report.Healthy), report.PeerCount,
		report.LastError, report.UsageSequence, report.ReportedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE nodes SET health=?,version=?,last_report_at=?,
		updated_at=? WHERE id=?`, health, report.Version,
		report.ReportedAt.UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano), report.NodeID)
	return err
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
