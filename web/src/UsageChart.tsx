import { useMemo, useState } from "react";
import type { UsagePoint } from "./types";
import { buildUsageBuckets, type UsageRange } from "./usage";
import { EmptyState } from "./ui";

export function UsageChart({
  points,
  range,
  now,
}: {
  points: UsagePoint[] | null | undefined;
  range: UsageRange;
  now?: Date;
}) {
  const buckets = useMemo(() => buildUsageBuckets(points, range, now), [now, points, range]);
  const [hovered, setHovered] = useState<number | null>(null);
  const max = Math.max(...buckets.map((item) => item.upload + item.download), 0);
  const totalUpload = buckets.reduce((sum, item) => sum + item.upload, 0);
  const totalDownload = buckets.reduce((sum, item) => sum + item.download, 0);
  if (max === 0) {
    return (
      <EmptyState
        description="所选时间范围内没有 WireGuard 流量增量。图表不会生成模拟数据。"
        title="暂无真实流量样本"
      />
    );
  }

  const width = 1000;
  const height = 300;
  const chartTop = 22;
  const chartBottom = 248;
  const chartHeight = chartBottom - chartTop;
  const column = width / buckets.length;
  const barWidth = Math.max(5, Math.min(28, column * 0.58));
  const labelStep = range === "24h" ? 4 : range === "7d" ? 1 : 5;

  return (
    <div className="usage-chart-wrap">
      <div className="chart-summary">
        <span><i className="legend-download" />下载 <strong>{formatBytes(totalDownload)}</strong></span>
        <span><i className="legend-upload" />上传 <strong>{formatBytes(totalUpload)}</strong></span>
      </div>
      <div className="usage-chart">
        <svg aria-label="真实 WireGuard 上传下载流量图" role="img" viewBox={`0 0 ${width} ${height}`}>
          {[0, 0.25, 0.5, 0.75, 1].map((ratio) => {
            const y = chartBottom - chartHeight * ratio;
            return (
              <g key={ratio}>
                <line className="chart-grid" x1="0" x2={width} y1={y} y2={y} />
                <text className="chart-axis" x="4" y={Math.max(12, y - 5)}>{formatBytes(max * ratio)}</text>
              </g>
            );
          })}
          {buckets.map((bucket, index) => {
            const x = index * column + (column - barWidth) / 2;
            const downloadHeight = bucket.download / max * chartHeight;
            const uploadHeight = bucket.upload / max * chartHeight;
            const downloadY = chartBottom - downloadHeight;
            const uploadY = downloadY - uploadHeight;
            return (
              <g
                data-bucket={bucket.key}
                key={bucket.key}
                onMouseEnter={() => setHovered(index)}
                onMouseLeave={() => setHovered(null)}
                onFocus={() => setHovered(index)}
                onBlur={() => setHovered(null)}
                tabIndex={0}
              >
                <rect className="chart-hit" height={chartHeight} width={column} x={index * column} y={chartTop} />
                {bucket.download > 0 && <rect className="bar-download" height={downloadHeight} rx="3" width={barWidth} x={x} y={downloadY} />}
                {bucket.upload > 0 && <rect className="bar-upload" height={uploadHeight} rx="3" width={barWidth} x={x} y={uploadY} />}
                {index % labelStep === 0 && <text className="chart-label" textAnchor="middle" x={x + barWidth / 2} y="280">{bucket.label}</text>}
              </g>
            );
          })}
        </svg>
        {hovered !== null && buckets[hovered] && (
          <div className="chart-tooltip" style={{ left: `${Math.min(88, Math.max(8, (hovered + 0.5) / buckets.length * 100))}%` }}>
            <strong>{buckets[hovered].label}</strong>
            <span>下载 {formatBytes(buckets[hovered].download)}</span>
            <span>上传 {formatBytes(buckets[hovered].upload)}</span>
          </div>
        )}
      </div>
    </div>
  );
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  if (value < 1024) return `${Math.round(value)} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let size = value / 1024;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit++;
  }
  return `${size.toFixed(size >= 100 ? 0 : size >= 10 ? 1 : 2)} ${units[unit]}`;
}

export function formatRate(bytesPerSecond: number) {
  return `${formatBytes(bytesPerSecond)}/s`;
}
