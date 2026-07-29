package store

import (
	"context"
	"testing"
	"time"

	"github.com/jeni0101/vpn/server/internal/model"
)

func TestMultiRegionMigrationAndCatalog(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/manager.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	regions, err := db.Regions(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 || regions[0].Code != "SG" || regions[1].Code != "MY" {
		t.Fatalf("unexpected regions: %#v", regions)
	}
	if regions[1].Enabled || regions[1].ExitMode != model.ExitModeIPv4BlockIPv6 {
		t.Fatalf("Malaysia must start disabled in IPv4-safe mode: %#v", regions[1])
	}
	if regions[1].IPv4Network != "10.67.0.0/24" ||
		regions[1].IPv6Network != "fd67:67:67::/64" {
		t.Fatalf("unexpected Malaysia networks: %#v", regions[1])
	}

	nodes, err := db.Nodes(ctx, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	var sg model.Node
	var my model.Node
	for _, node := range nodes {
		switch node.ID {
		case "sg-sin-01":
			sg = node
		case "my-kul-01":
			my = node
		}
	}
	if sg.ID == "" || my.ID == "" || my.Enabled {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	sg.Endpoint = "203.0.113.10:53147"
	sg.ServerPublicKey = "server-public-key"
	sg.Health = model.NodeHealthHealthy
	if err := db.UpsertNode(ctx, sg); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	catalog, err := db.Catalog(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Regions) != 1 || catalog.Regions[0].Code != "SG" ||
		len(catalog.Regions[0].Nodes) != 1 {
		t.Fatalf("catalog must expose only ready enabled nodes: %#v", catalog)
	}
}

func TestExistingDeviceBackfillsSingaporeCredential(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/manager.db"
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	device := model.Device{
		ID: "existing", Name: "Existing", Platform: "windows",
		IPv4: "10.66.0.14", IPv6: "fd66:66:66::14",
		PublicKey: "existing-public-key", Status: model.StatusActive,
		CreatedAt: now,
	}
	if err := db.CreateDevice(ctx, device, 14, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	credentials, err := db.DeviceRegions(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].RegionCode != "SG" ||
		credentials[0].PublicKey != device.PublicKey {
		t.Fatalf("device was not backfilled: %#v", credentials)
	}
}
