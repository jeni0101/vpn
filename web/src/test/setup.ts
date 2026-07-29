import "@testing-library/jest-dom/vitest";
import { vi } from "vitest";

class EventSourceStub {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSED = 2;
  readonly url: string;
  readonly withCredentials = false;
  readyState = 1;
  onerror = null;
  onmessage = null;
  onopen = null;

  constructor(url: string | URL) {
    this.url = String(url);
  }

  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() { return true; }
  close() { this.readyState = 2; }
}

vi.stubGlobal("EventSource", EventSourceStub);
