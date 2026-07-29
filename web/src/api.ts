import type { AuditEvent, Device, UsagePoint } from "./types";

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
    return (await request<{ devices: Device[] }>("/api/v1/devices")).devices;
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
  async usage(deviceId = "", bucket = "hour") {
    const now = new Date();
    const from = new Date(now.getTime() - 24 * 60 * 60 * 1000);
    const query = new URLSearchParams({
      device_id: deviceId,
      bucket,
      from: from.toISOString().replace(/\.\d{3}Z$/, "Z"),
      to: new Date(now.getTime() + 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, "Z"),
    });
    return (await request<{ points: UsagePoint[] }>(`/api/v1/usage?${query}`)).points;
  },
  async audit() {
    return (await request<{ events: AuditEvent[] }>("/api/v1/audit?limit=100")).events;
  },
};
