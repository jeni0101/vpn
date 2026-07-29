package manager

import (
	"context"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

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
	rotatedPrivate, _ := wgtypes.GeneratePrivateKey()
	rotatedPSK, _ := wgtypes.GenerateKey()
	rotated, err := service.Claim(ctx, ClaimRequest{
		Token: rotation.Invite.Token, PublicKey: rotatedPrivate.PublicKey().String(),
		PresharedKey: rotatedPSK.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rotated.PublicKey != rotatedPrivate.PublicKey().String() || len(wireguard.peers) != 2 {
		t.Fatal("rotation did not replace exactly one peer")
	}
	if _, err := service.Claim(ctx, ClaimRequest{
		Token: rotation.Invite.Token, PublicKey: rotatedPrivate.PublicKey().String(),
		PresharedKey: rotatedPSK.String(),
	}); err == nil {
		t.Fatal("rotation token was accepted twice")
	}
	if err := service.Revoke(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if len(wireguard.peers) != 1 {
		t.Fatalf("revoked peer remains applied: %d", len(wireguard.peers))
	}
}
