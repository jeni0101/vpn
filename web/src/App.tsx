import { FormEvent, useEffect, useMemo, useState } from "react";
import { api } from "./api";
import type { AuditEvent, Device, UsagePoint } from "./types";

type Screen = "dashboard" | "devices" | "usage" | "audit";
type AuthPhase = "loading" | "login" | "totp" | "ready";

const platformName: Record<string, string> = {
  windows: "Windows",
  android: "Android",
  ios: "iOS",
  macos: "macOS",
  other: "其他",
};

function App() {
  const [phase, setPhase] = useState<AuthPhase>("loading");
  const [username, setUsername] = useState("");
  const [screen, setScreen] = useState<Screen>("dashboard");
  const [devices, setDevices] = useState<Device[]>([]);
  const [usage, setUsage] = useState<UsagePoint[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [error, setError] = useState("");
  const [addOpen, setAddOpen] = useState(false);

  useEffect(() => {
    api.session()
      .then((session) => {
        setUsername(session.username);
        setPhase("ready");
      })
      .catch(() => setPhase("login"));
  }, []);

  const refresh = async () => {
    try {
      const [nextDevices, nextUsage] = await Promise.all([api.devices(), api.usage()]);
      setDevices(nextDevices);
      setUsage(nextUsage);
      if (screen === "audit") setAudit(await api.audit());
    } catch (reason) {
      setError(message(reason));
    }
  };

  useEffect(() => {
    if (phase !== "ready") return;
    void refresh();
    const source = new EventSource("/api/v1/events");
    source.addEventListener("devices", (event) => {
      try {
        const payload = JSON.parse((event as MessageEvent).data);
        setDevices(payload.devices);
      } catch {
        // A malformed event is ignored; the next one replaces it.
      }
    });
    return () => source.close();
  }, [phase, screen]);

  if (phase === "loading") return <Splash />;
  if (phase === "login" || phase === "totp") {
    return (
      <Login
        phase={phase}
        error={error}
        onError={setError}
        onPassword={() => setPhase("totp")}
        onReady={(name) => {
          setUsername(name);
          setError("");
          setPhase("ready");
        }}
      />
    );
  }

  return (
    <div className="shell">
      <Sidebar screen={screen} onScreen={setScreen} />
      <main className="main">
        <header className="topbar">
          <div>
            <p className="eyebrow">新加坡 · WireGuard 双栈</p>
            <h1>{title(screen)}</h1>
          </div>
          <div className="account">
            <span className="status-dot" />
            <span>{username}</span>
            <button
              className="text-button"
              onClick={() => void api.logout().then(() => setPhase("login"))}
            >
              退出
            </button>
          </div>
        </header>
        {error && (
          <div className="alert">
            <span>{error}</span>
            <button onClick={() => setError("")}>×</button>
          </div>
        )}
        {screen === "dashboard" && <Dashboard devices={devices} usage={usage} />}
        {screen === "devices" && (
          <Devices
            devices={devices}
            onAdd={() => setAddOpen(true)}
            onChanged={() => void refresh()}
            onError={(value) => setError(value)}
          />
        )}
        {screen === "usage" && <Usage devices={devices} points={usage} />}
        {screen === "audit" && <Audit events={audit} onLoad={() => void refresh()} />}
      </main>
      {addOpen && (
        <AddDevice
          onClose={() => setAddOpen(false)}
          onCreated={() => {
            setAddOpen(false);
            void refresh();
          }}
          onError={setError}
        />
      )}
    </div>
  );
}

function Splash() {
  return (
    <div className="auth-page">
      <div className="brand-mark">T</div>
      <p>正在连接 TNest VPN…</p>
    </div>
  );
}

function Login({
  phase,
  error,
  onError,
  onPassword,
  onReady,
}: {
  phase: "login" | "totp";
  error: string;
  onError: (value: string) => void;
  onPassword: () => void;
  onReady: (name: string) => void;
}) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    onError("");
    try {
      if (phase === "login") {
        await api.login(username, password);
        setPassword("");
        onPassword();
      } else {
        const result = await api.totp(code);
        onReady(result.username);
      }
    } catch (reason) {
      onError(message(reason));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="auth-page">
      <section className="auth-card">
        <div className="brand-row">
          <div className="brand-mark">T</div>
          <div><strong>TNest VPN</strong><span>安全管理控制台</span></div>
        </div>
        <div className="auth-copy">
          <p className="eyebrow">仅限管理员</p>
          <h1>{phase === "login" ? "欢迎回来" : "双因素验证"}</h1>
          <p>{phase === "login" ? "使用管理员凭据继续。" : "输入认证器中的 6 位验证码或恢复码。"}</p>
        </div>
        {error && <div className="alert compact">{error}</div>}
        <form onSubmit={submit}>
          {phase === "login" ? (
            <>
              <label>用户名<input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" /></label>
              <label>密码<input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" autoFocus /></label>
            </>
          ) : (
            <label>验证码<input value={code} onChange={(e) => setCode(e.target.value)} inputMode="numeric" autoComplete="one-time-code" autoFocus /></label>
          )}
          <button className="primary wide" disabled={busy}>{busy ? "验证中…" : "继续"}</button>
        </form>
      </section>
      <p className="auth-foot">连接凭据不会存储在浏览器中</p>
    </div>
  );
}

function Sidebar({ screen, onScreen }: { screen: Screen; onScreen: (value: Screen) => void }) {
  const links: Array<[Screen, string, string]> = [
    ["dashboard", "◫", "总览"],
    ["devices", "⌁", "设备"],
    ["usage", "↗", "流量"],
    ["audit", "≡", "审计"],
  ];
  return (
    <aside className="sidebar">
      <div className="brand-row">
        <div className="brand-mark small">T</div>
        <div><strong>TNest</strong><span>VPN Console</span></div>
      </div>
      <nav>
        {links.map(([value, icon, label]) => (
          <button className={screen === value ? "active" : ""} onClick={() => onScreen(value)} key={value}>
            <span>{icon}</span>{label}
          </button>
        ))}
      </nav>
      <div className="server-pill">
        <span className="status-dot" />
        <div><strong>服务器在线</strong><small>Singapore · wg0</small></div>
      </div>
    </aside>
  );
}

function Dashboard({ devices, usage }: { devices: Device[]; usage: UsagePoint[] }) {
  const active = devices.filter((device) => device.status === "active");
  const online = active.filter(isOnline);
  const totalUpload = active.reduce((sum, item) => sum + item.upload_bytes, 0);
  const totalDownload = active.reduce((sum, item) => sum + item.download_bytes, 0);
  return (
    <>
      <section className="stats-grid">
        <Metric label="在线设备" value={`${online.length} / ${active.length}`} note="三分钟内有握手" accent />
        <Metric label="累计下载" value={formatBytes(totalDownload)} note="服务端发送到设备" />
        <Metric label="累计上传" value={formatBytes(totalUpload)} note="设备发送到服务端" />
        <Metric label="隧道端口" value="51999" note="UDP · IPv4 外层" />
      </section>
      <section className="panel hero-panel">
        <div>
          <p className="eyebrow">过去 24 小时</p>
          <h2>网络用量</h2>
        </div>
        <MiniChart points={usage} />
      </section>
      <section className="panel">
        <div className="panel-title"><div><p className="eyebrow">Live peers</p><h2>最近设备</h2></div></div>
        <DeviceRows devices={devices.slice(0, 6)} />
      </section>
    </>
  );
}

function Devices({
  devices,
  onAdd,
  onChanged,
  onError,
}: {
  devices: Device[];
  onAdd: () => void;
  onChanged: () => void;
  onError: (value: string) => void;
}) {
  const revoke = async (device: Device) => {
    if (!confirm(`确定撤销“${device.name}”吗？该配置将立即失效。`)) return;
    const code = prompt("请输入当前 TOTP 验证码");
    if (!code) return;
    try {
      await api.revoke(device.id, code);
      onChanged();
    } catch (reason) {
      onError(message(reason));
    }
  };
  const rotate = async (device: Device) => {
    const code = prompt("生成轮换文件需要当前 TOTP 验证码");
    if (!code) return;
    try {
      const result = await api.rotate(device.id, code);
      const link = document.createElement("a");
      link.href = result.download_url;
      link.click();
    } catch (reason) {
      onError(message(reason));
    }
  };
  return (
    <section className="panel">
      <div className="panel-title">
        <div><p className="eyebrow">Peer management</p><h2>全部设备</h2></div>
        <button className="primary" onClick={onAdd}>＋ 添加设备</button>
      </div>
      <div className="device-table">
        <div className="table-head"><span>设备</span><span>地址</span><span>流量</span><span>状态</span><span>操作</span></div>
        {devices.map((device) => (
          <div className="table-row" key={device.id}>
            <DeviceName device={device} />
            <div className="mono"><strong>{device.ipv4}</strong><small>{device.ipv6}</small></div>
            <div><strong>↓ {formatBytes(device.download_bytes)}</strong><small>↑ {formatBytes(device.upload_bytes)}</small></div>
            <Status device={device} />
            <div className="row-actions">
              <button className="text-button" disabled={device.status !== "active"} onClick={() => void rotate(device)}>轮换</button>
              <button className="danger-link" disabled={device.status !== "active"} onClick={() => void revoke(device)}>撤销</button>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

function Usage({ devices, points }: { devices: Device[]; points: UsagePoint[] }) {
  return (
    <section className="panel hero-panel">
      <div className="panel-title">
        <div><p className="eyebrow">Asia/Shanghai</p><h2>24 小时流量</h2></div>
        <span className="muted">每 30 秒采集，按小时汇总</span>
      </div>
      <MiniChart points={points} large />
      <div className="usage-list">
        {devices.filter((d) => d.status === "active").map((device) => (
          <div key={device.id}><DeviceName device={device} /><strong>↓ {formatBytes(device.download_bytes)} · ↑ {formatBytes(device.upload_bytes)}</strong></div>
        ))}
      </div>
    </section>
  );
}

function Audit({ events, onLoad }: { events: AuditEvent[]; onLoad: () => void }) {
  useEffect(onLoad, []);
  return (
    <section className="panel">
      <div className="panel-title"><div><p className="eyebrow">Security log</p><h2>管理审计</h2></div></div>
      <div className="audit-list">
        {events.length === 0 && <p className="empty">暂无审计记录</p>}
        {events.map((event) => (
          <div key={event.id}><span className="audit-icon">✓</span><div><strong>{actionLabel(event.action)}</strong><small>{new Date(event.at).toLocaleString("zh-CN")} · {event.actor} · {event.remote_ip || "本机"}</small></div></div>
        ))}
      </div>
    </section>
  );
}

function AddDevice({ onClose, onCreated, onError }: { onClose: () => void; onCreated: () => void; onError: (value: string) => void }) {
  const [name, setName] = useState("");
  const [platform, setPlatform] = useState("windows");
  const [mode, setMode] = useState("invite");
  const [totp, setTotp] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      const result = await api.createDevice(name, platform, mode, totp);
      const link = document.createElement("a");
      link.href = result.download_url;
      link.click();
      onCreated();
    } catch (reason) {
      onError(message(reason));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="modal-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <section className="modal">
        <button className="modal-close" onClick={onClose}>×</button>
        <p className="eyebrow">New peer</p><h2>添加设备</h2>
        <p className="muted">安全注册文件由客户端在本地生成私钥；标准配置仅能下载一次。</p>
        <form onSubmit={submit}>
          <label>设备名称<input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：办公电脑" autoFocus required /></label>
          <label>平台<select value={platform} onChange={(e) => setPlatform(e.target.value)}><option value="windows">Windows</option><option value="android">Android</option><option value="ios">iOS</option><option value="macos">macOS</option><option value="other">其他</option></select></label>
          <label>配置方式<select value={mode} onChange={(e) => setMode(e.target.value)}><option value="invite">TNest 安全注册文件（推荐）</option><option value="standard">标准 WireGuard .conf</option><option value="qr">WireGuard 二维码 PNG</option></select></label>
          <label>当前 TOTP<input value={totp} onChange={(e) => setTotp(e.target.value)} inputMode="numeric" autoComplete="one-time-code" required /></label>
          <button className="primary wide" disabled={busy}>{busy ? "正在创建…" : "创建并下载"}</button>
        </form>
      </section>
    </div>
  );
}

function DeviceRows({ devices }: { devices: Device[] }) {
  return <div className="device-rows">{devices.map((device) => <div key={device.id}><DeviceName device={device} /><span className="mono">{device.ipv4}</span><Status device={device} /></div>)}</div>;
}

function DeviceName({ device }: { device: Device }) {
  return <div className="device-name"><span className={`platform ${device.platform}`}>{platformIcon(device.platform)}</span><div><strong>{device.name}</strong><small>{platformName[device.platform] || "其他"}{device.external_private_key ? " · 已有配置" : ""}</small></div></div>;
}

function Status({ device }: { device: Device }) {
  const label = device.status === "pending" ? "待注册" : device.status === "revoked" ? "已撤销" : isOnline(device) ? "在线" : "离线";
  return <span className={`status ${label === "在线" ? "online" : ""}`}>{label}</span>;
}

function Metric({ label, value, note, accent = false }: { label: string; value: string; note: string; accent?: boolean }) {
  return <div className={`metric ${accent ? "accent" : ""}`}><span>{label}</span><strong>{value}</strong><small>{note}</small></div>;
}

function MiniChart({ points, large = false }: { points: UsagePoint[]; large?: boolean }) {
  const buckets = useMemo(() => {
    const grouped = new Map<string, number>();
    points.forEach((point) => grouped.set(point.bucket, (grouped.get(point.bucket) || 0) + point.upload_bytes + point.download_bytes));
    return [...grouped.entries()].sort(([a], [b]) => a.localeCompare(b)).slice(-24);
  }, [points]);
  const max = Math.max(...buckets.map(([, value]) => value), 1);
  return (
    <div className={`chart ${large ? "large" : ""}`}>
      {buckets.length === 0 && <span className="empty">等待流量样本</span>}
      {buckets.map(([bucket, value]) => <div key={bucket} title={`${new Date(bucket).toLocaleString("zh-CN")} · ${formatBytes(value)}`}><i style={{ height: `${Math.max(4, value / max * 100)}%` }} /></div>)}
    </div>
  );
}

function isOnline(device: Device) {
  return Boolean(device.last_handshake && Date.now() - new Date(device.last_handshake).getTime() < 3 * 60 * 1000);
}
function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let size = value / 1024;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return `${size.toFixed(size >= 100 ? 0 : size >= 10 ? 1 : 2)} ${units[unit]}`;
}
function platformIcon(platform: string) {
  return platform === "windows" ? "⊞" : platform === "android" ? "A" : platform === "ios" ? "●" : platform === "macos" ? "◆" : "◇";
}
function title(screen: Screen) {
  return ({ dashboard: "运行总览", devices: "设备管理", usage: "流量统计", audit: "安全审计" } as const)[screen];
}
function actionLabel(action: string) {
  return ({ "device.invite_created": "已创建安全注册文件", "device.standard_config_created": "已创建标准配置", "device.qr_created": "已创建一次性二维码", "device.rotation_prepared": "已创建轮换注册文件", "device.enrolled": "设备完成注册或轮换", "device.revoked": "设备已撤销" } as Record<string, string>)[action] || action;
}
function message(reason: unknown) {
  return reason instanceof Error ? reason.message : "操作失败";
}

export default App;
