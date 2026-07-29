package manager

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/jeni0101/vpn/server/internal/catalog"
	"github.com/jeni0101/vpn/server/internal/config"
	"github.com/jeni0101/vpn/server/internal/model"
	"github.com/jeni0101/vpn/server/internal/security"
	"github.com/jeni0101/vpn/server/internal/store"
)

var safeName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,62}$`)

type Service struct {
	cfg           config.Manager
	store         *store.Store
	sealer        *security.Sealer
	wg            WireGuard
	catalogSigner *catalog.Signer
	mu            sync.Mutex
	now           func() time.Time
}

func (s *Service) SetCatalogSigner(signer *catalog.Signer) {
	s.catalogSigner = signer
}

func (s *Service) EnsureLocalNode(ctx context.Context) error {
	regions, err := s.store.Regions(ctx, true)
	if err != nil {
		return err
	}
	found := false
	for _, region := range regions {
		if region.Code == s.cfg.RegionCode {
			found = true
			break
		}
	}
	if !found {
		if err := s.store.UpsertRegion(ctx, model.Region{
			Code: s.cfg.RegionCode, DisplayName: s.cfg.RegionCode,
			SortOrder: 100, ExitMode: s.cfg.ExitMode,
			Enabled: false, ConfigVersion: 1,
		}); err != nil {
			return err
		}
	}
	return s.store.UpsertNode(ctx, model.Node{
		ID: s.cfg.NodeID, RegionCode: s.cfg.RegionCode,
		Endpoint: s.cfg.Endpoint, ProbeURL: s.cfg.NodeProbeURL,
		ServerPublicKey: s.cfg.ServerPublicKey, Priority: 10,
		Enabled: s.cfg.RegionCode == "SG", Health: model.NodeHealthHealthy,
	})
}

func (s *Service) Regions(ctx context.Context, includeDisabled bool) ([]model.Region, error) {
	return s.store.Regions(ctx, includeDisabled)
}

func (s *Service) Nodes(ctx context.Context, regionCode string, includeDisabled bool) ([]model.Node, error) {
	return s.store.Nodes(ctx, regionCode, includeDisabled)
}

func (s *Service) UpsertRegion(ctx context.Context, region model.Region) error {
	return s.store.UpsertRegion(ctx, region)
}

func (s *Service) UpsertNode(ctx context.Context, node model.Node) error {
	return s.store.UpsertNode(ctx, node)
}

func (s *Service) Catalog(ctx context.Context) (model.Catalog, error) {
	if s.catalogSigner == nil {
		return model.Catalog{}, errors.New("catalog signing is unavailable")
	}
	value, err := s.store.Catalog(ctx, s.now())
	if err != nil {
		return model.Catalog{}, err
	}
	if err := s.catalogSigner.Sign(&value); err != nil {
		return model.Catalog{}, err
	}
	return value, nil
}

func (s *Service) CatalogPublicKey() string {
	if s.catalogSigner == nil {
		return ""
	}
	return s.catalogSigner.PublicKey()
}

func (s *Service) SaveNodeReport(ctx context.Context, report model.NodeReport) error {
	if report.ReportedAt.IsZero() {
		report.ReportedAt = s.now().UTC()
	}
	return s.store.SaveNodeReport(ctx, report)
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

type RegionKeyRequest struct {
	RegionCode   string `json:"region_code"`
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
}

type ClaimV2Request struct {
	Token       string             `json:"token"`
	Credentials []RegionKeyRequest `json:"credentials"`
}

type ClaimV2Result struct {
	Device      model.Device                `json:"device"`
	DeviceToken string                      `json:"device_token"`
	Catalog     model.Catalog               `json:"catalog"`
	Regions     []model.RegionConfiguration `json:"regions"`
}

type EnrollRegionResult struct {
	Configuration model.RegionConfiguration `json:"configuration"`
}

type AppleBundleResult struct {
	Device model.Device `json:"device"`
	Bundle []byte       `json:"bundle"`
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
			Version:           2,
			Type:              "tnest-vpn-enrollment",
			ManagementURL:     s.cfg.ManagementURL,
			Token:             token,
			ExpiresAt:         enrollment.ExpiresAt,
			DeviceName:        device.Name,
			CatalogSigningKey: s.CatalogPublicKey(),
			Purpose:           "enroll",
		},
	}, nil
}

func (s *Service) ClaimV2(ctx context.Context, request ClaimV2Request) (ClaimV2Result, error) {
	if len(request.Credentials) == 0 || len(request.Credentials) > 32 {
		return ClaimV2Result{}, errors.New("invalid region credentials")
	}
	seen := make(map[string]bool, len(request.Credentials))
	var singapore RegionKeyRequest
	for _, credential := range request.Credentials {
		code := strings.ToUpper(strings.TrimSpace(credential.RegionCode))
		if code == "" || seen[code] {
			return ClaimV2Result{}, errors.New("invalid or duplicate region credential")
		}
		seen[code] = true
		if code == "SG" {
			singapore = credential
		}
	}
	if singapore.RegionCode == "" {
		return ClaimV2Result{}, errors.New("Singapore credential is required for initial enrollment")
	}
	device, err := s.Claim(ctx, ClaimRequest{
		Token: request.Token, PublicKey: singapore.PublicKey,
		PresharedKey: singapore.PresharedKey,
	})
	request.Token = ""
	if err != nil {
		return ClaimV2Result{}, err
	}
	_, sealed, _, err := s.store.Device(ctx, device.ID)
	if err != nil {
		return ClaimV2Result{}, err
	}
	if err := s.store.UpsertDeviceRegion(ctx, model.DeviceRegionCredential{
		DeviceID: device.ID, RegionCode: "SG", IPv4: device.IPv4,
		IPv6: device.IPv6, PublicKey: device.PublicKey,
		Status: model.RegionStatusActive,
	}, sealed, s.now().UTC()); err != nil {
		return ClaimV2Result{}, err
	}
	deviceToken, _, err := token()
	if err != nil {
		return ClaimV2Result{}, err
	}
	if err := s.store.CreateDeviceToken(
		ctx, device.ID, deviceToken, s.now().UTC(), s.now().UTC().Add(365*24*time.Hour),
	); err != nil {
		return ClaimV2Result{}, err
	}
	configurations := make([]model.RegionConfiguration, 0, len(request.Credentials))
	configuration, err := s.RegionConfiguration(ctx, device.ID, "SG")
	if err != nil {
		return ClaimV2Result{}, err
	}
	configurations = append(configurations, configuration)
	for _, credential := range request.Credentials {
		if strings.EqualFold(credential.RegionCode, "SG") {
			continue
		}
		if _, err := s.EnrollRegion(ctx, device, credential); err != nil {
			return ClaimV2Result{}, err
		}
		value, err := s.RegionConfiguration(ctx, device.ID, credential.RegionCode)
		if err != nil {
			return ClaimV2Result{}, err
		}
		configurations = append(configurations, value)
	}
	catalogValue, err := s.Catalog(ctx)
	if err != nil {
		return ClaimV2Result{}, err
	}
	return ClaimV2Result{
		Device: device, DeviceToken: deviceToken,
		Catalog: catalogValue, Regions: configurations,
	}, nil
}

func (s *Service) AuthenticateDevice(
	ctx context.Context,
	tokenValue string,
) (model.Device, error) {
	if len(tokenValue) < 32 {
		return model.Device{}, store.ErrNotFound
	}
	device, err := s.store.DeviceForToken(ctx, tokenValue, s.now().UTC())
	if err != nil {
		return model.Device{}, err
	}
	if device.Status != model.StatusActive {
		return model.Device{}, store.ErrNotFound
	}
	return device, nil
}

func (s *Service) EnrollRegion(
	ctx context.Context,
	device model.Device,
	request RegionKeyRequest,
) (model.DeviceRegionCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code := strings.ToUpper(strings.TrimSpace(request.RegionCode))
	region, err := s.store.Region(ctx, code, true)
	if err != nil {
		return model.DeviceRegionCredential{}, err
	}
	if _, err := wgtypes.ParseKey(request.PublicKey); err != nil {
		return model.DeviceRegionCredential{}, errors.New("invalid public key")
	}
	if _, err := wgtypes.ParseKey(request.PresharedKey); err != nil {
		return model.DeviceRegionCredential{}, errors.New("invalid preshared key")
	}
	if existing, _, err := s.store.DeviceRegion(ctx, device.ID, code); err == nil {
		if existing.PublicKey == request.PublicKey &&
			existing.Status == model.RegionStatusActive {
			return existing, nil
		}
		return model.DeviceRegionCredential{}, store.ErrConflict
	} else if !errors.Is(err, store.ErrNotFound) {
		return model.DeviceRegionCredential{}, err
	}
	slot, err := s.store.DeviceSlot(ctx, device.ID)
	if err != nil {
		return model.DeviceRegionCredential{}, err
	}
	ipv4, err := addressAt(region.IPv4Network, slot)
	if err != nil {
		return model.DeviceRegionCredential{}, err
	}
	ipv6, err := addressAt(region.IPv6Network, slot)
	if err != nil {
		return model.DeviceRegionCredential{}, err
	}
	sealed, err := s.sealer.Seal(
		[]byte(request.PresharedKey), "device-region-psk:"+code,
	)
	if err != nil {
		return model.DeviceRegionCredential{}, err
	}
	credential := model.DeviceRegionCredential{
		DeviceID: device.ID, RegionCode: code, IPv4: ipv4, IPv6: ipv6,
		PublicKey: request.PublicKey, Status: model.RegionStatusActive,
	}
	if err := s.store.UpsertDeviceRegion(ctx, credential, sealed, s.now().UTC()); err != nil {
		return model.DeviceRegionCredential{}, err
	}
	return credential, nil
}

func (s *Service) RegionConfiguration(
	ctx context.Context,
	deviceID, regionCode string,
) (model.RegionConfiguration, error) {
	credential, _, err := s.store.DeviceRegion(ctx, deviceID, regionCode)
	if err != nil {
		return model.RegionConfiguration{}, err
	}
	region, err := s.store.Region(ctx, credential.RegionCode, true)
	if err != nil {
		return model.RegionConfiguration{}, err
	}
	nodes, err := s.store.Nodes(ctx, credential.RegionCode, false)
	if err != nil {
		return model.RegionConfiguration{}, err
	}
	catalogNodes := make([]model.CatalogNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Endpoint == "" || node.ProbeURL == "" || node.ServerPublicKey == "" {
			continue
		}
		catalogNodes = append(catalogNodes, model.CatalogNode{
			ID: node.ID, Endpoint: node.Endpoint, ProbeURL: node.ProbeURL,
			ServerPublicKey: node.ServerPublicKey, Priority: node.Priority,
		})
	}
	if len(catalogNodes) == 0 {
		return model.RegionConfiguration{}, errors.New("region has no ready node")
	}
	return model.RegionConfiguration{
		RegionCode: credential.RegionCode,
		Address:    []string{credential.IPv4 + "/32", credential.IPv6 + "/128"},
		DNS:        append([]string(nil), region.DNS...), MTU: region.MTU,
		AllowedIPs: []string{"0.0.0.0/0", "::/0"}, PersistentKeepalive: 25,
		ConfigVersion: region.ConfigVersion, Nodes: catalogNodes,
	}, nil
}

func (s *Service) DeviceRegions(
	ctx context.Context,
	deviceID string,
) ([]model.RegionConfiguration, error) {
	credentials, err := s.store.DeviceRegions(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	result := make([]model.RegionConfiguration, 0, len(credentials))
	for _, credential := range credentials {
		if credential.Status != model.RegionStatusActive {
			continue
		}
		value, err := s.RegionConfiguration(ctx, deviceID, credential.RegionCode)
		if errors.Is(err, store.ErrNotFound) ||
			(err != nil && strings.Contains(err.Error(), "no ready node")) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Service) ClientUsage(
	ctx context.Context,
	deviceID, rangeValue, regionCode string,
) ([]model.UsagePoint, string, error) {
	now := s.now().UTC()
	var from time.Time
	var bucket string
	switch rangeValue {
	case "", "24h":
		from, bucket = now.Add(-24*time.Hour), "hour"
	case "7d":
		from, bucket = now.Add(-7*24*time.Hour), "day"
	case "30d":
		from, bucket = now.Add(-30*24*time.Hour), "day"
	default:
		return nil, "", errors.New("invalid usage range")
	}
	code := strings.ToUpper(strings.TrimSpace(regionCode))
	if code == "" {
		code = "SG"
	}
	if _, _, err := s.store.DeviceRegion(ctx, deviceID, code); err != nil {
		return nil, "", err
	}
	// The legacy Singapore collector remains authoritative until the Singapore
	// node agent is migrated. Every additional region uses node reports.
	if code != "SG" {
		points, err := s.store.RegionalUsage(
			ctx, deviceID, code, bucket, from, now.Add(time.Hour),
		)
		return points, bucket, err
	}
	points, err := s.store.Usage(ctx, deviceID, bucket, from, now.Add(time.Hour))
	if err != nil {
		return nil, "", err
	}
	for index := range points {
		points[index].RegionCode = "SG"
		points[index].NodeID = "sg-sin-01"
	}
	return points, bucket, nil
}

func (s *Service) DesiredState(
	ctx context.Context,
	nodeID string,
) (model.NodeDesiredState, error) {
	nodes, err := s.store.Nodes(ctx, "", true)
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	var node model.Node
	for _, candidate := range nodes {
		if candidate.ID == strings.ToLower(strings.TrimSpace(nodeID)) {
			node = candidate
			break
		}
	}
	if node.ID == "" {
		return model.NodeDesiredState{}, store.ErrNotFound
	}
	region, err := s.store.Region(ctx, node.RegionCode, false)
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	credentials, err := s.store.DesiredCredentials(ctx, node.RegionCode)
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	peers := make([]model.DesiredPeer, 0, len(credentials))
	for _, item := range credentials {
		contextName := "device-region-psk:" + node.RegionCode
		if node.RegionCode == "SG" {
			contextName = "device-psk"
		}
		plaintext, err := s.sealer.Open(item.SealedPSK, contextName)
		if err != nil {
			return model.NodeDesiredState{}, fmt.Errorf(
				"decrypt region credential for %s: %w", item.Device.ID, err,
			)
		}
		peers = append(peers, model.DesiredPeer{
			DeviceID: item.Device.ID, DeviceName: item.Device.Name,
			PublicKey: item.Credential.PublicKey, PresharedKey: string(plaintext),
			IPv4: item.Credential.IPv4, IPv6: item.Credential.IPv6,
		})
		for i := range plaintext {
			plaintext[i] = 0
		}
	}
	port := 53147
	if _, portValue, splitErr := net.SplitHostPort(node.Endpoint); splitErr == nil {
		if parsed, parseErr := strconv.Atoi(portValue); parseErr == nil {
			port = parsed
		}
	}
	interfaceIPv4, err := interfaceAddress(region.IPv4Network)
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	interfaceIPv6, err := interfaceAddress(region.IPv6Network)
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	state := model.NodeDesiredState{
		NodeID: node.ID, RegionCode: node.RegionCode,
		InterfaceIPv4: interfaceIPv4, InterfaceIPv6: interfaceIPv6,
		ExitMode: region.ExitMode, ListenPort: port, Peers: peers,
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	digest := sha256.Sum256(encoded)
	state.Version = int64(binary.BigEndian.Uint64(digest[:8]) & ((1 << 63) - 1))
	if state.Version == 0 {
		state.Version = 1
	}
	return state, nil
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
			Version: 2, Type: "tnest-vpn-enrollment",
			ManagementURL: s.cfg.ManagementURL, Token: value,
			ExpiresAt: rotation.ExpiresAt, DeviceName: device.Name,
			CatalogSigningKey: s.CatalogPublicKey(),
			Purpose:           "migrate",
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
	if err := s.store.UpsertDeviceRegion(ctx, model.DeviceRegionCredential{
		DeviceID: device.ID, RegionCode: "SG", IPv4: device.IPv4,
		IPv6: device.IPv6, PublicKey: device.PublicKey,
		Status: model.RegionStatusActive,
	}, sealed, now); err != nil {
		_ = s.wg.Apply(ctx, oldPeers)
		return StandardResult{}, err
	}
	return StandardResult{
		Device: device,
		Config: s.renderClient(device, privateKey.String(), psk.String()),
	}, nil
}

type appleRegionMaterial struct {
	region        model.Region
	node          model.Node
	credential    model.DeviceRegionCredential
	privateKey    string
	presharedKey  string
	sealedPSK     []byte
	sealedPrivate []byte
}

func (s *Service) CreateAppleBundle(
	ctx context.Context,
	request CreateRequest,
) (AppleBundleResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, platform, err := validateCreate(request)
	if err != nil {
		return AppleBundleResult{}, err
	}
	if platform != "ios" && platform != "macos" {
		return AppleBundleResult{}, errors.New("Apple bundle requires ios or macos platform")
	}
	now := s.now().UTC()
	slot, err := s.store.NextSlot(ctx, now)
	if err != nil {
		return AppleBundleResult{}, err
	}
	device := newDevice(name, platform, slot, model.StatusActive, now)
	materials, err := s.newAppleMaterials(ctx, device, slot)
	if err != nil {
		return AppleBundleResult{}, err
	}
	var singapore appleRegionMaterial
	for _, material := range materials {
		if material.region.Code == "SG" {
			singapore = material
			break
		}
	}
	if singapore.region.Code == "" {
		return AppleBundleResult{}, errors.New("Singapore region is not ready")
	}
	device.PublicKey = singapore.credential.PublicKey
	oldPeers, err := s.peerMaterials(ctx, "")
	if err != nil {
		return AppleBundleResult{}, err
	}
	candidate := append(append([]PeerMaterial(nil), oldPeers...), PeerMaterial{
		Device: device, PresharedKey: singapore.presharedKey,
	})
	if err := s.wg.Apply(ctx, candidate); err != nil {
		return AppleBundleResult{}, err
	}
	if err := s.store.CreateDevice(ctx, device, slot, singapore.sealedPSK); err != nil {
		_ = s.wg.Apply(ctx, oldPeers)
		return AppleBundleResult{}, err
	}
	for _, material := range materials {
		if err := s.store.UpsertDeviceRegion(
			ctx, material.credential, material.sealedPSK, now,
		); err != nil {
			_ = s.store.DeleteDevice(ctx, device.ID)
			_ = s.wg.Apply(ctx, oldPeers)
			return AppleBundleResult{}, err
		}
		if err := s.store.SetDeviceRegionPrivateKey(
			ctx, device.ID, material.region.Code, material.sealedPrivate,
		); err != nil {
			_ = s.store.DeleteDevice(ctx, device.ID)
			_ = s.wg.Apply(ctx, oldPeers)
			return AppleBundleResult{}, err
		}
	}
	bundle, err := renderAppleZIP(materials)
	if err != nil {
		return AppleBundleResult{}, err
	}
	for index := range materials {
		materials[index].privateKey = ""
		materials[index].presharedKey = ""
	}
	return AppleBundleResult{Device: device, Bundle: bundle}, nil
}

func (s *Service) AppleBundle(
	ctx context.Context,
	deviceID string,
) (AppleBundleResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, _, slot, err := s.store.Device(ctx, deviceID)
	if err != nil {
		return AppleBundleResult{}, err
	}
	if device.Status != model.StatusActive ||
		(device.Platform != "ios" && device.Platform != "macos") {
		return AppleBundleResult{}, errors.New("Apple bundle requires an active Apple device")
	}
	regions, err := s.store.Regions(ctx, false)
	if err != nil {
		return AppleBundleResult{}, err
	}
	existing, err := s.store.DeviceRegions(ctx, device.ID)
	if err != nil {
		return AppleBundleResult{}, err
	}
	byRegion := make(map[string]model.DeviceRegionCredential, len(existing))
	for _, credential := range existing {
		byRegion[credential.RegionCode] = credential
	}
	materials := make([]appleRegionMaterial, 0, len(regions))
	for _, region := range regions {
		nodes, err := s.store.Nodes(ctx, region.Code, false)
		if err != nil {
			return AppleBundleResult{}, err
		}
		node, ok := firstReadyNode(nodes)
		if !ok {
			continue
		}
		credential, found := byRegion[region.Code]
		if !found {
			material, err := s.newAppleMaterial(device, slot, region, node)
			if err != nil {
				return AppleBundleResult{}, err
			}
			if err := s.store.UpsertDeviceRegion(
				ctx, material.credential, material.sealedPSK, s.now().UTC(),
			); err != nil {
				return AppleBundleResult{}, err
			}
			if err := s.store.SetDeviceRegionPrivateKey(
				ctx, device.ID, region.Code, material.sealedPrivate,
			); err != nil {
				return AppleBundleResult{}, err
			}
			materials = append(materials, material)
			continue
		}
		sealedPSK, sealedPrivate, err := s.store.DeviceRegionSecrets(
			ctx, device.ID, region.Code,
		)
		if err != nil {
			return AppleBundleResult{}, err
		}
		if len(sealedPrivate) == 0 {
			return AppleBundleResult{}, errors.New(
				"legacy Apple device has no exportable private key; create a new Apple device",
			)
		}
		psk, err := s.sealer.Open(sealedPSK, pskContext(region.Code))
		if err != nil {
			return AppleBundleResult{}, errors.New("cannot decrypt Apple region credential")
		}
		privateKey, err := s.sealer.Open(
			sealedPrivate, "device-region-private:"+region.Code,
		)
		if err != nil {
			clearBytes(psk)
			return AppleBundleResult{}, errors.New("cannot decrypt Apple private key")
		}
		materials = append(materials, appleRegionMaterial{
			region: region, node: node, credential: credential,
			privateKey: string(privateKey), presharedKey: string(psk),
		})
		clearBytes(psk)
		clearBytes(privateKey)
	}
	if len(materials) == 0 {
		return AppleBundleResult{}, errors.New("no Apple regions are ready")
	}
	bundle, err := renderAppleZIP(materials)
	if err != nil {
		return AppleBundleResult{}, err
	}
	for index := range materials {
		materials[index].privateKey = ""
		materials[index].presharedKey = ""
	}
	return AppleBundleResult{Device: device, Bundle: bundle}, nil
}

func (s *Service) newAppleMaterials(
	ctx context.Context,
	device model.Device,
	slot int,
) ([]appleRegionMaterial, error) {
	regions, err := s.store.Regions(ctx, false)
	if err != nil {
		return nil, err
	}
	result := make([]appleRegionMaterial, 0, len(regions))
	for _, region := range regions {
		nodes, err := s.store.Nodes(ctx, region.Code, false)
		if err != nil {
			return nil, err
		}
		node, ok := firstReadyNode(nodes)
		if !ok {
			continue
		}
		value, err := s.newAppleMaterial(device, slot, region, node)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Service) newAppleMaterial(
	device model.Device,
	slot int,
	region model.Region,
	node model.Node,
) (appleRegionMaterial, error) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return appleRegionMaterial{}, err
	}
	psk, err := wgtypes.GenerateKey()
	if err != nil {
		return appleRegionMaterial{}, err
	}
	ipv4, err := addressAt(region.IPv4Network, slot)
	if err != nil {
		return appleRegionMaterial{}, err
	}
	ipv6, err := addressAt(region.IPv6Network, slot)
	if err != nil {
		return appleRegionMaterial{}, err
	}
	sealedPSK, err := s.sealer.Seal([]byte(psk.String()), pskContext(region.Code))
	if err != nil {
		return appleRegionMaterial{}, err
	}
	sealedPrivate, err := s.sealer.Seal(
		[]byte(privateKey.String()), "device-region-private:"+region.Code,
	)
	if err != nil {
		return appleRegionMaterial{}, err
	}
	return appleRegionMaterial{
		region: region, node: node,
		credential: model.DeviceRegionCredential{
			DeviceID: device.ID, RegionCode: region.Code,
			IPv4: ipv4, IPv6: ipv6, PublicKey: privateKey.PublicKey().String(),
			Status: model.RegionStatusActive,
		},
		privateKey: privateKey.String(), presharedKey: psk.String(),
		sealedPSK: sealedPSK, sealedPrivate: sealedPrivate,
	}, nil
}

func firstReadyNode(nodes []model.Node) (model.Node, bool) {
	for _, node := range nodes {
		if node.Enabled && node.Endpoint != "" && node.ServerPublicKey != "" {
			return node, true
		}
	}
	return model.Node{}, false
}

func renderAppleZIP(materials []appleRegionMaterial) ([]byte, error) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, material := range materials {
		name := appleTunnelName(material.region) + ".conf"
		file, err := writer.CreateHeader(&zip.FileHeader{
			Name: name, Method: zip.Deflate,
		})
		if err != nil {
			return nil, err
		}
		dns := strings.Join(material.region.DNS, ", ")
		config := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32, %s/128
DNS = %s
MTU = %d

[Peer]
PublicKey = %s
PresharedKey = %s
Endpoint = %s
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`, material.privateKey, material.credential.IPv4, material.credential.IPv6,
			dns, material.region.MTU, material.node.ServerPublicKey,
			material.presharedKey, material.node.Endpoint)
		if _, err := io.WriteString(file, config); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appleTunnelName(region model.Region) string {
	switch region.Code {
	case "SG":
		return "TNest-SG-Singapore"
	case "MY":
		return "TNest-MY-Kuala-Lumpur"
	}
	var safe strings.Builder
	for _, r := range region.DisplayName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') {
			safe.WriteRune(r)
		} else if safe.Len() > 0 {
			safe.WriteByte('-')
		}
	}
	return "TNest-" + region.Code + "-" + strings.Trim(safe.String(), "-")
}

func pskContext(regionCode string) string {
	if regionCode == "SG" {
		return "device-psk"
	}
	return "device-region-psk:" + regionCode
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
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
	if err := s.store.UpsertDeviceRegion(ctx, model.DeviceRegionCredential{
		DeviceID: device.ID, RegionCode: "SG", IPv4: device.IPv4,
		IPv6: device.IPv6, PublicKey: device.PublicKey,
		Status: model.RegionStatusActive,
	}, sealed, s.now().UTC()); err != nil {
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
	if subtle.ConstantTimeCompare(
		[]byte(device.PublicKey), []byte(request.PublicKey),
	) != 1 {
		return model.Device{}, errors.New("migration must preserve the Singapore public key")
	}
	_, currentSealed, _, err := s.store.Device(ctx, device.ID)
	if err != nil {
		return model.Device{}, err
	}
	currentPSK, err := s.sealer.Open(currentSealed, "device-psk")
	if err != nil {
		return model.Device{}, err
	}
	defer func() {
		for index := range currentPSK {
			currentPSK[index] = 0
		}
	}()
	if subtle.ConstantTimeCompare(currentPSK, []byte(request.PresharedKey)) != 1 {
		return model.Device{}, errors.New("migration must preserve the Singapore preshared key")
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
	if err := s.store.UpsertDeviceRegion(ctx, model.DeviceRegionCredential{
		DeviceID: rotated.ID, RegionCode: "SG", IPv4: rotated.IPv4,
		IPv6: rotated.IPv6, PublicKey: rotated.PublicKey,
		Status: model.RegionStatusActive,
	}, sealed, now); err != nil {
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
	if err := s.store.RevokeDeviceRegions(ctx, id, now); err != nil {
		return err
	}
	if err := s.store.RevokeDeviceTokens(ctx, id, now); err != nil {
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

func addressAt(network string, slot int) (string, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(network))
	if err != nil || slot < 1 {
		return "", fmt.Errorf("invalid region network %q", network)
	}
	if prefix.Addr().Is6() {
		base := prefix.Masked().Addr().String()
		candidate, parseErr := netip.ParseAddr(base + strconv.Itoa(slot))
		if parseErr != nil || !prefix.Contains(candidate) {
			return "", fmt.Errorf("region network %q does not contain slot %d", network, slot)
		}
		return candidate.String(), nil
	}
	address := prefix.Masked().Addr()
	for index := 0; index < slot; index++ {
		address = address.Next()
		if !address.IsValid() || !prefix.Contains(address) {
			return "", fmt.Errorf("region network %q does not contain slot %d", network, slot)
		}
	}
	return address.String(), nil
}

func interfaceAddress(network string) (string, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(network))
	if err != nil {
		return "", fmt.Errorf("invalid region network %q", network)
	}
	address := prefix.Masked().Addr().Next()
	if !address.IsValid() || !prefix.Contains(address) {
		return "", fmt.Errorf("region network %q has no server address", network)
	}
	return fmt.Sprintf("%s/%d", address, prefix.Bits()), nil
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
