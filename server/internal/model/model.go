package model

import "time"

const (
	StatusPending = "pending"
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

type Device struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Platform        string     `json:"platform"`
	IPv4            string     `json:"ipv4"`
	IPv6            string     `json:"ipv6"`
	PublicKey       string     `json:"public_key"`
	Status          string     `json:"status"`
	ExternalPrivate bool       `json:"external_private_key"`
	CreatedAt       time.Time  `json:"created_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	QuarantineUntil *time.Time `json:"quarantine_until,omitempty"`
	LastHandshake   *time.Time `json:"last_handshake,omitempty"`
	StatsUpdatedAt  *time.Time `json:"stats_updated_at,omitempty"`
	UploadBytes     int64      `json:"upload_bytes"`
	DownloadBytes   int64      `json:"download_bytes"`
}

type UsagePoint struct {
	DeviceID      string    `json:"device_id"`
	RegionCode    string    `json:"region_code,omitempty"`
	NodeID        string    `json:"node_id,omitempty"`
	Bucket        time.Time `json:"bucket"`
	UploadBytes   int64     `json:"upload_bytes"`
	DownloadBytes int64     `json:"download_bytes"`
}

const (
	ExitModeDualStack     = "dual_stack"
	ExitModeIPv4BlockIPv6 = "ipv4_exit_ipv6_blocked"
	RegionStatusPending   = "pending"
	RegionStatusActive    = "active"
	RegionStatusRevoked   = "revoked"
	NodeHealthUnknown     = "unknown"
	NodeHealthHealthy     = "healthy"
	NodeHealthDegraded    = "degraded"
	NodeHealthOffline     = "offline"
)

type Region struct {
	Code          string   `json:"code"`
	DisplayName   string   `json:"display_name"`
	SortOrder     int      `json:"sort_order"`
	ExitMode      string   `json:"exit_mode"`
	IPv4Network   string   `json:"ipv4_network"`
	IPv6Network   string   `json:"ipv6_network"`
	DNS           []string `json:"dns"`
	MTU           int      `json:"mtu"`
	Enabled       bool     `json:"enabled"`
	ConfigVersion int      `json:"config_version"`
}

type Node struct {
	ID              string     `json:"id"`
	RegionCode      string     `json:"region_code"`
	Endpoint        string     `json:"endpoint"`
	ProbeURL        string     `json:"probe_url"`
	ServerPublicKey string     `json:"server_public_key"`
	Priority        int        `json:"priority"`
	Enabled         bool       `json:"enabled"`
	Health          string     `json:"health"`
	Version         string     `json:"version,omitempty"`
	LastReportAt    *time.Time `json:"last_report_at,omitempty"`
}

type DeviceRegionCredential struct {
	DeviceID   string `json:"device_id"`
	RegionCode string `json:"region_code"`
	IPv4       string `json:"ipv4"`
	IPv6       string `json:"ipv6"`
	PublicKey  string `json:"public_key"`
	Status     string `json:"status"`
}

type RegionConfiguration struct {
	RegionCode          string        `json:"region_code"`
	Address             []string      `json:"address"`
	DNS                 []string      `json:"dns"`
	MTU                 int           `json:"mtu"`
	AllowedIPs          []string      `json:"allowed_ips"`
	PersistentKeepalive int           `json:"persistent_keepalive"`
	ConfigVersion       int           `json:"config_version"`
	Nodes               []CatalogNode `json:"nodes"`
}

type DesiredPeer struct {
	DeviceID     string `json:"device_id"`
	DeviceName   string `json:"device_name"`
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
	IPv4         string `json:"ipv4"`
	IPv6         string `json:"ipv6"`
}

type NodeDesiredState struct {
	Version       int64         `json:"version"`
	NodeID        string        `json:"node_id"`
	RegionCode    string        `json:"region_code"`
	InterfaceIPv4 string        `json:"interface_ipv4"`
	InterfaceIPv6 string        `json:"interface_ipv6"`
	ExitMode      string        `json:"exit_mode"`
	ListenPort    int           `json:"listen_port"`
	Peers         []DesiredPeer `json:"peers"`
}

type Catalog struct {
	Version   int64           `json:"catalog_version"`
	IssuedAt  time.Time       `json:"issued_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	Regions   []CatalogRegion `json:"regions"`
	Signature string          `json:"signature"`
}

type CatalogRegion struct {
	Code          string        `json:"code"`
	DisplayName   string        `json:"display_name"`
	SortOrder     int           `json:"sort_order"`
	ExitMode      string        `json:"exit_mode"`
	IPv4Network   string        `json:"ipv4_network"`
	IPv6Network   string        `json:"ipv6_network"`
	DNS           []string      `json:"dns"`
	MTU           int           `json:"mtu"`
	ConfigVersion int           `json:"config_version"`
	Nodes         []CatalogNode `json:"nodes"`
}

type CatalogNode struct {
	ID              string `json:"id"`
	Endpoint        string `json:"endpoint"`
	ProbeURL        string `json:"probe_url"`
	ServerPublicKey string `json:"server_public_key"`
	Priority        int    `json:"priority"`
}

type NodeReport struct {
	NodeID        string    `json:"node_id"`
	Version       string    `json:"version"`
	Healthy       bool      `json:"healthy"`
	PeerCount     int       `json:"peer_count"`
	LastError     string    `json:"last_error,omitempty"`
	ReportedAt    time.Time `json:"reported_at"`
	UsageSequence int64     `json:"usage_sequence"`
}

type Enrollment struct {
	ID        string     `json:"id"`
	DeviceID  string     `json:"device_id"`
	TokenHash []byte     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

type InviteFile struct {
	Version           int       `json:"version"`
	Type              string    `json:"type"`
	ManagementURL     string    `json:"management_url"`
	Token             string    `json:"token"`
	ExpiresAt         time.Time `json:"expires_at"`
	DeviceName        string    `json:"device_name"`
	CatalogSigningKey string    `json:"catalog_signing_key,omitempty"`
	Purpose           string    `json:"purpose,omitempty"`
}

type AuditEvent struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	DeviceID string    `json:"device_id,omitempty"`
	RemoteIP string    `json:"remote_ip,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

type LiveStats struct {
	PublicKey     string
	ReceiveBytes  int64
	TransmitBytes int64
	LastHandshake time.Time
}
