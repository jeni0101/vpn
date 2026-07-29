import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Results } from "@cloudflare/speedtest";
import {
  SpeedTestPanel,
  type SpeedEngine,
  type SpeedEngineFactory,
} from "./SpeedTestPanel";

afterEach(cleanup);

function mockResults(summary: ReturnType<Results["getSummary"]>) {
  return { getSummary: () => summary } as unknown as Results;
}

describe("lightweight speed test", () => {
  it("shows results without persisting them", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const engine = {
      results: mockResults({}),
      onResultsChange: vi.fn(),
      onPhaseChange: vi.fn(),
      onFinish: vi.fn(),
      onError: vi.fn(),
      play: vi.fn(),
      pause: vi.fn(),
    } as unknown as SpeedEngine;
    const factory = vi.fn(() => engine) as SpeedEngineFactory;
    render(<SpeedTestPanel createEngine={factory} />);
    fireEvent.click(screen.getByRole("button", { name: "开始轻量测试" }));
    expect(engine.play).toHaveBeenCalled();
    act(() => {
      engine.onFinish(mockResults({
        latency: 28.4,
        jitter: 3.2,
        download: 80_000_000,
        upload: 20_000_000,
        totalDurationMs: 12_000,
      }));
    });
    expect(await screen.findByText("80.0 Mbps")).toBeInTheDocument();
    expect(screen.getByText("20.0 Mbps")).toBeInTheDocument();
    expect(setItem).not.toHaveBeenCalled();
  });

  it("can cancel an active test", () => {
    const engine = {
      results: mockResults({}),
      onResultsChange: vi.fn(),
      onPhaseChange: vi.fn(),
      onFinish: vi.fn(),
      onError: vi.fn(),
      play: vi.fn(),
      pause: vi.fn(),
    } as unknown as SpeedEngine;
    render(<SpeedTestPanel createEngine={() => engine} />);
    fireEvent.click(screen.getByRole("button", { name: "开始轻量测试" }));
    fireEvent.click(screen.getByRole("button", { name: "取消测试" }));
    expect(engine.pause).toHaveBeenCalled();
    expect(screen.getByText("测试已取消，本次结果没有保存。")).toBeInTheDocument();
  });
});
