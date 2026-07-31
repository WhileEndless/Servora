import { afterEach, describe, expect, it, vi } from "vitest";
import { apiClient } from "./ApiClient";
import { MonitorStore } from "./MonitorStore";
import type { Session, Snapshot } from "@/types";

const session: Session = { username: "alice", csrf: "csrf", version: "test", role: "admin" };

describe("MonitorStore session restoration", () => {
  afterEach(() => vi.restoreAllMocks());

  it("keeps a valid session when telemetry is temporarily unavailable", async () => {
    vi.spyOn(apiClient, "session").mockResolvedValue(session);
    vi.spyOn(apiClient, "overview").mockRejectedValue(new Error("collector unavailable"));
    vi.spyOn(apiClient, "history").mockRejectedValue(new Error("history unavailable"));
    vi.spyOn(apiClient, "stream").mockReturnValue({ close: vi.fn() } as unknown as EventSource);

    const store = new MonitorStore();
    await expect(store.restore()).resolves.toBe(true);
    expect(store.authenticated.value).toBe(true);
    expect(store.session.value?.username).toBe("alice");
  });

	it("keeps the stream live when Go sends null for empty collections", async () => {
    vi.spyOn(apiClient, "session").mockResolvedValue(session);
    vi.spyOn(apiClient, "overview").mockRejectedValue(new Error("collector unavailable"));
    vi.spyOn(apiClient, "history").mockResolvedValue([]);
    let receive: ((snapshot: Snapshot) => void) | undefined;
    vi.spyOn(apiClient, "stream").mockImplementation((onSnapshot) => {
      receive = onSnapshot;
      return { close: vi.fn() } as unknown as EventSource;
    });

    const store = new MonitorStore();
    await store.restore();
    const snapshot = {
      timestamp: new Date().toISOString(),
      hostname: "host", kernel: "linux", uptime_seconds: 1,
      cpu: { usage: 1, load: [0, 0, 0], cores: 1 },
      memory: { Total: 1, Used: 1, Available: 0, SwapTotal: 0, SwapUsed: 0 },
      disks: null, network: null, processes: null, services: null,
      ssh_sessions: null, timers: null, containers: null, connections: null,
      docker: {
        server_version: "", storage_driver: "", containers: 0,
        containers_running: 0, containers_stopped: 0,
        containers_paused: 0, images: 0,
      },
      capabilities: null, freshness: null,
    } as unknown as Snapshot;

    expect(() => receive?.(snapshot)).not.toThrow();
    expect(store.streamConnected.value).toBe(true);
    expect(store.snapshot.value.containers).toEqual([]);
    expect(store.snapshot.value.connections).toEqual([]);
	});

	it("clears protected telemetry when the stream session expires", async () => {
		vi.spyOn(apiClient, "session").mockResolvedValue(session);
		vi.spyOn(apiClient, "overview").mockRejectedValue(new Error("collector unavailable"));
		vi.spyOn(apiClient, "history").mockResolvedValue([]);
		let expire: (() => void) | undefined;
		const close = vi.fn();
		vi.spyOn(apiClient, "stream").mockImplementation((_snapshot, _state, onExpired) => {
			expire = onExpired;
			return { close } as unknown as EventSource;
		});

		const store = new MonitorStore();
		await store.restore();
		expire?.();
		expect(close).toHaveBeenCalled();
		expect(store.authenticated.value).toBe(false);
		expect(store.snapshot.value.hostname).toBe("");
		expect(store.history.value).toEqual([]);
	});
});
