import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { UsageChart } from "./UsageChart";

describe("UsageChart", () => {
  it("renders one narrow real bucket among 24 fixed buckets", () => {
    const { container } = render(
      <UsageChart
        now={new Date("2026-07-29T03:15:00Z")}
        points={[{
          device_id: "ios",
          bucket: "2026-07-29T02:00:00Z",
          upload_bytes: 1_000,
          download_bytes: 4_000,
        }]}
        range="24h"
      />,
    );
    expect(screen.getByRole("img", { name: "真实 WireGuard 上传下载流量图" })).toBeInTheDocument();
    expect(container.querySelectorAll("[data-bucket]")).toHaveLength(24);
    expect(container.querySelectorAll(".bar-download")).toHaveLength(1);
    expect(container.querySelectorAll(".bar-upload")).toHaveLength(1);
  });

  it("shows a truthful empty state", () => {
    render(<UsageChart points={null} range="24h" />);
    expect(screen.getByText("暂无真实流量样本")).toBeInTheDocument();
  });
});
