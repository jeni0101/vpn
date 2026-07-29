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
	Bucket        time.Time `json:"bucket"`
	UploadBytes   int64     `json:"upload_bytes"`
	DownloadBytes int64     `json:"download_bytes"`
}

type Enrollment struct {
	ID        string     `json:"id"`
	DeviceID  string     `json:"device_id"`
	TokenHash []byte     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

type InviteFile struct {
	Version       int       `json:"version"`
	Type          string    `json:"type"`
	ManagementURL string    `json:"management_url"`
	Token         string    `json:"token"`
	ExpiresAt     time.Time `json:"expires_at"`
	DeviceName    string    `json:"device_name"`
	Purpose       string    `json:"purpose,omitempty"`
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
