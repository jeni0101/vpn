package manager

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/jeni0101/vpn/server/internal/catalog"
	"github.com/jeni0101/vpn/server/internal/config"
	"github.com/jeni0101/vpn/server/internal/model"
	"github.com/jeni0101/vpn/server/internal/security"
	"github.com/jeni0101/vpn/server/internal/store"
)

type fakeWG struct {
	peers []PeerMaterial
	err   error
}

func (f *fakeWG) Stats(context.Context) (map[string]model.LiveStats, error) {
	return map[string]model.LiveStats{}, nil
}

func (f *fakeWG) Apply(_ context.Context, peers []PeerMaterial) error {
	if f.err != nil {
		return f.err
	}
	f.peers = append([]PeerMaterial(nil), peers...)
	return nil
}

func TestInviteClaimStandardAndRevoke(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir() + "/manager.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sealer, _ := security.NewSealer(make([]byte, 32))
	serverKey, _ := wgtypes.GeneratePrivateKey()
	cfg := config.Manager{
		ManagementURL: "https://vpn.example.com",
		Endpoint:      "203.0.113.10:51999", ServerPublicKey: serverKey.PublicKey().String(),
		InviteTTL: 10 * time.Minute, Quarantine: 7 * 24 * time.Hour,
	}
	wireguard := &fakeWG{}
	service := NewService(cfg, db, sealer, wireguard)
	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	invite, err := service.CreateInvite(ctx, CreateRequest{Name: "phone", Platform: "android"})
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate, _ := wgtypes.GeneratePrivateKey()
	psk, _ := wgtypes.GenerateKey()
	claimed, err := service.Claim(ctx, ClaimRequest{
		Token: invite.Invite.Token, PublicKey: clientPrivate.PublicKey().String(),
		PresharedKey: psk.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != model.StatusActive || len(wireguard.peers) != 1 {
		t.Fatalf("claim did not activate peer: %#v", claimed)
	}
	if _, err := service.Claim(ctx, ClaimRequest{
		Token: invite.Invite.Token, PublicKey: clientPrivate.PublicKey().String(),
		PresharedKey: psk.String(),
	}); err == nil {
		t.Fatal("enrollment token was accepted twice")
	}
	standard, err := service.CreateStandard(ctx, CreateRequest{Name: "laptop", Platform: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	if standard.Config == "" || len(wireguard.peers) != 2 {
		t.Fatal("standard config was not generated")
	}
	rotation, err := service.CreateRotationInvite(ctx, standard.Device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotation.Invite.Version != 2 || rotation.Invite.Purpose != "migrate" {
		t.Fatalf("unexpected migration invite: %#v", rotation.Invite)
	}
	migrationPSK := wireguard.peers[1].PresharedKey
	rotated, err := service.Claim(ctx, ClaimRequest{
		Token: rotation.Invite.Token, PublicKey: standard.Device.PublicKey,
		PresharedKey: migrationPSK,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rotated.PublicKey != standard.Device.PublicKey || len(wireguard.peers) != 2 {
		t.Fatal("migration changed the existing Singapore peer")
	}
	if _, err := service.Claim(ctx, ClaimRequest{
		Token: rotation.Invite.Token, PublicKey: standard.Device.PublicKey,
		PresharedKey: migrationPSK,
	}); err == nil {
		t.Fatal("migration token was accepted twice")
	}
	if err := service.Revoke(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if len(wireguard.peers) != 1 {
		t.Fatalf("revoked peer remains applied: %d", len(wireguard.peers))
	}
}

func TestV2RegionsDesiredStateAndAppleBundle(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir() + "/manager.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sealer, _ := security.NewSealer(make([]byte, 32))
	sgServer, _ := wgtypes.GeneratePrivateKey()
	myServer, _ := wgtypes.GeneratePrivateKey()
	cfg := config.Manager{
		ManagementURL:   "https://vpn.example.com",
		Endpoint:        "203.0.113.10:53147",
		ServerPublicKey: sgServer.PublicKey().String(),
		InviteTTL:       10 * time.Minute,
		Quarantine:      7 * 24 * time.Hour,
	}
	wireguard := &fakeWG{}
	service := NewService(cfg, db, sealer, wireguard)
	_, signingPrivate, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := catalog.NewSigner(signingPrivate)
	service.SetCatalogSigner(signer)
	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	sgNodes, _ := service.Nodes(ctx, "SG", true)
	sgNode := sgNodes[0]
	sgNode.Endpoint = cfg.Endpoint
	sgNode.ProbeURL = "https://vpn.example.com/latency"
	sgNode.ServerPublicKey = cfg.ServerPublicKey
	sgNode.Enabled = true
	if err := service.UpsertNode(ctx, sgNode); err != nil {
		t.Fatal(err)
	}
	my, err := db.Region(ctx, "MY", false)
	if err != nil {
		t.Fatal(err)
	}
	my.Enabled = true
	if err := service.UpsertRegion(ctx, my); err != nil {
		t.Fatal(err)
	}
	myNodes, _ := service.Nodes(ctx, "MY", true)
	myNode := myNodes[0]
	// Shared public IP products may translate a random public port to the
	// node's fixed internal WireGuard port.
	myNode.Endpoint = "203.0.113.20:10067"
	myNode.ServerPublicKey = myServer.PublicKey().String()
	myNode.Enabled = true
	if err := service.UpsertNode(ctx, myNode); err != nil {
		t.Fatal(err)
	}

	invite, err := service.CreateInvite(
		ctx, CreateRequest{Name: "v2-phone", Platform: "android"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if invite.Invite.Version != 2 ||
		invite.Invite.CatalogSigningKey != signer.PublicKey() {
		t.Fatalf("unexpected v2 invite: %#v", invite.Invite)
	}
	sgClient, _ := wgtypes.GeneratePrivateKey()
	sgPSK, _ := wgtypes.GenerateKey()
	result, err := service.ClaimV2(ctx, ClaimV2Request{
		Token: invite.Invite.Token,
		Credentials: []RegionKeyRequest{{
			RegionCode: "SG", PublicKey: sgClient.PublicKey().String(),
			PresharedKey: sgPSK.String(),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeviceToken == "" || len(result.Regions) != 1 ||
		result.Regions[0].RegionCode != "SG" {
		t.Fatalf("unexpected claim result: %#v", result)
	}
	authenticated, err := service.AuthenticateDevice(ctx, result.DeviceToken)
	if err != nil || authenticated.ID != result.Device.ID {
		t.Fatalf("device token was not scoped to claimed device: %#v %v", authenticated, err)
	}

	myClient, _ := wgtypes.GeneratePrivateKey()
	myPSK, _ := wgtypes.GenerateKey()
	credential, err := service.EnrollRegion(ctx, authenticated, RegionKeyRequest{
		RegionCode: "MY", PublicKey: myClient.PublicKey().String(),
		PresharedKey: myPSK.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.IPv4 != "10.67.0.14" || credential.IPv6 != "fd67:67:67::14" {
		t.Fatalf("unexpected Kuala Lumpur addresses: %#v", credential)
	}
	desired, err := service.DesiredState(ctx, "my-kul-01")
	if err != nil {
		t.Fatal(err)
	}
	if desired.ExitMode != model.ExitModeIPv4BlockIPv6 ||
		desired.ListenPort != 53147 ||
		len(desired.Peers) != 1 ||
		desired.Peers[0].PublicKey != myClient.PublicKey().String() {
		t.Fatalf("unexpected Kuala Lumpur desired state: %#v", desired)
	}
	for index, counters := range [][2]int64{{100, 200}, {160, 290}} {
		if err := service.SaveNodeReport(ctx, model.NodeReport{
			NodeID: "my-kul-01", Version: "1", Healthy: true, PeerCount: 1,
			ReportedAt:    now.Add(time.Duration(index) * 5 * time.Minute),
			UsageSequence: int64(index + 1),
			Usage: []model.NodeUsageCounter{{
				DeviceID: result.Device.ID, PublicKey: myClient.PublicKey().String(),
				ReceiveBytes: counters[0], TransmitBytes: counters[1],
				LastHandshake: now,
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	usage, bucket, err := service.ClientUsage(
		ctx, result.Device.ID, "24h", "MY",
	)
	if err != nil {
		t.Fatal(err)
	}
	if bucket != "hour" || len(usage) != 1 ||
		usage[0].RegionCode != "MY" || usage[0].NodeID != "my-kul-01" ||
		usage[0].UploadBytes != 60 || usage[0].DownloadBytes != 90 {
		t.Fatalf("unexpected regional usage: %#v bucket=%s", usage, bucket)
	}

	apple, err := service.CreateAppleBundle(
		ctx, CreateRequest{Name: "iphone", Platform: "ios"},
	)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(apple.Bundle), int64(len(apple.Bundle)))
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, file := range archive.File {
		names[file.Name] = true
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(io.LimitReader(reader, 16*1024))
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "AllowedIPs = 0.0.0.0/0, ::/0") {
			t.Fatalf("%s does not enforce full tunnel", file.Name)
		}
	}
	if !names["TNest-SG-Singapore.conf"] ||
		!names["TNest-MY-Kuala-Lumpur.conf"] {
		t.Fatalf("unexpected Apple bundle entries: %#v", names)
	}

	if err := service.Revoke(ctx, result.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateDevice(ctx, result.DeviceToken); err == nil {
		t.Fatal("revoked device token remains valid")
	}
	desired, err = service.DesiredState(ctx, "my-kul-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Peers) != 1 || desired.Peers[0].DeviceID != apple.Device.ID {
		t.Fatalf("revoked peer remains in desired state: %#v", desired.Peers)
	}
}
