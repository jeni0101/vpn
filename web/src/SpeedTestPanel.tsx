import SpeedTest, {
  type ConfigOptions,
  type MeasurementSummary,
  type MeasurementType,
  type PhaseChangePayload,
  type Results,
} from "@cloudflare/speedtest";
import { useEffect, useRef, useState } from "react";
import { Button, EmptyState, Icon, Panel } from "./ui";

const measurements: NonNullable<ConfigOptions["measurements"]> = [
  { type: "latency", numPackets: 10 },
  { type: "download", bytes: 100_000, count: 3, bypassMinDuration: true },
  { type: "download", bytes: 1_000_000, count: 3 },
  { type: "download", bytes: 10_000_000, count: 2 },
  { type: "upload", bytes: 100_000, count: 3, bypassMinDuration: true },
  { type: "upload", bytes: 1_000_000, count: 3 },
  { type: "upload", bytes: 5_000_000, count: 2 },
];

export interface SpeedEngine {
  results: Results;
  onResultsChange: (payload: { type: MeasurementType }) => void;
  onPhaseChange: (payload: PhaseChangePayload) => void;
  onFinish: (results: Results) => void;
  onError: (message: string) => void;
  play: () => void;
  pause: () => void;
}

export type SpeedEngineFactory = (options: ConfigOptions) => SpeedEngine;

const defaultFactory: SpeedEngineFactory = (options) => new SpeedTest(options);

export function SpeedTestPanel({
  createEngine = defaultFactory,
}: {
  createEngine?: SpeedEngineFactory;
}) {
  const [status, setStatus] = useState<"idle" | "running" | "done" | "cancelled" | "error">("idle");
  const [summary, setSummary] = useState<MeasurementSummary>({});
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState("");
  const engineRef = useRef<SpeedEngine | null>(null);
  const runRef = useRef(0);
  const timeoutRef = useRef<number | null>(null);

  const stopTimer = () => {
    if (timeoutRef.current !== null) window.clearTimeout(timeoutRef.current);
    timeoutRef.current = null;
  };

  const cancel = (timedOut = false) => {
    runRef.current++;
    engineRef.current?.pause();
    engineRef.current = null;
    stopTimer();
    setStatus(timedOut ? "error" : "cancelled");
    setError(timedOut ? "测试超过 30 秒，已自动停止。请检查网络后重试。" : "");
  };

  const start = () => {
    cancel();
    const run = ++runRef.current;
    setSummary({});
    setError("");
    setProgress(3);
    setStatus("running");
    const engine = createEngine({
      autoStart: false,
      measurements,
      measureDownloadLoadedLatency: false,
      measureUploadLoadedLatency: false,
      bandwidthFinishRequestDuration: 1200,
      bandwidthAbortRequestDuration: 5000,
      bandwidthMinRequestDuration: 10,
    });
    engineRef.current = engine;
    engine.onResultsChange = () => {
      if (run !== runRef.current) return;
      setSummary(engine.results.getSummary());
    };
    engine.onPhaseChange = ({ measurementId }) => {
      if (run !== runRef.current) return;
      setProgress(Math.min(92, Math.round((measurementId + 1) / measurements.length * 92)));
    };
    engine.onFinish = (results) => {
      if (run !== runRef.current) return;
      stopTimer();
      setSummary(results.getSummary());
      setProgress(100);
      setStatus("done");
      engineRef.current = null;
    };
    engine.onError = (reason) => {
      if (run !== runRef.current) return;
      stopTimer();
      setError(reason || "Cloudflare 测速请求失败。");
      setStatus("error");
      engineRef.current = null;
    };
    timeoutRef.current = window.setTimeout(() => cancel(true), 30_000);
    engine.play();
  };

  useEffect(() => () => {
    runRef.current++;
    engineRef.current?.pause();
    stopTimer();
  }, []);

  return (
    <div className="speed-layout">
      <Panel className="speed-hero">
        <div className="panel-heading">
          <div>
            <p className="eyebrow">Current route diagnostics</p>
            <h2>当前设备网络测试</h2>
            <p>测试浏览器当前实际上网路径。VPN 已连接时，结果反映 VPN 出口体验。</p>
          </div>
          {status === "running"
            ? <Button onClick={() => cancel()} variant="secondary">取消测试</Button>
            : <Button icon="gauge" onClick={start}>{status === "done" ? "重新测试" : "开始轻量测试"}</Button>}
        </div>
        <div className="speed-notice">
          <Icon name="shield" />
          <span>预计消耗不超过约 40 MB 流量；测速请求和完成结果会发送给 Cloudflare，本面板不会保存测速历史。</span>
        </div>
        {status === "running" && (
          <div className="speed-progress" aria-label={`测试进度 ${progress}%`}>
            <div><i style={{ width: `${progress}%` }} /></div>
            <span>正在测试… {progress}%</span>
          </div>
        )}
        {(status === "error" || status === "cancelled") && (
          <div className={`speed-message ${status}`}>
            {status === "cancelled" ? "测试已取消，本次结果没有保存。" : error}
          </div>
        )}
        {status === "idle" && (
          <EmptyState description="点击开始后将依次测量延迟、下载速度和上传速度。" title="尚未开始测试" />
        )}
        {(status === "running" || status === "done") && (
          <div className="speed-metrics" aria-live="polite">
            <SpeedMetric icon="activity" label="空闲延迟" value={formatMs(summary.latency)} />
            <SpeedMetric icon="activity" label="抖动" value={formatMs(summary.jitter)} />
            <SpeedMetric icon="download" label="下载" value={formatMbps(summary.download)} accent />
            <SpeedMetric icon="upload" label="上传" value={formatMbps(summary.upload)} />
          </div>
        )}
      </Panel>
      <Panel className="speed-help">
        <p className="eyebrow">Reading the result</p>
        <h3>如何理解</h3>
        <dl>
          <div><dt>延迟</dt><dd>越低越好，直接影响网页响应、游戏和远程控制。</dd></div>
          <div><dt>抖动</dt><dd>延迟的波动幅度，越稳定越适合语音和视频。</dd></div>
          <div><dt>上下行</dt><dd>当前设备经现有网络路径访问互联网的吞吐能力。</dd></div>
        </dl>
      </Panel>
    </div>
  );
}

function SpeedMetric({
  icon,
  label,
  value,
  accent = false,
}: {
  icon: "activity" | "download" | "upload";
  label: string;
  value: string;
  accent?: boolean;
}) {
  return (
    <div className={accent ? "accent" : ""}>
      <span><Icon name={icon} size={17} />{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function formatMs(value: number | boolean | undefined) {
  return typeof value === "number" && Number.isFinite(value) ? `${value.toFixed(1)} ms` : "—";
}

function formatMbps(value: number | boolean | undefined) {
  return typeof value === "number" && Number.isFinite(value) ? `${(value / 1_000_000).toFixed(1)} Mbps` : "—";
}
