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
  stats_updated_at?: string;
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

export type ExitMode = "dual_stack" | "ipv4_exit_ipv6_blocked";

export interface Region {
  code: string;
  display_name: string;
  sort_order: number;
  exit_mode: ExitMode;
  enabled: boolean;
  config_version: number;
}

export type NodeHealth = "unknown" | "healthy" | "degraded" | "offline";

export interface VPNNode {
  id: string;
  region_code: string;
  endpoint: string;
  probe_url: string;
  server_public_key: string;
  priority: number;
  enabled: boolean;
  health: NodeHealth;
  version?: string;
  last_report_at?: string;
}
