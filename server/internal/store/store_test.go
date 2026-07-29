package store

import (
	"context"
	"testing"
	"time"

	"github.com/jeni0101/vpn/server/internal/model"
)

func TestSlotAllocationAndUsageReset(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/manager.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	slot, err := db.NextSlot(ctx, now)
	if err != nil || slot != 14 {
		t.Fatalf("first slot=%d err=%v", slot, err)
	}
	device := model.Device{
		ID: "device-1", Name: "test", Platform: "windows", IPv4: "10.66.0.14",
		IPv6: "fd66:66:66::14", PublicKey: "public", Status: model.StatusActive,
		CreatedAt: now,
	}
	if err := db.CreateDevice(ctx, device, 14, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	slot, _ = db.NextSlot(ctx, now)
	if slot != 15 {
		t.Fatalf("second slot=%d", slot)
	}
	if err := db.RecordStats(ctx, device.ID, 100, 200, time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordStats(ctx, device.ID, 160, 280, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordStats(ctx, device.ID, 10, 20, now, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	points, err := db.Usage(ctx, device.ID, "hour", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].UploadBytes != 70 || points[0].DownloadBytes != 100 {
		t.Fatalf("unexpected usage: %#v", points)
	}
	devices, err := db.ListDevices(ctx)
	if err != nil || len(devices) != 1 || devices[0].StatsUpdatedAt == nil {
		t.Fatalf("missing stats sample time: devices=%#v err=%v", devices, err)
	}
	if !devices[0].StatsUpdatedAt.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("stats sample time=%v", devices[0].StatsUpdatedAt)
	}
}

func TestEmptyCollectionsEncodeAsArrays(t *testing.T) {
	ctx := context.Background()
	db, err := Open(t.TempDir() + "/manager.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	points, err := db.Usage(ctx, "", "hour", time.Now().Add(-time.Hour), time.Now())
	if err != nil || points == nil || len(points) != 0 {
		t.Fatalf("empty usage must be a non-nil slice: %#v err=%v", points, err)
	}
	events, err := db.Audit(ctx, 100)
	if err != nil || events == nil || len(events) != 0 {
		t.Fatalf("empty audit must be a non-nil slice: %#v err=%v", events, err)
	}
}
