package catalog

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/jeni0101/vpn/server/internal/model"
)

func TestSignedCatalogRejectsTampering(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner(private)
	if err != nil {
		t.Fatal(err)
	}
	value := model.Catalog{
		Version: 1, IssuedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		Regions: []model.CatalogRegion{{
			Code: "SG", DisplayName: "Singapore", ExitMode: model.ExitModeDualStack,
			Nodes: []model.CatalogNode{{
				ID: "sg-sin-01", Endpoint: "203.0.113.10:53147",
				ProbeURL: "https://vpn.example.com/latency",
				ServerPublicKey: "public", Priority: 10,
			}},
		}},
	}
	if err := signer.Sign(&value); err != nil {
		t.Fatal(err)
	}
	if !Verify(value, signer.PublicKey()) {
		t.Fatal("valid catalog signature was rejected")
	}
	value.Regions[0].DisplayName = "tampered"
	if Verify(value, signer.PublicKey()) {
		t.Fatal("tampered catalog signature was accepted")
	}
}
