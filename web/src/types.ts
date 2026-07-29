export type DeviceStatus = "pending" | "active" | "revoked";

export interface Device {
  id: string;
  name: string;
  platform: "windows" | "android" | "ios" | "macos" | "other";
  ipv4: string;
  ipv6: string;
  public_key: string;
  status: DeviceStatus;
  external_private_key: boolean;
  created_at: string;
  revoked_at?: string;
  quarantine_until?: string;
  last_handshake?: string;
  upload_bytes: number;
  download_bytes: number;
}
export interface UsagePoint {
  device_id: string;
  bucket: string;
  upload_bytes: number;
  download_bytes: number;
}

export interface AuditEvent {
  id: number;
  at: string;
  actor: string;
  action: string;
  device_id?: string;
  remote_ip?: string;
  detail?: string;
}
