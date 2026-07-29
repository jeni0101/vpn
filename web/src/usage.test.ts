import { describe, expect, it } from "vitest";
import type { Device, UsagePoint } from "./types";
import { buildUsageBuckets, calculateDeviceRates, mergeDeviceRates } from "./usage";

describe("real usage buckets", () => {
  it("fills 24 hours and keeps a single sample in its actual bucket", () => {
    const now = new Date("2026-07-29T03:15:00Z");
    const points: UsagePoint[] = [{
      device_id: "ios",
      bucket: "2026-07-29T02:00:00Z",
      upload_bytes: 4_000,
      download_bytes: 9_000,
    }];
    const buckets = buildUsageBuckets(points, "24h", now);
    expect(buckets).toHaveLength(24);
    expect(buckets.filter((item) => item.upload + item.download > 0)).toEqual([
      expect.objectContaining({ key: "2026-07-29T02:00:00.000Z", upload: 4_000, download: 9_000 }),
    ]);
  });

  it("normalizes missing and invalid points to zero instead of inventing data", () => {
    expect(buildUsageBuckets(null, "7d", new Date("2026-07-29T03:15:00Z")))
      .toHaveLength(7);
    expect(buildUsageBuckets([{
      device_id: "ios",
      bucket: "invalid",
      upload_bytes: Number.NaN,
      download_bytes: -10,
    }], "30d", new Date("2026-07-29T03:15:00Z")).every(
      (item) => item.upload === 0 && item.download === 0,
    )).toBe(true);
  });
});

describe("sample rates", () => {
  const base: Device = {
    id: "ios",
    name: "ios",
    platform: "ios",
    ipv4: "10.66.0.12",
    ipv6: "fd66:66:66::12",
    public_key: "public",
    status: "active",
    external_private_key: true,
    created_at: "2026-07-29T00:00:00Z",
    stats_updated_at: "2026-07-29T03:00:00Z",
    upload_bytes: 1_000,
    download_bytes: 2_000,
  };

  it("uses WireGuard counter deltas over the actual sample interval", () => {
    const rates = calculateDeviceRates([base], [{
      ...base,
      stats_updated_at: "2026-07-29T03:00:30Z",
      upload_bytes: 1_600,
      download_bytes: 2_900,
    }]);
    expect(rates.ios).toEqual({ uploadBps: 20, downloadBps: 30 });
  });

  it("does not emit negative rates after a counter reset", () => {
    const rates = calculateDeviceRates([base], [{
      ...base,
      stats_updated_at: "2026-07-29T03:00:30Z",
      upload_bytes: 10,
      download_bytes: 20,
    }]);
    expect(rates.ios).toEqual({ uploadBps: 0, downloadBps: 0 });
  });

  it("retains the last rate while SSE repeats the same 30-second sample", () => {
    const existing = { ios: { uploadBps: 20, downloadBps: 30 } };
    expect(mergeDeviceRates(existing, [base], [{ ...base }])).toEqual(existing);
  });
});
