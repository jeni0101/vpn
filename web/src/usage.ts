import type { Device, UsagePoint } from "./types";

export type UsageRange = "24h" | "7d" | "30d";

export interface UsageQuery {
  range: UsageRange;
  deviceId?: string;
  now?: Date;
}

export interface UsageWindow {
  bucket: "hour" | "day";
  from: Date;
  to: Date;
  count: number;
}

export interface ChartBucket {
  key: string;
  label: string;
  upload: number;
  download: number;
}

export interface DeviceRate {
  uploadBps: number;
  downloadBps: number;
}

const shanghaiHour = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  hour: "2-digit",
  hour12: false,
});

const shanghaiDay = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  month: "2-digit",
  day: "2-digit",
});

export function usageWindow(range: UsageRange, now = new Date()): UsageWindow {
  const current = new Date(now);
  if (range === "24h") {
    const end = floorUTC(current, "hour");
    end.setUTCHours(end.getUTCHours() + 1);
    const start = new Date(end);
    start.setUTCHours(start.getUTCHours() - 24);
    return { bucket: "hour", from: start, to: end, count: 24 };
  }
  const count = range === "7d" ? 7 : 30;
  const end = floorUTC(current, "day");
  end.setUTCDate(end.getUTCDate() + 1);
  const start = new Date(end);
  start.setUTCDate(start.getUTCDate() - count);
  return { bucket: "day", from: start, to: end, count };
}

export function buildUsageBuckets(
  points: UsagePoint[] | null | undefined,
  range: UsageRange,
  now = new Date(),
): ChartBucket[] {
  const window = usageWindow(range, now);
  const totals = new Map<string, { upload: number; download: number }>();
  for (const point of Array.isArray(points) ? points : []) {
    const date = new Date(point.bucket);
    if (Number.isNaN(date.getTime())) continue;
    const key = floorUTC(date, window.bucket).toISOString();
    const current = totals.get(key) ?? { upload: 0, download: 0 };
    current.upload += safeBytes(point.upload_bytes);
    current.download += safeBytes(point.download_bytes);
    totals.set(key, current);
  }
  return Array.from({ length: window.count }, (_, index) => {
    const date = new Date(window.from);
    if (window.bucket === "hour") {
      date.setUTCHours(date.getUTCHours() + index);
    } else {
      date.setUTCDate(date.getUTCDate() + index);
    }
    const key = date.toISOString();
    const value = totals.get(key) ?? { upload: 0, download: 0 };
    return {
      key,
      label: window.bucket === "hour" ? shanghaiHour.format(date) : shanghaiDay.format(date),
      upload: value.upload,
      download: value.download,
    };
  });
}

export function calculateDeviceRates(
  previous: Device[] | null | undefined,
  current: Device[] | null | undefined,
): Record<string, DeviceRate> {
  const result: Record<string, DeviceRate> = {};
  const prior = new Map((Array.isArray(previous) ? previous : []).map((device) => [device.id, device]));
  for (const device of Array.isArray(current) ? current : []) {
    const before = prior.get(device.id);
    const beforeAt = before?.stats_updated_at ? new Date(before.stats_updated_at).getTime() : Number.NaN;
    const currentAt = device.stats_updated_at ? new Date(device.stats_updated_at).getTime() : Number.NaN;
    const elapsed = (currentAt - beforeAt) / 1000;
    if (!before || !Number.isFinite(elapsed) || elapsed <= 0) continue;
    result[device.id] = {
      uploadBps: Math.max(0, safeBytes(device.upload_bytes) - safeBytes(before.upload_bytes)) / elapsed,
      downloadBps: Math.max(0, safeBytes(device.download_bytes) - safeBytes(before.download_bytes)) / elapsed,
    };
  }
  return result;
}

export function mergeDeviceRates(
  existing: Record<string, DeviceRate>,
  previous: Device[] | null | undefined,
  current: Device[] | null | undefined,
): Record<string, DeviceRate> {
  const before = new Map((Array.isArray(previous) ? previous : []).map((device) => [device.id, device]));
  const calculated = calculateDeviceRates(previous, current);
  const next: Record<string, DeviceRate> = {};
  for (const device of Array.isArray(current) ? current : []) {
    const prior = before.get(device.id);
    if (!prior) {
      next[device.id] = { uploadBps: 0, downloadBps: 0 };
      continue;
    }
    if (device.stats_updated_at === prior.stats_updated_at) {
      next[device.id] = existing[device.id] ?? { uploadBps: 0, downloadBps: 0 };
      continue;
    }
    next[device.id] = calculated[device.id] ?? { uploadBps: 0, downloadBps: 0 };
  }
  return next;
}

export function safeBytes(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : 0;
}

function floorUTC(date: Date, bucket: "hour" | "day"): Date {
  const value = new Date(date);
  value.setUTCMinutes(0, 0, 0);
  if (bucket === "day") value.setUTCHours(0);
  return value;
}
