import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  }));
}

afterEach(() => {
  cleanup();
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

describe("TNest VPN protected UI", () => {
  it("renders an empty audit response without a white screen", async () => {
    vi.stubGlobal("EventSource", class {
      addEventListener() {}
      close() {}
    });
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.includes("/auth/session")) return json({ username: "admin", csrf_token: "csrf" });
      if (path.includes("/devices")) return json({ devices: [] });
      if (path.includes("/usage")) return json({ points: [] });
      if (path.includes("/audit")) return json({ events: null });
      return json({});
    }));
    render(<App />);
    await screen.findByRole("heading", { name: "运行总览" });
    fireEvent.click(screen.getByRole("button", { name: /审计/ }));
    expect(await screen.findByRole("heading", { name: "安全审计" })).toBeInTheDocument();
    expect(await screen.findByText("暂无审计记录")).toBeInTheDocument();
  });

  it("shows a recoverable state when protected APIs fail", async () => {
    vi.stubGlobal("EventSource", class {
      addEventListener() {}
      close() {}
    });
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path.includes("/auth/session")) return json({ username: "admin", csrf_token: "csrf" });
      return json({ error: "管理服务暂时不可用" }, 503);
    }));
    render(<App />);
    await waitFor(() => expect(screen.getAllByText("管理服务暂时不可用").length).toBeGreaterThan(0));
    expect(screen.getByRole("button", { name: "重新加载" })).toBeInTheDocument();
  });
});
