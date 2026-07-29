import type { AuditEvent, Device, Region, UsagePoint, VPNNode } from "./types";
import { usageWindow, type UsageQuery } from "./usage";

let csrfToken = sessionStorage.getItem("tnest_csrf") ?? "";

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body) headers.set("Content-Type", "application/json");
  if (init.method && init.method !== "GET") headers.set("X-CSRF-Token", csrfToken);
  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "same-origin",
    cache: "no-store",
  });
  if (response.status === 204) return undefined as T;
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || `请求失败 (${response.status})`);
  }
  return payload as T;
}

export const api = {
  async session() {
    const data = await request<{ username: string; csrf_token: string }>("/api/v1/auth/session");
    csrfToken = data.csrf_token;
    sessionStorage.setItem("tnest_csrf", csrfToken);
    return data;
  },
  login(username: string, password: string) {
    return request<{ next: string }>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
  },
  async totp(code: string) {
    const data = await request<{ username: string; csrf_token: string }>("/api/v1/auth/totp", {
      method: "POST",
      body: JSON.stringify({ code }),
    });
    csrfToken = data.csrf_token;
    sessionStorage.setItem("tnest_csrf", csrfToken);
    return data;
  },
  async logout() {
    await request<void>("/api/v1/auth/logout", { method: "POST", body: "{}" });
    csrfToken = "";
    sessionStorage.removeItem("tnest_csrf");
  },
  async devices() {
    const payload = await request<{ devices?: Device[] | null }>("/api/v1/devices");
    return Array.isArray(payload.devices) ? payload.devices : [];
  },
  createDevice(name: string, platform: string, mode: string, totp: string) {
    return request<{ device: Device; download_url: string; expires_at: string }>(
      "/api/v1/devices",
      {
        method: "POST",
        headers: { "X-TNest-TOTP": totp },
        body: JSON.stringify({ name, platform, mode }),
      },
    );
  },
  revoke(id: string, totp: string) {
    return request<void>(`/api/v1/devices/${encodeURIComponent(id)}/revoke`, {
      method: "POST",
      headers: { "X-TNest-TOTP": totp },
      body: "{}",
    });
  },
  rotate(id: string, totp: string) {
    return request<{ device: Device; download_url: string; expires_at: string }>(
      `/api/v1/devices/${encodeURIComponent(id)}/rotate`,
      {
        method: "POST",
        headers: { "X-TNest-TOTP": totp },
        body: "{}",
      },
    );
  },
  async usage(options: UsageQuery = { range: "24h" }) {
    const window = usageWindow(options.range, options.now);
    const search = new URLSearchParams({
      device_id: options.deviceId ?? "",
      bucket: window.bucket,
      from: window.from.toISOString().replace(/\.\d{3}Z$/, "Z"),
      to: window.to.toISOString().replace(/\.\d{3}Z$/, "Z"),
    });
    const payload = await request<{ points?: UsagePoint[] | null }>(`/api/v1/usage?${search}`);
    return Array.isArray(payload.points) ? payload.points : [];
  },
  async audit() {
    const payload = await request<{ events?: AuditEvent[] | null }>("/api/v1/audit?limit=100");
    return Array.isArray(payload.events) ? payload.events : [];
  },
  async regions() {
    const payload = await request<{ regions?: Region[] | null }>("/api/v2/admin/regions");
    return Array.isArray(payload.regions) ? payload.regions : [];
  },
  async nodes(region = "") {
    const query = region ? `?region=${encodeURIComponent(region)}` : "";
    const payload = await request<{ nodes?: VPNNode[] | null }>(
      `/api/v2/admin/nodes${query}`,
    );
    return Array.isArray(payload.nodes) ? payload.nodes : [];
  },
};
