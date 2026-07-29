import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { api } from "./api";
import type { AuditEvent, Device, Region, UsagePoint, VPNNode } from "./types";
import {
  Button,
  Dialog,
  EmptyState,
  ErrorBoundary,
  ErrorState,
  Icon,
  Panel,
  Skeleton,
  Status,
  Tabs,
  type IconName,
} from "./ui";
import { UsageChart, formatBytes, formatRate } from "./UsageChart";
import {
  mergeDeviceRates,
  safeBytes,
  type DeviceRate,
  type UsageRange,
} from "./usage";

type Screen = "dashboard" | "regions" | "devices" | "usage" | "audit";
type AuthPhase = "loading" | "login" | "totp" | "ready";

const platformName: Record<string, string> = {
  windows: "Windows",
  android: "Android",
  ios: "iOS",
  macos: "macOS",
  other: "其他",
};

export default function App() {
  const [phase, setPhase] = useState<AuthPhase>("loading");
  const [username, setUsername] = useState("");
  const [screen, setScreen] = useState<Screen>("dashboard");
  const [devices, setDevices] = useState<Device[]>([]);
  const [rates, setRates] = useState<Record<string, DeviceRate>>({});
  const [dashboardUsage, setDashboardUsage] = useState<UsagePoint[]>([]);
  const [coreLoading, setCoreLoading] = useState(true);
  const [error, setError] = useState("");
  const [addOpen, setAddOpen] = useState(false);
  const previousDevices = useRef<Device[]>([]);

  const acceptDevices = useCallback((next: Device[] | null | undefined) => {
    const safe = Array.isArray(next) ? next : [];
    setRates((existing) => mergeDeviceRates(existing, previousDevices.current, safe));
    previousDevices.current = safe;
    setDevices(safe);
  }, []);

  const refreshCore = useCallback(async () => {
    try {
      const [nextDevices, nextUsage] = await Promise.all([
        api.devices(),
        api.usage({ range: "24h" }),
      ]);
      acceptDevices(nextDevices);
      setDashboardUsage(nextUsage);
      setError("");
    } catch (reason) {
      setError(message(reason));
    } finally {
      setCoreLoading(false);
    }
  }, [acceptDevices]);

  useEffect(() => {
    api.session()
      .then((session) => {
        setUsername(session.username);
        setPhase("ready");
      })
      .catch(() => setPhase("login"));
  }, []);

  useEffect(() => {
    if (phase !== "ready") return;
    void refreshCore();
    const timer = window.setInterval(() => void refreshCore(), 30_000);
    const source = new EventSource("/api/v1/events");
    source.addEventListener("devices", (event) => {
      try {
        const payload = JSON.parse((event as MessageEvent).data) as { devices?: Device[] };
        acceptDevices(payload.devices);
      } catch {
        // The 30-second refresh remains the fallback for malformed SSE messages.
      }
    });
    return () => {
      window.clearInterval(timer);
      source.close();
    };
  }, [acceptDevices, phase, refreshCore]);

  if (phase === "loading") return <Splash />;
  if (phase === "login" || phase === "totp") {
    return (
      <Login
        error={error}
        onError={setError}
        onPassword={() => setPhase("totp")}
        onReady={(name) => {
          setUsername(name);
          setError("");
          setPhase("ready");
        }}
        phase={phase}
      />
    );
  }

  return (
    <div className="shell">
      <Sidebar onScreen={setScreen} screen={screen} />
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
              aria-label="退出登录"
              className="account-logout"
              onClick={() => void api.logout().then(() => setPhase("login"))}
            >
              <Icon name="logout" size={16} />
              <span>退出</span>
            </button>
          </div>
        </header>
        {error && (
          <div className="alert" role="alert">
            <span>{error}</span>
            <button aria-label="关闭提示" onClick={() => setError("")}>×</button>
          </div>
        )}
        <ErrorBoundary>
          {screen === "dashboard" && (
            <Dashboard
              devices={devices}
              error={error}
              loading={coreLoading}
              onRetry={() => void refreshCore()}
              points={dashboardUsage}
              rates={rates}
            />
          )}
          {screen === "devices" && (
            <Devices
              devices={devices}
              onAdd={() => setAddOpen(true)}
              onChanged={() => void refreshCore()}
              onError={setError}
              rates={rates}
            />
          )}
          {screen === "regions" && <RegionsPage />}
          {screen === "usage" && <UsagePage devices={devices} rates={rates} />}
          {screen === "audit" && <AuditPage />}
        </ErrorBoundary>
      </main>
      <Dialog
        description="安全注册文件由客户端在本地生成私钥；标准配置只能下载一次。"
        onClose={() => setAddOpen(false)}
        open={addOpen}
        title="添加设备"
      >
        <AddDevice
          onClose={() => setAddOpen(false)}
          onCreated={() => {
            setAddOpen(false);
            void refreshCore();
          }}
          onError={setError}
        />
      </Dialog>
    </div>
  );
}

function Splash() {
  return (
    <div className="auth-page">
      <div className="brand-mark">T</div>
      <span className="splash-spinner" />
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
        const result = await api.totp(code.replace(/\s/g, ""));
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
          <p>{phase === "login" ? "使用管理员凭据继续。" : "输入验证器中 TNest VPN 对应的 6 位验证码或恢复码。"}</p>
        </div>
        {error && <div className="alert compact" role="alert">{error}</div>}
        <form onSubmit={submit}>
          {phase === "login" ? (
            <>
              <label>用户名<input autoComplete="username" onChange={(event) => setUsername(event.target.value)} value={username} /></label>
              <label>密码<input autoComplete="current-password" autoFocus onChange={(event) => setPassword(event.target.value)} type="password" value={password} /></label>
            </>
          ) : (
            <label>验证码<input autoComplete="one-time-code" autoFocus inputMode="numeric" onChange={(event) => setCode(event.target.value)} value={code} /></label>
          )}
          <Button className="wide" loading={busy} type="submit">继续</Button>
        </form>
      </section>
      <p className="auth-foot">认证凭据不会保存在浏览器中</p>
    </div>
  );
}

function Sidebar({
  screen,
  onScreen,
}: {
  screen: Screen;
  onScreen: (value: Screen) => void;
}) {
  const links: Array<{ value: Screen; icon: IconName; label: string }> = [
    { value: "dashboard", icon: "dashboard", label: "总览" },
    { value: "regions", icon: "server", label: "地区" },
    { value: "devices", icon: "devices", label: "设备" },
    { value: "usage", icon: "traffic", label: "流量" },
    { value: "audit", icon: "audit", label: "审计" },
  ];
  return (
    <aside className="sidebar">
      <div className="brand-row">
        <div className="brand-mark small">T</div>
        <div><strong>TNest</strong><span>VPN Console</span></div>
      </div>
      <nav aria-label="主要导航">
        {links.map((link) => (
          <button
            aria-current={screen === link.value ? "page" : undefined}
            className={screen === link.value ? "active" : ""}
            key={link.value}
            onClick={() => onScreen(link.value)}
          >
            <Icon name={link.icon} size={19} /><span>{link.label}</span>
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

function Dashboard({
  devices,
  points,
  rates,
  loading,
  error,
  onRetry,
}: {
  devices: Device[];
  points: UsagePoint[];
  rates: Record<string, DeviceRate>;
  loading: boolean;
  error: string;
  onRetry: () => void;
}) {
  if (loading) return <><Skeleton height={132} /><Skeleton height={340} /></>;
  if (error && devices.length === 0) return <Panel><ErrorState message={error} onRetry={onRetry} /></Panel>;
  const active = devices.filter((device) => device.status === "active");
  const online = active.filter(isOnline);
  const totalUpload = active.reduce((sum, item) => sum + safeBytes(item.upload_bytes), 0);
  const totalDownload = active.reduce((sum, item) => sum + safeBytes(item.download_bytes), 0);
  const liveRate = Object.values(rates).reduce(
    (sum, rate) => ({
      uploadBps: sum.uploadBps + rate.uploadBps,
      downloadBps: sum.downloadBps + rate.downloadBps,
    }),
    { uploadBps: 0, downloadBps: 0 },
  );
  return (
    <>
      <section className="stats-grid">
        <Metric accent icon="devices" label="在线设备" note="三分钟内有握手" value={`${online.length} / ${active.length}`} />
        <Metric icon="download" label="最近下载速率" note="WireGuard 最近采样均值" value={formatRate(liveRate.downloadBps)} />
        <Metric icon="upload" label="最近上传速率" note="WireGuard 最近采样均值" value={formatRate(liveRate.uploadBps)} />
        <Metric icon="traffic" label="累计流量" note="自管理统计启用以来" value={formatBytes(totalUpload + totalDownload)} />
      </section>
      <Panel>
        <div className="panel-heading">
          <div><p className="eyebrow">真实 WireGuard 计数</p><h2>过去 24 小时网络用量</h2></div>
          <span className="muted">每 30 秒采样 · Asia/Shanghai 显示</span>
        </div>
        <UsageChart points={points} range="24h" />
      </Panel>
      <Panel>
        <div className="panel-heading">
          <div><p className="eyebrow">Live peers</p><h2>最近设备</h2></div>
        </div>
        <DeviceRows devices={devices.slice(0, 6)} rates={rates} />
      </Panel>
    </>
  );
}

function Devices({
  devices,
  rates,
  onAdd,
  onChanged,
  onError,
}: {
  devices: Device[];
  rates: Record<string, DeviceRate>;
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
    const code = prompt("生成多地区迁移文件需要当前 TOTP 验证码");
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
  const downloadAppleBundle = async (device: Device) => {
    const code = prompt("重新生成 Apple 多地区配置需要当前 TOTP 验证码");
    if (!code) return;
    try {
      const result = await api.appleBundle(device.id, code);
      const link = document.createElement("a");
      link.href = result.download_url;
      link.click();
    } catch (reason) {
      onError(message(reason));
    }
  };
  return (
    <Panel>
      <div className="panel-heading">
        <div><p className="eyebrow">Peer management</p><h2>全部设备</h2><p>撤销设备仍保留历史用量，在线设备使用最近三分钟握手判断。</p></div>
        <Button icon="plus" onClick={onAdd}>添加设备</Button>
      </div>
      {devices.length === 0 ? (
        <EmptyState description="创建第一台设备后，可在这里查看连接状态和真实流量。" title="暂无设备" />
      ) : (
        <div className="device-table">
          <div className="table-head"><span>设备</span><span>地址</span><span>累计流量</span><span>最近速率</span><span>状态</span><span>操作</span></div>
          {devices.map((device) => (
            <div className="table-row" key={device.id}>
              <DeviceName device={device} />
              <div className="mono"><strong>{device.ipv4}</strong><small>{device.ipv6}</small></div>
              <div><strong>↓ {formatBytes(safeBytes(device.download_bytes))}</strong><small>↑ {formatBytes(safeBytes(device.upload_bytes))}</small></div>
              <div><strong>↓ {formatRate(rates[device.id]?.downloadBps ?? 0)}</strong><small>↑ {formatRate(rates[device.id]?.uploadBps ?? 0)}</small></div>
              <DeviceStatus device={device} />
              <div className="row-actions">
                {device.platform === "ios" || device.platform === "macos" ? (
                  <Button disabled={device.status !== "active"} onClick={() => void downloadAppleBundle(device)} variant="ghost">下载地区 ZIP</Button>
                ) : (
                  <Button disabled={device.status !== "active"} onClick={() => void rotate(device)} variant="ghost">迁移到多地区</Button>
                )}
                <Button disabled={device.status !== "active"} onClick={() => void revoke(device)} variant="danger">撤销</Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </Panel>
  );
}

function UsagePage({
  devices,
  rates,
}: {
  devices: Device[];
  rates: Record<string, DeviceRate>;
}) {
  const [range, setRange] = useState<UsageRange>("24h");
  const [deviceId, setDeviceId] = useState("");
  const [points, setPoints] = useState<UsagePoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    setLoading(true);
    try {
      setPoints(await api.usage({ range, deviceId }));
      setError("");
    } catch (reason) {
      setError(message(reason));
    } finally {
      setLoading(false);
    }
  }, [deviceId, range]);
  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), 30_000);
    return () => window.clearInterval(timer);
  }, [load]);
  const visibleDevices = deviceId ? devices.filter((device) => device.id === deviceId) : devices;
  const totalUpload = visibleDevices.reduce((sum, item) => sum + safeBytes(item.upload_bytes), 0);
  const totalDownload = visibleDevices.reduce((sum, item) => sum + safeBytes(item.download_bytes), 0);
  const rate = visibleDevices.reduce(
    (sum, item) => ({
      uploadBps: sum.uploadBps + (rates[item.id]?.uploadBps ?? 0),
      downloadBps: sum.downloadBps + (rates[item.id]?.downloadBps ?? 0),
    }),
    { uploadBps: 0, downloadBps: 0 },
  );
  return (
    <>
      <Panel>
        <div className="panel-heading usage-heading">
          <div><p className="eyebrow">Usage analytics</p><h2>真实流量统计</h2><p>数据来自服务端 WireGuard peer 计数器，不包含管理服务启用前的历史。</p></div>
          <div className="usage-controls">
            <Tabs
              items={[{ value: "24h", label: "24小时" }, { value: "7d", label: "7天" }, { value: "30d", label: "30天" }]}
              label="统计周期"
              onChange={setRange}
              value={range}
            />
            <label className="select-label">
              <span>设备</span>
              <select onChange={(event) => setDeviceId(event.target.value)} value={deviceId}>
                <option value="">全部设备</option>
                {devices.map((device) => <option key={device.id} value={device.id}>{device.name}{device.status === "revoked" ? "（已撤销）" : ""}</option>)}
              </select>
            </label>
          </div>
        </div>
        {loading ? <Skeleton height={350} /> : error ? <ErrorState message={error} onRetry={() => void load()} /> : <UsageChart points={points} range={range} />}
      </Panel>
      <section className="stats-grid usage-stats">
        <Metric icon="download" label="累计下载" note="自统计启用以来" value={formatBytes(totalDownload)} />
        <Metric icon="upload" label="累计上传" note="自统计启用以来" value={formatBytes(totalUpload)} />
        <Metric icon="download" label="最近下载速率" note="最近真实采样均值" value={formatRate(rate.downloadBps)} />
        <Metric icon="upload" label="最近上传速率" note="最近真实采样均值" value={formatRate(rate.uploadBps)} />
      </section>
    </>
  );
}

function RegionsPage() {
  const [regions, setRegions] = useState<Region[]>([]);
  const [nodes, setNodes] = useState<VPNNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [nextRegions, nextNodes] = await Promise.all([api.regions(), api.nodes()]);
      setRegions(nextRegions);
      setNodes(nextNodes);
      setError("");
    } catch (reason) {
      setError(message(reason));
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => { void load(); }, [load]);
  if (loading) return <><Skeleton height={132} /><Skeleton height={320} /></>;
  if (error) return <Panel><ErrorState message={error} onRetry={() => void load()} /></Panel>;
  return (
    <Panel>
      <div className="panel-heading">
        <div>
          <p className="eyebrow">Regions and nodes</p>
          <h2>地区与节点</h2>
          <p>客户端只显示地区；节点按健康状态、优先级和延迟自动选择。</p>
        </div>
        <Button icon="refresh" onClick={() => void load()} variant="secondary">刷新</Button>
      </div>
      <div className="region-list">
        {regions.map((region) => {
          const regionNodes = nodes.filter((node) => node.region_code === region.code);
          return (
            <section className="region-card" key={region.code}>
              <header>
                <div>
                  <strong>{region.display_name}</strong>
                  <small>{region.code} · 配置 v{region.config_version}</small>
                </div>
                <Status tone={region.enabled ? "online" : "warning"}>
                  {region.enabled ? "已启用" : "未启用"}
                </Status>
              </header>
              <p>{region.exit_mode === "dual_stack"
                ? "IPv4 / IPv6 双栈出口"
                : "IPv4 出口，IPv6 安全阻断"}</p>
              <div className="region-nodes">
                {regionNodes.length === 0 && <span>尚未登记节点</span>}
                {regionNodes.map((node) => (
                  <div key={node.id}>
                    <div>
                      <strong>{node.id}</strong>
                      <small>{node.endpoint || "Endpoint 待配置"}</small>
                    </div>
                    <Status tone={
                      !node.enabled ? "warning" :
                        node.health === "healthy" ? "online" :
                          node.health === "offline" ? "danger" : "neutral"
                    }>
                      {!node.enabled ? "禁用" : node.health}
                    </Status>
                  </div>
                ))}
              </div>
            </section>
          );
        })}
      </div>
    </Panel>
  );
}

function AuditPage() {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [filter, setFilter] = useState<"all" | "auth" | "device">("all");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    setLoading(true);
    try {
      setEvents(await api.audit());
      setError("");
    } catch (reason) {
      setError(message(reason));
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);
  const filtered = useMemo(
    () => events.filter((event) => filter === "all" || event.action?.startsWith(`${filter}.`)),
    [events, filter],
  );
  return (
    <Panel>
      <div className="panel-heading">
        <div><p className="eyebrow">Security log</p><h2>管理审计</h2><p>只记录操作元数据，不记录密码、TOTP、密钥、PSK或配置正文。</p></div>
        <Button icon="refresh" loading={loading} onClick={() => void load()} variant="secondary">刷新</Button>
      </div>
      <Tabs
        items={[{ value: "all", label: "全部" }, { value: "auth", label: "登录安全" }, { value: "device", label: "设备操作" }]}
        label="审计类型"
        onChange={setFilter}
        value={filter}
      />
      {loading ? <Skeleton height={260} /> : error ? (
        <ErrorState message={error} onRetry={() => void load()} />
      ) : filtered.length === 0 ? (
        <EmptyState description={events.length === 0 ? "尚未产生管理操作记录。" : "当前筛选条件下没有记录。"} title="暂无审计记录" />
      ) : (
        <div className="audit-table">
          <div className="audit-head"><span>操作</span><span>管理员</span><span>来源 IP</span><span>目标</span><span>时间</span></div>
          {filtered.map((event) => (
            <div className="audit-row" key={event.id}>
              <div className="audit-action"><span><Icon name={event.action?.startsWith("auth.") ? "shield" : "check"} size={17} /></span><div><strong>{actionLabel(event.action)}</strong>{event.detail && <small>{event.detail}</small>}</div></div>
              <span>{event.actor || "系统"}</span>
              <span className="mono">{event.remote_ip || "本机"}</span>
              <span className="mono">{event.device_id || "—"}</span>
              <time dateTime={event.at}>{safeDate(event.at)}</time>
            </div>
          ))}
        </div>
      )}
    </Panel>
  );
}

function AddDevice({
  onClose,
  onCreated,
  onError,
}: {
  onClose: () => void;
  onCreated: () => void;
  onError: (value: string) => void;
}) {
  const [name, setName] = useState("");
  const [platform, setPlatform] = useState("windows");
  const [mode, setMode] = useState("invite");
  const [totp, setTotp] = useState("");
  const [busy, setBusy] = useState(false);
  const applePlatform = platform === "ios" || platform === "macos";
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
    <form onSubmit={submit}>
      <label>设备名称<input autoFocus onChange={(event) => setName(event.target.value)} placeholder="例如：办公电脑" required value={name} /></label>
      <label>平台<select onChange={(event) => {
        const value = event.target.value;
        setPlatform(value);
        setMode(value === "ios" || value === "macos" ? "apple_bundle" : "invite");
      }} value={platform}><option value="windows">Windows</option><option value="android">Android</option><option value="ios">iOS</option><option value="macos">macOS</option><option value="other">其他</option></select></label>
      <label>配置方式<select onChange={(event) => setMode(event.target.value)} value={mode}>{applePlatform ? <option value="apple_bundle">Apple 多地区 WireGuard ZIP（推荐）</option> : <><option value="invite">TNest 安全注册文件（推荐）</option><option value="standard">标准 WireGuard .conf</option><option value="qr">WireGuard 二维码 PNG</option></>}</select></label>
      <label>当前 TOTP<input autoComplete="one-time-code" inputMode="numeric" onChange={(event) => setTotp(event.target.value)} required value={totp} /></label>
      <div className="dialog-actions"><Button onClick={onClose} variant="secondary">取消</Button><Button loading={busy} type="submit">创建并下载</Button></div>
    </form>
  );
}

function DeviceRows({
  devices,
  rates,
}: {
  devices: Device[];
  rates: Record<string, DeviceRate>;
}) {
  if (devices.length === 0) return <EmptyState description="尚未导入或创建设备。" title="暂无设备" />;
  return (
    <div className="device-rows">
      {devices.map((device) => (
        <div key={device.id}>
          <DeviceName device={device} />
          <div className="device-live-rate"><strong>↓ {formatRate(rates[device.id]?.downloadBps ?? 0)}</strong><small>↑ {formatRate(rates[device.id]?.uploadBps ?? 0)}</small></div>
          <span className="mono">{device.ipv4}</span>
          <DeviceStatus device={device} />
        </div>
      ))}
    </div>
  );
}

function DeviceName({ device }: { device: Device }) {
  return (
    <div className="device-name">
      <span className={`platform ${device.platform}`}>{platformIcon(device.platform)}</span>
      <div><strong>{device.name}</strong><small>{platformName[device.platform] || "其他"}{device.external_private_key ? " · 已有配置" : ""}</small></div>
    </div>
  );
}

function DeviceStatus({ device }: { device: Device }) {
  if (device.status === "pending") return <Status tone="warning">待注册</Status>;
  if (device.status === "revoked") return <Status tone="danger">已撤销</Status>;
  return isOnline(device) ? <Status tone="online">在线</Status> : <Status>离线</Status>;
}

function Metric({
  label,
  value,
  note,
  icon,
  accent = false,
}: {
  label: string;
  value: string;
  note: string;
  icon: IconName;
  accent?: boolean;
}) {
  return (
    <div className={`metric ${accent ? "accent" : ""}`}>
      <span><Icon name={icon} size={16} />{label}</span>
      <strong>{value}</strong>
      <small>{note}</small>
    </div>
  );
}

function isOnline(device: Device) {
  const time = device.last_handshake ? new Date(device.last_handshake).getTime() : Number.NaN;
  return Number.isFinite(time) && Date.now() - time < 3 * 60 * 1000;
}

function platformIcon(platform: string) {
  return platform === "windows" ? "⊞" : platform === "android" ? "A" : platform === "ios" ? "●" : platform === "macos" ? "◆" : "◇";
}

function title(screen: Screen) {
  return ({
    dashboard: "运行总览",
    regions: "地区与节点",
    devices: "设备管理",
    usage: "流量统计",
    audit: "安全审计",
  } as const)[screen];
}

function actionLabel(action: string | undefined) {
  return ({
    "auth.login": "管理员登录成功",
    "auth.logout": "管理员退出",
    "device.invite_created": "已创建安全注册文件",
    "device.standard_config_created": "已创建标准配置",
    "device.qr_created": "已创建一次性二维码",
    "device.rotation_prepared": "已创建轮换注册文件",
    "device.migration_prepared": "已创建多地区迁移文件",
    "device.enrolled": "设备完成注册或轮换",
    "device.revoked": "设备已撤销",
  } as Record<string, string>)[action ?? ""] || action || "未知操作";
}

function safeDate(value: string | undefined) {
  const date = value ? new Date(value) : new Date(Number.NaN);
  return Number.isNaN(date.getTime())
    ? "未知时间"
    : date.toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", hour12: false });
}

function message(reason: unknown) {
  return reason instanceof Error ? reason.message : "操作失败";
}
