package manager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/jeni0101/vpn/server/internal/config"
	"github.com/jeni0101/vpn/server/internal/model"
	"github.com/jeni0101/vpn/server/internal/security"
	"github.com/jeni0101/vpn/server/internal/store"
)

var safeName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,62}$`)

type Service struct {
	cfg    config.Manager
	store  *store.Store
	sealer *security.Sealer
	wg     WireGuard
	mu     sync.Mutex
	now    func() time.Time
}

type CreateRequest struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type StandardResult struct {
	Device model.Device `json:"device"`
	Config string       `json:"config"`
}

type InviteResult struct {
	Device model.Device     `json:"device"`
	Invite model.InviteFile `json:"invite"`
}

type ClaimRequest struct {
	Token        string `json:"token"`
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
}

type ClientSettings struct {
	ServerPublicKey     string   `json:"server_public_key"`
	Endpoint            string   `json:"endpoint"`
	DNS                 []string `json:"dns"`
	MTU                 int      `json:"mtu"`
	AllowedIPs          []string `json:"allowed_ips"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
}

type ClaimResult struct {
	Device        model.Device   `json:"device"`
	Configuration ClientSettings `json:"configuration"`
}

type ImportRequest struct {
	Devices []ImportDevice `json:"devices"`
}

type ImportDevice struct {
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	IPv4         string `json:"ipv4"`
	IPv6         string `json:"ipv6"`
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
	CreatedAt    string `json:"created_at"`
}

func NewService(
	cfg config.Manager,
	db *store.Store,
	sealer *security.Sealer,
	wg WireGuard,
) *Service {
	return &Service{cfg: cfg, store: db, sealer: sealer, wg: wg, now: time.Now}
}

func (s *Service) Devices(ctx context.Context) ([]model.Device, error) {
	return s.store.ListDevices(ctx)
}

func (s *Service) CreateInvite(ctx context.Context, request CreateRequest) (InviteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, platform, err := validateCreate(request)
	if err != nil {
		return InviteResult{}, err
	}
	now := s.now().UTC()
	slot, err := s.store.NextSlot(ctx, now)
	if err != nil {
		return InviteResult{}, err
	}
	device := newDevice(name, platform, slot, model.StatusPending, now)
	if err := s.store.CreateDevice(ctx, device, slot, nil); err != nil {
		return InviteResult{}, err
	}
	token, tokenHash, err := token()
	if err != nil {
		return InviteResult{}, err
	}
	enrollment := model.Enrollment{
		ID:        newID(),
		DeviceID:  device.ID,
		TokenHash: tokenHash,
		ExpiresAt: now.Add(s.cfg.InviteTTL),
	}
	if err := s.store.CreateEnrollment(ctx, enrollment); err != nil {
		_ = s.store.DeletePendingDevice(ctx, device.ID)
		return InviteResult{}, err
	}
	return InviteResult{
		Device: device,
		Invite: model.InviteFile{
			Version:       1,
			Type:          "tnest-vpn-enrollment",
			ManagementURL: s.cfg.ManagementURL,
			Token:         token,
			ExpiresAt:     enrollment.ExpiresAt,
			DeviceName:    device.Name,
			Purpose:       "enroll",
		},
	}, nil
}

func (s *Service) CreateRotationInvite(ctx context.Context, id string) (InviteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, _, _, err := s.store.Device(ctx, id)
	if err != nil {
		return InviteResult{}, err
	}
	if device.Status != model.StatusActive {
		return InviteResult{}, errors.New("only active devices can be rotated")
	}
	value, hash, err := token()
	if err != nil {
		return InviteResult{}, err
	}
	now := s.now().UTC()
	rotation := model.Enrollment{
		ID: newID(), DeviceID: device.ID, TokenHash: hash,
		ExpiresAt: now.Add(s.cfg.InviteTTL),
	}
	if err := s.store.CreateRotation(ctx, rotation); err != nil {
		return InviteResult{}, err
	}
	return InviteResult{
		Device: device,
		Invite: model.InviteFile{
			Version: 1, Type: "tnest-vpn-enrollment",
			ManagementURL: s.cfg.ManagementURL, Token: value,
			ExpiresAt: rotation.ExpiresAt, DeviceName: device.Name,
			Purpose: "rotate",
		},
	}, nil
}

func (s *Service) CreateStandard(ctx context.Context, request CreateRequest) (StandardResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, platform, err := validateCreate(request)
	if err != nil {
		return StandardResult{}, err
	}
	now := s.now().UTC()
	slot, err := s.store.NextSlot(ctx, now)
	if err != nil {
		return StandardResult{}, err
	}
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return StandardResult{}, err
	}
	publicKey := privateKey.PublicKey()
	psk, err := wgtypes.GenerateKey()
	if err != nil {
		return StandardResult{}, err
	}
	sealed, err := s.sealer.Seal([]byte(psk.String()), "device-psk")
	if err != nil {
		return StandardResult{}, err
	}
	device := newDevice(name, platform, slot, model.StatusActive, now)
	device.PublicKey = publicKey.String()
	oldPeers, err := s.peerMaterials(ctx, "")
	if err != nil {
		return StandardResult{}, err
	}
	candidate := append(append([]PeerMaterial(nil), oldPeers...), PeerMaterial{
		Device: device, PresharedKey: psk.String(),
	})
	if err := s.wg.Apply(ctx, candidate); err != nil {
		return StandardResult{}, err
	}
	if err := s.store.CreateDevice(ctx, device, slot, sealed); err != nil {
		_ = s.wg.Apply(ctx, oldPeers)
		return StandardResult{}, err
	}
	return StandardResult{
		Device: device,
		Config: s.renderClient(device, privateKey.String(), psk.String()),
	}, nil
}

func (s *Service) Claim(ctx context.Context, request ClaimRequest) (model.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(request.Token) < 32 {
		return model.Device{}, errors.New("invalid enrollment token")
	}
	if _, err := wgtypes.ParseKey(request.PublicKey); err != nil {
		return model.Device{}, errors.New("invalid public key")
	}
	if _, err := wgtypes.ParseKey(request.PresharedKey); err != nil {
		return model.Device{}, errors.New("invalid preshared key")
	}
	oldPeers, err := s.peerMaterials(ctx, "")
	if err != nil {
		return model.Device{}, err
	}
	sealed, err := s.sealer.Seal([]byte(request.PresharedKey), "device-psk")
	if err != nil {
		return model.Device{}, err
	}
	hash := sha256.Sum256([]byte(request.Token))
	device, err := s.store.ClaimEnrollment(ctx, hash[:], request.PublicKey, sealed, s.now().UTC())
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return model.Device{}, err
		}
		return s.claimRotation(ctx, hash[:], request, sealed, oldPeers)
	}
	candidate := append(append([]PeerMaterial(nil), oldPeers...), PeerMaterial{
		Device: device, PresharedKey: request.PresharedKey,
	})
	if err := s.wg.Apply(ctx, candidate); err != nil {
		_ = s.store.UndoClaim(ctx, device.ID)
		_ = s.wg.Apply(ctx, oldPeers)
		return model.Device{}, err
	}
	return device, nil
}

func (s *Service) claimRotation(
	ctx context.Context,
	tokenHash []byte,
	request ClaimRequest,
	sealed []byte,
	oldPeers []PeerMaterial,
) (model.Device, error) {
	now := s.now().UTC()
	device, err := s.store.RotationDevice(ctx, tokenHash, now)
	if err != nil {
		return model.Device{}, err
	}
	rotated := device
	rotated.PublicKey = request.PublicKey
	candidate := make([]PeerMaterial, 0, len(oldPeers))
	for _, peer := range oldPeers {
		if peer.Device.ID == device.ID {
			candidate = append(candidate, PeerMaterial{
				Device: rotated, PresharedKey: request.PresharedKey,
			})
		} else {
			candidate = append(candidate, peer)
		}
	}
	if err := s.wg.Apply(ctx, candidate); err != nil {
		return model.Device{}, err
	}
	if err := s.store.FinalizeRotation(ctx, tokenHash, request.PublicKey, sealed, now); err != nil {
		_ = s.wg.Apply(ctx, oldPeers)
		return model.Device{}, err
	}
	return rotated, nil
}

func (s *Service) Settings() ClientSettings {
	return ClientSettings{
		ServerPublicKey:     s.cfg.ServerPublicKey,
		Endpoint:            s.cfg.Endpoint,
		DNS:                 []string{"1.1.1.1", "1.0.0.1", "2606:4700:4700::1111", "2606:4700:4700::1001"},
		MTU:                 1420,
		AllowedIPs:          []string{"0.0.0.0/0", "::/0"},
		PersistentKeepalive: 25,
	}
}

func (s *Service) Revoke(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, _, _, err := s.store.Device(ctx, id)
	if err != nil {
		return err
	}
	if device.Status != model.StatusActive {
		return errors.New("only active devices can be revoked")
	}
	oldPeers, err := s.peerMaterials(ctx, "")
	if err != nil {
		return err
	}
	candidate := make([]PeerMaterial, 0, len(oldPeers)-1)
	for _, peer := range oldPeers {
		if peer.Device.ID != id {
			candidate = append(candidate, peer)
		}
	}
	if err := s.wg.Apply(ctx, candidate); err != nil {
		return err
	}
	now := s.now().UTC()
	if err := s.store.RevokeDevice(ctx, id, now, now.Add(s.cfg.Quarantine)); err != nil {
		_ = s.wg.Apply(ctx, oldPeers)
		return err
	}
	return nil
}

func (s *Service) Import(ctx context.Context, request ImportRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.ApplyChanges {
		return errors.New("legacy import is only allowed in read-only mode")
	}
	for _, item := range request.Devices {
		if !safeName.MatchString(item.Name) {
			return fmt.Errorf("invalid device name %q", item.Name)
		}
		publicKey, err := wgtypes.ParseKey(item.PublicKey)
		if err != nil || publicKey.String() != item.PublicKey {
			return fmt.Errorf("invalid public key for %s", item.Name)
		}
		if _, err := wgtypes.ParseKey(item.PresharedKey); err != nil {
			return fmt.Errorf("invalid PSK for %s", item.Name)
		}
		slot, err := slotFromIPv4(item.IPv4)
		if err != nil {
			return err
		}
		if item.IPv6 != fmt.Sprintf("fd66:66:66::%d", slot) {
			return fmt.Errorf("invalid VPN IPv6 address %q for slot %d", item.IPv6, slot)
		}
		created := s.now().UTC()
		if item.CreatedAt != "" {
			if parsed, err := time.Parse("20060102T150405Z", item.CreatedAt); err == nil {
				created = parsed
			}
		}
		sealed, err := s.sealer.Seal([]byte(item.PresharedKey), "device-psk")
		if err != nil {
			return err
		}
		device := model.Device{
			ID:              stableID(item.Name),
			Name:            item.Name,
			Platform:        normalizePlatform(item.Platform),
			IPv4:            item.IPv4,
			IPv6:            item.IPv6,
			PublicKey:       item.PublicKey,
			Status:          model.StatusActive,
			ExternalPrivate: true,
			CreatedAt:       created,
		}
		if err := s.store.ImportDevice(ctx, device, slot, sealed); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Usage(
	ctx context.Context,
	deviceID, bucket string,
	from, to time.Time,
) ([]model.UsagePoint, error) {
	return s.store.Usage(ctx, deviceID, bucket, from, to)
}

func (s *Service) Audit(ctx context.Context, limit int) ([]model.AuditEvent, error) {
	return s.store.Audit(ctx, limit)
}

func (s *Service) AddAudit(ctx context.Context, event model.AuditEvent) error {
	if event.At.IsZero() {
		event.At = s.now().UTC()
	}
	return s.store.AddAudit(ctx, event)
}

func (s *Service) Poll(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	s.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollOnce(ctx)
		}
	}
}

func (s *Service) pollOnce(ctx context.Context) {
	stats, err := s.wg.Stats(ctx)
	if err != nil {
		return
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return
	}
	now := s.now().UTC()
	for _, device := range devices {
		if device.Status != model.StatusActive {
			continue
		}
		current, ok := stats[device.PublicKey]
		if !ok {
			continue
		}
		_ = s.store.RecordStats(ctx, device.ID, current.ReceiveBytes,
			current.TransmitBytes, current.LastHandshake, now)
	}
	_ = s.store.Prune(ctx, now)
}

func (s *Service) peerMaterials(ctx context.Context, skipID string) ([]PeerMaterial, error) {
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]PeerMaterial, 0, len(devices))
	for _, device := range devices {
		if device.Status != model.StatusActive || device.ID == skipID {
			continue
		}
		_, sealed, _, err := s.store.Device(ctx, device.ID)
		if err != nil {
			return nil, err
		}
		plaintext, err := s.sealer.Open(sealed, "device-psk")
		if err != nil {
			return nil, fmt.Errorf("decrypt PSK for %s: %w", device.Name, err)
		}
		result = append(result, PeerMaterial{Device: device, PresharedKey: string(plaintext)})
	}
	return result, nil
}

func (s *Service) renderClient(device model.Device, privateKey, psk string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32, %s/128
DNS = 1.1.1.1, 1.0.0.1, 2606:4700:4700::1111, 2606:4700:4700::1001
MTU = 1420

[Peer]
PublicKey = %s
PresharedKey = %s
Endpoint = %s
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`, privateKey, device.IPv4, device.IPv6, s.cfg.ServerPublicKey, psk, s.cfg.Endpoint)
}

func validateCreate(request CreateRequest) (string, string, error) {
	name := strings.TrimSpace(request.Name)
	if !safeName.MatchString(name) {
		return "", "", errors.New("device name must be 1-63 safe characters")
	}
	platform := normalizePlatform(request.Platform)
	if platform == "" {
		return "", "", errors.New("unsupported platform")
	}
	return name, platform, nil
}

func normalizePlatform(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "windows":
		return "windows"
	case "android":
		return "android"
	case "ios":
		return "ios"
	case "macos":
		return "macos"
	case "other":
		return "other"
	default:
		return ""
	}
}

func newDevice(name, platform string, slot int, status string, now time.Time) model.Device {
	return model.Device{
		ID:        newID(),
		Name:      name,
		Platform:  platform,
		IPv4:      fmt.Sprintf("10.66.0.%d", slot),
		IPv6:      fmt.Sprintf("fd66:66:66::%d", slot),
		Status:    status,
		CreatedAt: now,
	}
}

func slotFromIPv4(value string) (int, error) {
	var slot int
	if _, err := fmt.Sscanf(value, "10.66.0.%d", &slot); err != nil ||
		slot < 10 || slot > 254 || value != fmt.Sprintf("10.66.0.%d", slot) {
		return 0, fmt.Errorf("invalid VPN IPv4 address %q", value)
	}
	return slot, nil
}

func token() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(value))
	return value, hash[:], nil
}

func newID() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

func stableID(name string) string {
	hash := sha256.Sum256([]byte("tnest-existing:" + strings.ToLower(name)))
	raw := hash[:16]
	raw[6] = (raw[6] & 0x0f) | 0x50
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}
